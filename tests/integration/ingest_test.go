package integration

import (
	"context"
	"net"
	"net/smtp"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"phantom-mail/internal/clock"
	"phantom-mail/internal/hub"
	"phantom-mail/internal/inbox"
	"phantom-mail/internal/smtpd"
	"phantom-mail/internal/store"
	"phantom-mail/internal/store/fs"
)

// startIngest wires SMTP -> inbox service -> fs store + hub on loopback.
func startIngest(t *testing.T, dir string) (smtpAddr string, svc *inbox.Service) {
	t.Helper()
	st, err := fs.New(dir, store.Options{Clock: clock.Real{}})
	if err != nil {
		t.Fatal(err)
	}
	svc = inbox.New(st, hub.New())
	srv := smtpd.New(smtpd.Config{
		Domains:         []string{"localhost"},
		MaxMessageBytes: 1 << 20,
		Clock:           clock.Real{},
	}, svc)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = srv.Serve(l) }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	})
	return l.Addr().String(), svc
}

func TestSMTPDeliveryIsPersistedAndPublished(t *testing.T) {
	dir := t.TempDir()
	addr, svc := startIngest(t, dir)

	events, cancel := svc.Subscribe("alice")
	defer cancel()

	msg := "From: Sender <sender@example.com>\r\nTo: alice@localhost\r\nSubject: Your verification code\r\n\r\nYour code is 482913\r\n"
	if err := smtp.SendMail(addr, nil, "sender@example.com", []string{"alice@localhost"}, []byte(msg)); err != nil {
		t.Fatal(err)
	}

	select {
	case ev := <-events:
		if ev.Summary.Subject != "Your verification code" || ev.Summary.Mailbox != "alice" {
			t.Fatalf("event = %+v", ev)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no hub event after delivery")
	}

	list, err := svc.List("alice", "", 10)
	if err != nil || len(list) != 1 {
		t.Fatalf("list = %v, %v", list, err)
	}
	files, _ := filepath.Glob(filepath.Join(dir, "alice", "*.eml"))
	if len(files) != 1 {
		t.Fatalf("expected one persisted .eml file, got %v", files)
	}
	raw, err := os.ReadFile(files[0])
	if err != nil || !strings.Contains(string(raw), "Your code is 482913") {
		t.Fatalf("persisted file = %q, %v", raw, err)
	}
	if !strings.Contains(string(raw), "X-Original-To: alice@localhost") {
		t.Fatalf("original recipient not preserved:\n%s", raw)
	}
}
