package integration

import (
	"encoding/json"
	"io"
	"net/http"
	"net/smtp"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"phantom-mail/internal/app"
	"phantom-mail/internal/clock"
	"phantom-mail/internal/config"
)

// A message past its retention period must disappear from every read path:
// list, direct fetch, wait, the event stream, and the disk.
func TestExpiredMessageDisappearsEverywhere(t *testing.T) {
	clk := clock.NewFake(time.Now())
	dir := t.TempDir()
	cfg := config.Defaults()
	cfg.HTTPAddr, cfg.SMTPAddr, cfg.DataDir = "127.0.0.1:0", "127.0.0.1:0", dir
	cfg.Retention = time.Hour
	cfg.JanitorInterval = 20 * time.Millisecond
	a, err := app.New(cfg, app.NewLogger(io.Discard, "error"), clk)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Start(); err != nil {
		t.Fatal(err)
	}
	defer stop(t, a)
	base := "http://" + a.HTTPAddr() + "/api/v1/mailboxes/alice"

	msg := "From: s@example.com\r\nTo: alice@localhost\r\nSubject: short lived\r\n\r\nbye\r\n"
	if err := smtp.SendMail(a.SMTPAddr(), nil, "s@example.com", []string{"alice@localhost"}, []byte(msg)); err != nil {
		t.Fatal(err)
	}
	var list struct {
		Messages []struct {
			ID string `json:"id"`
		} `json:"messages"`
	}
	get := func(path string) (*http.Response, string) {
		resp, err := http.Get(base + path)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp, string(b)
	}
	_, body := get("/messages")
	if err := json.Unmarshal([]byte(body), &list); err != nil || len(list.Messages) != 1 {
		t.Fatalf("setup: %q %v", body, err)
	}
	id := list.Messages[0].ID

	// Listen for the deletion announcement before time passes.
	sse, err := http.Get(base + "/events")
	if err != nil {
		t.Fatal(err)
	}
	defer sse.Body.Close()
	sawDeleted := make(chan string, 1)
	go func() {
		buf := make([]byte, 4096)
		var acc strings.Builder
		for {
			n, err := sse.Body.Read(buf)
			acc.Write(buf[:n])
			if strings.Contains(acc.String(), "event: deleted") {
				sawDeleted <- acc.String()
				return
			}
			if err != nil {
				return
			}
		}
	}()

	clk.Advance(61 * time.Minute)

	select {
	case got := <-sawDeleted:
		if !strings.Contains(got, id) {
			t.Fatalf("deleted event does not name the message: %q", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no deleted event after expiry")
	}

	if _, body := get("/messages"); strings.TrimSpace(body) != `{"messages":[]}` {
		t.Fatalf("list still shows the message: %s", body)
	}
	if resp, _ := get("/messages/" + id); resp.StatusCode != 404 {
		t.Fatalf("direct fetch = %d, want 404", resp.StatusCode)
	}
	if resp, _ := get("/messages/wait?timeout=1"); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("wait = %d, want 204 (nothing left to return)", resp.StatusCode)
	}
	if files, _ := filepath.Glob(filepath.Join(dir, "alice", "*")); len(files) != 0 {
		t.Fatalf("files remain on disk: %v", files)
	}
}
