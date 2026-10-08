package e2e

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/smtp"
	"strings"
	"testing"
	"time"

	"phantom-mail/internal/app"
	"phantom-mail/internal/clock"
	"phantom-mail/internal/config"
)

// startApp runs a full in-process instance on loopback ports.
func startApp(t *testing.T, mutate ...func(*config.Config)) *app.App {
	t.Helper()
	cfg := config.Defaults()
	cfg.HTTPAddr = "127.0.0.1:0"
	cfg.SMTPAddr = "127.0.0.1:0"
	cfg.DataDir = t.TempDir()
	for _, m := range mutate {
		m(&cfg)
	}
	a, err := app.New(cfg, app.NewLogger(io.Discard, "error"), clock.Real{})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = a.Shutdown(ctx)
	})
	return a
}

func sendMail(t *testing.T, a *app.App, to, subject, body string) {
	t.Helper()
	msg := fmt.Sprintf("From: Sender <sender@example.com>\r\nTo: %s\r\nSubject: %s\r\n\r\n%s\r\n", to, subject, body)
	if err := smtp.SendMail(a.SMTPAddr(), nil, "sender@example.com", []string{to}, []byte(msg)); err != nil {
		t.Fatalf("SendMail: %v", err)
	}
}

func httpGet(t *testing.T, url string, hdr ...string) (*http.Response, string) {
	t.Helper()
	req, _ := http.NewRequest("GET", url, nil)
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, string(b)
}

func getJSON(t *testing.T, url string, v any, hdr ...string) *http.Response {
	t.Helper()
	resp, body := httpGet(t, url, hdr...)
	if err := json.Unmarshal([]byte(body), v); err != nil && resp.StatusCode == 200 {
		t.Fatalf("bad JSON from %s: %q", url, body)
	}
	return resp
}

// sseEvents streams "event name -> data" pairs from an SSE URL.
func sseEvents(t *testing.T, url string) <-chan [2]string {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	out := make(chan [2]string, 64)
	go func() {
		defer close(out)
		sc := bufio.NewScanner(resp.Body)
		name := ""
		for sc.Scan() {
			l := sc.Text()
			switch {
			case strings.HasPrefix(l, "event: "):
				name = strings.TrimPrefix(l, "event: ")
			case strings.HasPrefix(l, "data: "):
				out <- [2]string{name, strings.TrimPrefix(l, "data: ")}
			}
		}
	}()
	return out
}

func smtpSend(addr, to, raw string) error {
	return smtp.SendMail(addr, nil, "sender@example.com", []string{to}, []byte(raw))
}
