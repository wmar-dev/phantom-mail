// Package app wires configuration, storage, the hub, the SMTP receiver and
// (as stories land) the HTTP server into one process with graceful shutdown.
package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"phantom-mail/internal/clock"
	"phantom-mail/internal/config"
	"phantom-mail/internal/httpapi"
	"phantom-mail/internal/hub"
	"phantom-mail/internal/inbox"
	"phantom-mail/internal/limits"
	"phantom-mail/internal/retention"
	"phantom-mail/internal/smtpd"
	"phantom-mail/internal/store"
	"phantom-mail/internal/store/fs"
	"phantom-mail/internal/store/memory"
	"phantom-mail/internal/web"
)

// NewLogger returns a JSON logger writing one object per line to w.
func NewLogger(w io.Writer, level string) *slog.Logger {
	var lv slog.Level
	switch level {
	case "debug":
		lv = slog.LevelDebug
	case "warn":
		lv = slog.LevelWarn
	case "error":
		lv = slog.LevelError
	default:
		lv = slog.LevelInfo
	}
	return slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{Level: lv}))
}

// App is a running (or ready to run) Phantom Mail instance.
type App struct {
	cfg  config.Config
	log  *slog.Logger
	clk  clock.Clock
	svc  *inbox.Service
	smtp *smtpd.Server
	api  *httpapi.Server
	http *http.Server

	smtpL net.Listener
	httpL net.Listener

	ctx    context.Context // cancelled on Shutdown to stop background work
	cancel context.CancelFunc

	mu     sync.Mutex
	closed bool
	done   sync.WaitGroup
}

// New builds an App from cfg. Nothing is listening until Start.
func New(cfg config.Config, log *slog.Logger, clk clock.Clock) (*App, error) {
	if clk == nil {
		clk = clock.Real{}
	}
	opts := store.Options{
		Retention:     cfg.Retention,
		MaxPerMailbox: cfg.MaxMessagesPerMailbox,
		MaxMailboxes:  cfg.MaxMailboxes,
		MaxTotalBytes: cfg.MaxTotalBytes,
		Clock:         clk,
	}
	var (
		st  store.Store
		err error
	)
	if cfg.Storage == "memory" {
		st = memory.New(opts)
	} else {
		if st, err = fs.New(cfg.DataDir, opts); err != nil {
			return nil, err
		}
	}
	svc := inbox.New(st, hub.New())
	a := &App{cfg: cfg, log: log, clk: clk, svc: svc}
	a.ctx, a.cancel = context.WithCancel(context.Background())
	a.smtp = smtpd.New(smtpd.Config{
		Domains:         cfg.Domains,
		MaxMessageBytes: cfg.MaxMessageBytes,
		ConnLimiter:     limits.New(cfg.RateSMTPConn, clk),
		MessageLimiter:  limits.New(cfg.RateSMTPMessage, clk),
		MailboxLimiter:  limits.New(cfg.RateMailbox, clk),
		Logger:          log,
		Clock:           clk,
	}, svc)
	a.api = httpapi.New(httpapi.Config{
		APIToken:        cfg.APIToken,
		TrustedProxies:  cfg.TrustedProxies,
		MaxSSEPerClient: cfg.MaxStreamsPerClient,
		RateLimiter:     limits.New(cfg.RateHTTP, clk),
		AuthFailLimiter: limits.New(cfg.RateAuthFail, clk),
		UI:              web.Handler(),
		Logger:          log,
		Clock:           clk,
	}, svc)
	a.http = &http.Server{
		Handler:           a.api.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		MaxHeaderBytes:    64 << 10,
		IdleTimeout:       2 * time.Minute,
		// No WriteTimeout: event streams and long polls are long-lived.
	}
	return a, nil
}

// Service exposes the mailbox service (for tests and wiring).
func (a *App) Service() *inbox.Service { return a.svc }

// Start opens the listeners and serves in the background.
func (a *App) Start() error {
	l, err := net.Listen("tcp", a.cfg.SMTPAddr)
	if err != nil {
		return fmt.Errorf("smtp listen: %w", err)
	}
	hl, err := net.Listen("tcp", a.cfg.HTTPAddr)
	if err != nil {
		_ = l.Close()
		return fmt.Errorf("http listen: %w", err)
	}
	a.smtpL, a.httpL = l, hl
	a.done.Add(3)
	go func() {
		defer a.done.Done()
		retention.Run(a.ctx, a.svc, a.cfg.JanitorInterval, a.log)
	}()
	go func() {
		defer a.done.Done()
		if err := a.http.Serve(hl); err != nil && !errors.Is(err, http.ErrServerClosed) {
			a.log.Error("http server stopped", "error", err.Error())
		}
	}()
	go func() {
		defer a.done.Done()
		if err := a.smtp.Serve(l); err != nil {
			a.log.Error("smtp server stopped", "error", err.Error())
		}
	}()
	a.log.Info("started", "smtp", l.Addr().String(), "http", hl.Addr().String(), "domains", a.cfg.Domains, "storage", a.cfg.Storage)
	return nil
}

// SMTPAddr is the address the SMTP receiver listens on (after Start).
func (a *App) SMTPAddr() string {
	if a.smtpL == nil {
		return ""
	}
	return a.smtpL.Addr().String()
}

// HTTPAddr is the address the HTTP server listens on (after Start).
func (a *App) HTTPAddr() string {
	if a.httpL == nil {
		return ""
	}
	return a.httpL.Addr().String()
}

// Shutdown stops accepting new work, lets in-flight SMTP messages finish, and
// waits for background goroutines, until ctx ends.
func (a *App) Shutdown(ctx context.Context) error {
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return nil
	}
	a.closed = true
	a.mu.Unlock()

	a.log.Info("shutting down")
	a.cancel() // stop the retention janitor
	var errs []error
	if err := a.smtp.Shutdown(ctx); err != nil {
		errs = append(errs, err)
	}
	a.api.Close() // end event streams so the HTTP server can drain
	if err := a.http.Shutdown(ctx); err != nil {
		errs = append(errs, err)
		_ = a.http.Close()
	}
	waited := make(chan struct{})
	go func() { a.done.Wait(); close(waited) }()
	select {
	case <-waited:
	case <-ctx.Done():
		errs = append(errs, ctx.Err())
	}
	a.log.Info("stopped")
	return errors.Join(errs...)
}

// Run starts the app and blocks until ctx is cancelled, then shuts down
// gracefully (up to 30 seconds).
func Run(ctx context.Context, cfg config.Config, log *slog.Logger) error {
	a, err := New(cfg, log, clock.Real{})
	if err != nil {
		return err
	}
	if err := a.Start(); err != nil {
		return err
	}
	<-ctx.Done()
	sctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return a.Shutdown(sctx)
}
