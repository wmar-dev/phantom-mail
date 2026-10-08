package integration

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/smtp"
	"testing"
	"time"

	"phantom-mail/internal/app"
	"phantom-mail/internal/clock"
	"phantom-mail/internal/config"
)

func startOn(t *testing.T, dir string) *app.App {
	t.Helper()
	cfg := config.Defaults()
	cfg.HTTPAddr, cfg.SMTPAddr, cfg.DataDir = "127.0.0.1:0", "127.0.0.1:0", dir
	a, err := app.New(cfg, app.NewLogger(io.Discard, "error"), clock.Real{})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Start(); err != nil {
		t.Fatal(err)
	}
	return a
}

func stop(t *testing.T, a *app.App) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := a.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
}

func listIDs(t *testing.T, a *app.App, mailbox string) []string {
	t.Helper()
	resp, err := http.Get("http://" + a.HTTPAddr() + "/api/v1/mailboxes/" + mailbox + "/messages")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out struct {
		Messages []struct {
			ID      string `json:"id"`
			Subject string `json:"subject"`
		} `json:"messages"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, m := range out.Messages {
		ids = append(ids, m.ID+":"+m.Subject)
	}
	return ids
}

// A restart (or a replaced container using the same volume) must not lose
// mail that is still within its retention period.
func TestMessagesSurviveARestart(t *testing.T) {
	dir := t.TempDir()
	a := startOn(t, dir)
	for _, subj := range []string{"first", "second", "third"} {
		msg := "From: s@example.com\r\nTo: alice@localhost\r\nSubject: " + subj + "\r\n\r\nbody\r\n"
		if err := smtp.SendMail(a.SMTPAddr(), nil, "s@example.com", []string{"alice@localhost"}, []byte(msg)); err != nil {
			t.Fatal(err)
		}
	}
	before := listIDs(t, a, "alice")
	if len(before) != 3 {
		t.Fatalf("before restart: %v", before)
	}
	stop(t, a)

	b := startOn(t, dir)
	defer stop(t, b)
	after := listIDs(t, b, "alice")
	if len(after) != 3 {
		t.Fatalf("after restart: %v", after)
	}
	for i := range before {
		if before[i] != after[i] {
			t.Fatalf("message %d changed across restart: %q -> %q", i, before[i], after[i])
		}
	}
	// New mail keeps working and sorts after the restored mail.
	msg := "Subject: fourth\r\n\r\nbody\r\n"
	if err := smtp.SendMail(b.SMTPAddr(), nil, "s@example.com", []string{"alice@localhost"}, []byte(msg)); err != nil {
		t.Fatal(err)
	}
	if got := listIDs(t, b, "alice"); len(got) != 4 || got[0][33:] != "fourth" {
		t.Fatalf("after new mail: %v", got)
	}
}

func TestStartingTwiceOnTheSameAddressFailsClearly(t *testing.T) {
	a := startOn(t, t.TempDir())
	defer stop(t, a)
	cfg := config.Defaults()
	cfg.HTTPAddr, cfg.SMTPAddr, cfg.DataDir = a.HTTPAddr(), "127.0.0.1:0", t.TempDir()
	b, err := app.New(cfg, app.NewLogger(io.Discard, "error"), clock.Real{})
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Start(); err == nil {
		stop(t, b)
		t.Fatal("expected an error when the port is taken")
	}
}
