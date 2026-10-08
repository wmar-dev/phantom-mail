package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/smtp"
	"strings"
	"testing"
	"time"

	"phantom-mail/internal/app"
	"phantom-mail/internal/clock"
	"phantom-mail/internal/config"
)

const secret = "very-secret-token-value"

func runLogged(t *testing.T, level string) *bytes.Buffer {
	t.Helper()
	cfg := config.Defaults()
	cfg.HTTPAddr, cfg.SMTPAddr, cfg.Storage = "127.0.0.1:0", "127.0.0.1:0", "memory"
	cfg.APIToken = secret
	var buf bytes.Buffer
	a, err := app.New(cfg, app.NewLogger(&buf, level), clock.Real{})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Start(); err != nil {
		t.Fatal(err)
	}
	base := "http://" + a.HTTPAddr()
	msg := "From: s@example.com\r\nTo: alice@localhost\r\nSubject: x\r\n\r\nbody\r\n"
	_ = smtp.SendMail(a.SMTPAddr(), nil, "s@example.com", []string{"alice@localhost"}, []byte(msg))
	for _, req := range []struct{ path, auth string }{
		{"/api/v1/mailboxes/alice/messages", "Bearer " + secret},
		{"/api/v1/mailboxes/alice/messages", "Bearer nope"},
		{"/api/v1/mailboxes/alice/messages?token=" + secret, ""},
	} {
		r, _ := http.NewRequest("GET", base+req.path, nil)
		if req.auth != "" {
			r.Header.Set("Authorization", req.auth)
		}
		if resp, err := http.DefaultClient.Do(r); err == nil {
			resp.Body.Close()
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = a.Shutdown(ctx)
	return &buf
}

func TestLogsAreStructuredJSONAndNeverContainSecrets(t *testing.T) {
	buf := runLogged(t, "info")
	sc := bufio.NewScanner(buf)
	n := 0
	for sc.Scan() {
		var obj map[string]any
		if err := json.Unmarshal(sc.Bytes(), &obj); err != nil {
			t.Fatalf("line %d is not a JSON object: %q", n+1, sc.Text())
		}
		for _, k := range []string{"time", "level", "msg"} {
			if _, ok := obj[k]; !ok {
				t.Fatalf("line %d lacks %q: %q", n+1, k, sc.Text())
			}
		}
		n++
	}
	if n < 4 {
		t.Fatalf("expected startup, delivery, request and shutdown logs, got %d lines", n)
	}
	if strings.Contains(buf.String(), secret) {
		t.Fatal("the access token appears in the logs")
	}
	if strings.Contains(buf.String(), "body") && strings.Contains(buf.String(), "\"body\"") {
		t.Fatal("message content must not be logged")
	}
}

func TestLogLevelFiltersOutput(t *testing.T) {
	quiet := runLogged(t, "error")
	if strings.Contains(quiet.String(), `"level":"INFO"`) {
		t.Fatalf("info lines present at level=error:\n%s", quiet.String())
	}
}
