package httpapi

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"phantom-mail/internal/limits"
)

const (
	cookieName = "pm_session"
	apiPrefix  = "/api/v1/"
)

// sessionValue is the cookie value for a signed-in browser: an HMAC of a fixed
// label keyed with the access token. The raw token is never stored in the
// browser, and the value cannot be used as the token.
func (s *Server) sessionValue() string {
	mac := hmac.New(sha256.New, []byte(s.cfg.APIToken))
	mac.Write([]byte("phantom-mail session v1"))
	return hex.EncodeToString(mac.Sum(nil))
}

// equal compares secrets in constant time (hashing first hides their length).
func equal(a, b string) bool {
	ha, hb := sha256.Sum256([]byte(a)), sha256.Sum256([]byte(b))
	return subtle.ConstantTimeCompare(ha[:], hb[:]) == 1
}

func (s *Server) authorized(r *http.Request) bool {
	if s.cfg.APIToken == "" {
		return true
	}
	if h := r.Header.Get("Authorization"); len(h) > 7 && strings.EqualFold(h[:7], "Bearer ") {
		if equal(strings.TrimSpace(h[7:]), s.cfg.APIToken) {
			return true
		}
	}
	if c, err := r.Cookie(cookieName); err == nil && equal(c.Value, s.sessionValue()) {
		return true
	}
	return false
}

// authenticate protects /api/v1 (except sign-in) when a token is configured.
// The health check, the OpenAPI document and the static web interface stay
// open so the sign-in page itself can load. Tokens in query strings are
// never read.
func (s *Server) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if s.cfg.APIToken != "" && (strings.HasPrefix(p, apiPrefix) || p == "/api") &&
			p != apiPrefix+"session" && !s.authorized(r) {
			w.Header().Set("WWW-Authenticate", `Bearer realm="phantom-mail"`)
			writeError(w, http.StatusUnauthorized, "unauthorized", "an access token is required")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// rateLimit applies the per-client request budget (health checks exempt).
func (s *Server) rateLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.cfg.RateLimiter != nil && r.URL.Path != "/healthz" && !s.cfg.RateLimiter.Allow(s.clientIP(r)) {
			w.Header().Set("Retry-After", "60")
			writeError(w, http.StatusTooManyRequests, "rate_limited", "too many requests, slow down")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) secureRequest(r *http.Request) bool {
	return r.TLS != nil || limits.ForwardedHTTPS(r.RemoteAddr, r.Header.Get("X-Forwarded-Proto"), s.cfg.TrustedProxies)
}

// createSession exchanges the access token for a session cookie so browsers
// can use EventSource, iframes and download links, which cannot carry an
// Authorization header.
func (s *Server) createSession(w http.ResponseWriter, r *http.Request) {
	if s.cfg.APIToken == "" { // nothing to sign in to
		w.WriteHeader(http.StatusNoContent)
		return
	}
	client := s.clientIP(r)
	if !s.cfg.AuthFailLimiter.Peek(client) {
		w.Header().Set("Retry-After", "60")
		writeError(w, http.StatusTooManyRequests, "rate_limited", "too many failed sign-in attempts, try again later")
		return
	}
	var body struct {
		Token string `json:"token"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	if err := dec.Decode(&body); err != nil || body.Token == "" {
		writeError(w, http.StatusBadRequest, "bad_request", `send a JSON body like {"token":"..."}`)
		return
	}
	if !equal(body.Token, s.cfg.APIToken) {
		s.cfg.AuthFailLimiter.Allow(client) // spend one unit of the failure budget
		s.cfg.Logger.Warn("sign-in failed", "client", client)
		writeError(w, http.StatusUnauthorized, "unauthorized", "the access token is not correct")
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    s.sessionValue(),
		Path:     "/",
		HttpOnly: true,
		Secure:   s.secureRequest(r),
		SameSite: http.SameSiteStrictMode,
		// No expiry: the cookie lasts for the browser session.
	})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) deleteSession(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   s.secureRequest(r),
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
	})
	w.Header().Set("Content-Length", strconv.Itoa(0))
	w.WriteHeader(http.StatusNoContent)
}
