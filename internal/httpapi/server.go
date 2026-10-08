// Package httpapi serves the JSON API, Server-Sent Events, the health check
// and (via Config.UI) the embedded web interface.
package httpapi

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"

	"phantom-mail/internal/clock"
	"phantom-mail/internal/inbox"
	"phantom-mail/internal/limits"
	"phantom-mail/internal/mailbox"
	"phantom-mail/internal/message"
)

// Config configures the HTTP server. Zero values are valid.
type Config struct {
	// APIToken, when set, is required for every /api/v1 request except sign-in.
	APIToken string
	// TrustedProxies lists peers whose X-Forwarded-* headers are believed.
	TrustedProxies []netip.Prefix
	// RateLimiter limits requests per client address; AuthFailLimiter limits
	// failed sign-in attempts. Nil means unlimited.
	RateLimiter     *limits.Limiter
	AuthFailLimiter *limits.Limiter
	// UI serves everything that is not under /api/, /healthz or /openapi.yaml.
	UI http.Handler

	HeartbeatInterval time.Duration // SSE keep-alive; default 15s
	MaxSSEPerClient   int           // concurrent event streams per client; default 100
	MaxWait           time.Duration // upper bound for long polls; default 60s

	Logger *slog.Logger
	Clock  clock.Clock
}

// Server is the HTTP side of the service.
type Server struct {
	cfg     Config
	svc     *inbox.Service
	started time.Time
	mux     *http.ServeMux

	closeOnce sync.Once
	closing   chan struct{}

	sseMu sync.Mutex
	sse   map[string]int

	routes_ []string // registered "METHOD /path" patterns, for the contract test
}

// New returns a Server over svc.
func New(cfg Config, svc *inbox.Service) *Server {
	if cfg.HeartbeatInterval <= 0 {
		cfg.HeartbeatInterval = 15 * time.Second
	}
	if cfg.MaxSSEPerClient <= 0 {
		cfg.MaxSSEPerClient = 100
	}
	if cfg.MaxWait <= 0 {
		cfg.MaxWait = 60 * time.Second
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	if cfg.Clock == nil {
		cfg.Clock = clock.Real{}
	}
	s := &Server{
		cfg:     cfg,
		svc:     svc,
		started: cfg.Clock.Now(),
		closing: make(chan struct{}),
		sse:     map[string]int{},
	}
	s.routes()
	return s
}

// Close ends open event streams so the HTTP server can shut down promptly.
func (s *Server) Close() { s.closeOnce.Do(func() { close(s.closing) }) }

// Routes lists the registered API endpoints as "METHOD /path" patterns.
func (s *Server) Routes() []string { return append([]string(nil), s.routes_...) }

func (s *Server) routes() {
	m := http.NewServeMux()
	handle := func(pattern string, h http.HandlerFunc) {
		m.HandleFunc(pattern, h)
		s.routes_ = append(s.routes_, pattern)
	}
	handle("GET /healthz", s.health)
	handle("GET /openapi.yaml", s.openapi)
	handle("POST /api/v1/session", s.createSession)
	handle("DELETE /api/v1/session", s.deleteSession)

	const base = "/api/v1/mailboxes/{mailbox}"
	handle("GET "+base+"/messages", s.listMessages)
	handle("DELETE "+base+"/messages", s.emptyMailbox)
	handle("GET "+base+"/messages/wait", s.waitForMessage)
	handle("GET "+base+"/messages/{id}", s.getMessage)
	handle("DELETE "+base+"/messages/{id}", s.deleteMessage)
	handle("GET "+base+"/messages/{id}/html", s.getHTML)
	handle("GET "+base+"/messages/{id}/attachments/{index}", s.getAttachment)
	handle("GET "+base+"/events", s.events)

	m.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/" && r.URL.Path != "/api" && s.knownAPIPath(r) {
			writeError(w, http.StatusMethodNotAllowed, "bad_request", "method not allowed")
			return
		}
		writeError(w, http.StatusNotFound, "not_found", "no such API endpoint")
	})
	if s.cfg.UI != nil {
		m.Handle("/", s.cfg.UI)
	} else {
		m.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			writeError(w, http.StatusNotFound, "not_found", "not found")
		})
	}
	s.mux = m
}

// knownAPIPath reports whether the path matches a registered pattern for some
// other method (so the client gets 405 instead of 404).
func (s *Server) knownAPIPath(r *http.Request) bool {
	for _, method := range []string{"GET", "POST", "DELETE"} {
		probe := r.Clone(r.Context())
		probe.Method = method
		if _, pattern := s.mux.Handler(probe); pattern != "" && pattern != "/api/" && pattern != "/" {
			return true
		}
	}
	return false
}

// Handler returns the full handler with middleware applied.
func (s *Server) Handler() http.Handler { return s.middleware(s.mux) }

func (s *Server) middleware(next http.Handler) http.Handler {
	return s.recoverer(s.hardening(s.logging(s.rateLimit(s.authenticate(next)))))
}

// hardening sets headers every response should carry and bounds how long a
// slow reader can hold an ordinary response open. Event streams and long
// polls manage their own lifetime.
func (s *Server) hardening(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		if !strings.HasSuffix(r.URL.Path, "/events") && !strings.HasSuffix(r.URL.Path, "/messages/wait") {
			_ = http.NewResponseController(w).SetWriteDeadline(s.cfg.Clock.Now().Add(30 * time.Second))
		}
		next.ServeHTTP(w, r)
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (w *statusWriter) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	n, err := w.ResponseWriter.Write(b)
	w.bytes += n
	return n, err
}

// Unwrap lets http.ResponseController reach the underlying writer (Flush etc.).
func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (s *Server) logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			next.ServeHTTP(w, r)
			return
		}
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w}
		next.ServeHTTP(sw, r)
		s.cfg.Logger.Info("http request",
			"method", r.Method, "path", r.URL.Path, "status", sw.status,
			"bytes", sw.bytes, "duration_ms", time.Since(start).Milliseconds(),
			"client", s.clientIP(r))
	})
}

func (s *Server) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				if v == http.ErrAbortHandler {
					panic(v)
				}
				s.cfg.Logger.Error("panic in handler", "path", r.URL.Path, "panic", v)
				writeError(w, http.StatusInternalServerError, "internal", "internal error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func (s *Server) clientIP(r *http.Request) string {
	return limits.ClientIP(r.RemoteAddr, r.Header.Values("X-Forwarded-For"), s.cfg.TrustedProxies)
}

// ---- helpers shared by handlers ----

func writeJSON(w http.ResponseWriter, status int, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = w.Write(append(b, '\n'))
}

func writeError(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": msg}})
}

// mailboxParam validates and normalizes the {mailbox} path value, writing a
// 400 response when it is invalid.
func mailboxParam(w http.ResponseWriter, r *http.Request) (string, bool) {
	name, err := mailbox.Normalize(r.PathValue("mailbox"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_mailbox",
			"mailbox names use 1-64 letters, digits, '.', '_' or '-'")
		return "", false
	}
	return name, true
}

func summariesOrEmpty(l []message.Summary) []message.Summary {
	if l == nil {
		return []message.Summary{}
	}
	return l
}
