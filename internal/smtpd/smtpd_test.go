package smtpd

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/smtp"
	"net/textproto"
	"strings"
	"sync"
	"testing"
	"time"

	"phantom-mail/internal/clock"
	"phantom-mail/internal/limits"
)

type fakeInbox struct {
	mu         sync.Mutex
	boxes      map[string][]string
	acceptsErr error
	deliverErr error
}

func (f *fakeInbox) Accepts(mailbox string) error { return f.acceptsErr }

func (f *fakeInbox) Deliver(mailbox string, raw []byte) error {
	if f.deliverErr != nil {
		return f.deliverErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.boxes == nil {
		f.boxes = map[string][]string{}
	}
	f.boxes[mailbox] = append(f.boxes[mailbox], string(raw))
	return nil
}

func (f *fakeInbox) get(mailbox string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.boxes[mailbox]...)
}

func start(t *testing.T, cfg Config, inbox Inbox) (addr string, srv *Server) {
	t.Helper()
	if cfg.Domains == nil {
		cfg.Domains = []string{"localhost"}
	}
	if cfg.Clock == nil {
		cfg.Clock = clock.NewFake(time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC))
	}
	srv = New(cfg, inbox)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- srv.Serve(l) }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
		<-done
	})
	return l.Addr().String(), srv
}

func send(t *testing.T, addr, from string, rcpts []string, body string) error {
	t.Helper()
	c, err := smtp.Dial(addr)
	if err != nil {
		return err
	}
	defer c.Close()
	if err := c.Mail(from); err != nil {
		return err
	}
	for _, r := range rcpts {
		if err := c.Rcpt(r); err != nil {
			return err
		}
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := io.WriteString(w, body); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}

func code(err error) int {
	var te *textproto.Error
	if errors.As(err, &te) {
		return te.Code
	}
	return 0
}

const simpleBody = "From: Sender <sender@example.com>\r\nTo: alice@localhost\r\nSubject: Your verification code\r\n\r\nYour code is 482913\r\n"

func TestAcceptedDomainIsDelivered(t *testing.T) {
	inbox := &fakeInbox{}
	addr, _ := start(t, Config{MaxMessageBytes: 1 << 20}, inbox)
	if err := send(t, addr, "sender@example.com", []string{"alice@localhost"}, simpleBody); err != nil {
		t.Fatal(err)
	}
	got := inbox.get("alice")
	if len(got) != 1 {
		t.Fatalf("deliveries = %d", len(got))
	}
	raw := got[0]
	for _, want := range []string{"Return-Path: <sender@example.com>", "X-Original-To: alice@localhost", "Received: ", "Subject: Your verification code", "Your code is 482913"} {
		if !strings.Contains(raw, want) {
			t.Errorf("stored message lacks %q:\n%s", want, raw)
		}
	}
}

func TestUnknownDomainRejected(t *testing.T) {
	inbox := &fakeInbox{}
	addr, _ := start(t, Config{MaxMessageBytes: 1 << 20}, inbox)
	err := send(t, addr, "s@example.com", []string{"alice@evil.example.org"}, simpleBody)
	if code(err) != 550 {
		t.Fatalf("err = %v, want 550", err)
	}
	if len(inbox.boxes) != 0 {
		t.Fatal("mail for an unserved domain was stored")
	}
}

func TestInvalidMailboxRejected(t *testing.T) {
	addr, _ := start(t, Config{MaxMessageBytes: 1 << 20}, &fakeInbox{})
	for _, rcpt := range []string{"../x@localhost", "bad name@localhost", strings.Repeat("a", 65) + "@localhost"} {
		c, err := smtp.Dial(addr)
		if err != nil {
			t.Fatal(err)
		}
		_ = c.Mail("s@example.com")
		if err := c.Rcpt(rcpt); code(err) != 550 {
			t.Errorf("Rcpt(%q) err = %v, want 550", rcpt, err)
		}
		c.Close()
	}
}

func TestOversizeMessageRejected(t *testing.T) {
	inbox := &fakeInbox{}
	addr, _ := start(t, Config{MaxMessageBytes: 1000}, inbox)
	big := simpleBody + strings.Repeat("0123456789abcdef\r\n", 500)
	err := send(t, addr, "s@example.com", []string{"alice@localhost"}, big)
	if code(err) != 552 {
		t.Fatalf("err = %v, want 552", err)
	}
	if len(inbox.get("alice")) != 0 {
		t.Fatal("oversize message stored")
	}
	// The server must keep working afterwards.
	if err := send(t, addr, "s@example.com", []string{"alice@localhost"}, "Subject: small\r\n\r\nok\r\n"); err != nil {
		t.Fatalf("server unusable after oversize message: %v", err)
	}
}

func TestDeclaredSizeTooLargeRejectedEarly(t *testing.T) {
	addr, _ := start(t, Config{MaxMessageBytes: 1000}, &fakeInbox{})
	c, tp := rawConn(t, addr)
	defer c.Close()
	cmd(t, tp, "EHLO test", 250)
	if line := cmdLine(t, tp, "MAIL FROM:<a@example.com> SIZE=999999"); !strings.HasPrefix(line, "552") {
		t.Fatalf("line = %q, want 552", line)
	}
}

func TestEHLOAdvertisesSizeAndNoAuthOrStartTLS(t *testing.T) {
	addr, _ := start(t, Config{MaxMessageBytes: 12345}, &fakeInbox{})
	c, tp := rawConn(t, addr)
	defer c.Close()
	if err := tp.PrintfLine("EHLO test"); err != nil {
		t.Fatal(err)
	}
	_, msg, err := tp.ReadResponse(250)
	if err != nil {
		t.Fatal(err)
	}
	upper := strings.ToUpper(msg)
	if !strings.Contains(upper, "SIZE 12345") {
		t.Errorf("EHLO lacks SIZE: %q", msg)
	}
	for _, bad := range []string{"AUTH", "STARTTLS", "RELAY"} {
		if strings.Contains(upper, bad) {
			t.Errorf("EHLO advertises %s: %q", bad, msg)
		}
	}
	if line := cmdLine(t, tp, "AUTH PLAIN AGZvbwBiYXI="); !strings.HasPrefix(line, "502") {
		t.Errorf("AUTH reply = %q, want 502", line)
	}
	if line := cmdLine(t, tp, "STARTTLS"); !strings.HasPrefix(line, "502") {
		t.Errorf("STARTTLS reply = %q, want 502", line)
	}
}

func TestPlusAddressGoesToBaseMailboxKeepingOriginalRecipient(t *testing.T) {
	inbox := &fakeInbox{}
	addr, _ := start(t, Config{MaxMessageBytes: 1 << 20}, inbox)
	if err := send(t, addr, "s@example.com", []string{"Alice+Shop@localhost"}, simpleBody); err != nil {
		t.Fatal(err)
	}
	got := inbox.get("alice")
	if len(got) != 1 || !strings.Contains(got[0], "X-Original-To: Alice+Shop@localhost") {
		t.Fatalf("got = %v", got)
	}
}

func TestMultipleRecipients(t *testing.T) {
	inbox := &fakeInbox{}
	addr, _ := start(t, Config{MaxMessageBytes: 1 << 20}, inbox)
	if err := send(t, addr, "s@example.com", []string{"alice@localhost", "bob@localhost", "alice+x@localhost"}, simpleBody); err != nil {
		t.Fatal(err)
	}
	if n := len(inbox.get("alice")); n != 1 {
		t.Fatalf("alice got %d copies, want 1 (same mailbox is delivered once)", n)
	}
	a := inbox.get("alice")[0]
	if !strings.Contains(a, "X-Original-To: alice@localhost") || !strings.Contains(a, "X-Original-To: alice+x@localhost") {
		t.Fatalf("alice's copy lacks both original recipients:\n%s", a)
	}
	b := inbox.get("bob")
	if len(b) != 1 || strings.Contains(strings.SplitN(b[0], "\r\n\r\n", 2)[0], "X-Original-To: alice") {
		t.Fatalf("bob's copy must only list bob as original recipient: %v", b)
	}
}

func TestTooManyRecipients(t *testing.T) {
	addr, _ := start(t, Config{MaxMessageBytes: 1 << 20, MaxRecipients: 2}, &fakeInbox{})
	c, err := smtp.Dial(addr)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_ = c.Mail("s@example.com")
	_ = c.Rcpt("a@localhost")
	_ = c.Rcpt("b@localhost")
	if err := c.Rcpt("c@localhost"); code(err) != 452 {
		t.Fatalf("err = %v, want 452", err)
	}
}

func TestDotStuffingAndLineEndings(t *testing.T) {
	inbox := &fakeInbox{}
	addr, _ := start(t, Config{MaxMessageBytes: 1 << 20}, inbox)
	body := "Subject: dots\r\n\r\n.hidden line\r\n..double\r\nlast\r\n"
	if err := send(t, addr, "s@example.com", []string{"alice@localhost"}, body); err != nil {
		t.Fatal(err)
	}
	got := inbox.get("alice")[0]
	if !strings.Contains(got, "\r\n.hidden line\r\n") || !strings.Contains(got, "\r\n..double\r\n") {
		t.Fatalf("dot-unstuffing wrong:\n%q", got)
	}
}

func rawConn(t *testing.T, addr string) (net.Conn, *textproto.Conn) {
	t.Helper()
	c, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))
	tp := textproto.NewConn(c)
	if _, _, err := tp.ReadResponse(220); err != nil {
		t.Fatalf("greeting: %v", err)
	}
	return c, tp
}

func cmdLine(t *testing.T, tp *textproto.Conn, line string) string {
	t.Helper()
	if err := tp.PrintfLine("%s", line); err != nil {
		t.Fatal(err)
	}
	resp, err := tp.ReadLine()
	if err != nil {
		t.Fatalf("reading reply to %q: %v", line, err)
	}
	for len(resp) >= 4 && resp[3] == '-' { // multi-line reply
		resp, err = tp.ReadLine()
		if err != nil {
			t.Fatal(err)
		}
	}
	return resp
}

func cmd(t *testing.T, tp *textproto.Conn, line string, want int) {
	t.Helper()
	got := cmdLine(t, tp, line)
	if !strings.HasPrefix(got, fmt.Sprint(want)) {
		t.Fatalf("%q -> %q, want %d", line, got, want)
	}
}

func TestSessionCommands(t *testing.T) {
	addr, _ := start(t, Config{MaxMessageBytes: 1 << 20}, &fakeInbox{})
	c, tp := rawConn(t, addr)
	defer c.Close()
	cmd(t, tp, "HELO test", 250)
	cmd(t, tp, "NOOP", 250)
	cmd(t, tp, "RCPT TO:<a@localhost>", 503) // RCPT before MAIL
	cmd(t, tp, "DATA", 503)                  // DATA before RCPT
	cmd(t, tp, "MAIL FROM:<s@example.com>", 250)
	cmd(t, tp, "RCPT TO:<a@localhost>", 250)
	cmd(t, tp, "RSET", 250)
	cmd(t, tp, "DATA", 503) // RSET cleared the transaction
	cmd(t, tp, "VRFY a", 252)
	cmd(t, tp, "BOGUS", 500)
	cmd(t, tp, "QUIT", 221)
	if _, err := tp.ReadLine(); err == nil {
		t.Fatal("connection should be closed after QUIT")
	}
}

func TestReadTimeoutClosesIdleConnection(t *testing.T) {
	addr, _ := start(t, Config{MaxMessageBytes: 1 << 20, ReadTimeout: 100 * time.Millisecond}, &fakeInbox{})
	c, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(3 * time.Second))
	r := bufio.NewReader(c)
	if _, err := r.ReadString('\n'); err != nil { // greeting
		t.Fatal(err)
	}
	start := time.Now()
	line, err := r.ReadString('\n') // should be the 421 timeout notice, then EOF
	if err == nil && !strings.HasPrefix(line, "421") {
		t.Fatalf("line = %q", line)
	}
	if _, err := r.ReadString('\n'); err == nil {
		t.Fatal("connection still open after timeout")
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("timeout took too long")
	}
}

func TestStorageFailureYields452(t *testing.T) {
	inbox := &fakeInbox{deliverErr: errors.New("disk full")}
	addr, _ := start(t, Config{MaxMessageBytes: 1 << 20}, inbox)
	err := send(t, addr, "s@example.com", []string{"alice@localhost"}, simpleBody)
	if code(err) != 452 {
		t.Fatalf("err = %v, want 452", err)
	}
}

func TestMailboxLimitYields452AtRcpt(t *testing.T) {
	inbox := &fakeInbox{acceptsErr: errors.New("mailbox limit reached")}
	addr, _ := start(t, Config{MaxMessageBytes: 1 << 20}, inbox)
	c, err := smtp.Dial(addr)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_ = c.Mail("s@example.com")
	if err := c.Rcpt("new@localhost"); code(err) != 452 {
		t.Fatalf("err = %v, want 452", err)
	}
}

func TestConnectionRateLimit(t *testing.T) {
	clk := clock.NewFake(time.Now())
	addr, _ := start(t, Config{MaxMessageBytes: 1 << 20, ConnLimiter: limits.New(1, clk), Clock: clk}, &fakeInbox{})
	c1, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer c1.Close()
	_ = c1.SetDeadline(time.Now().Add(3 * time.Second))
	if _, _, err := textproto.NewConn(c1).ReadResponse(220); err != nil {
		t.Fatalf("first connection: %v", err)
	}
	c2, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer c2.Close()
	_ = c2.SetDeadline(time.Now().Add(3 * time.Second))
	if _, _, err := textproto.NewConn(c2).ReadResponse(220); code(err) != 421 {
		t.Fatalf("second connection err = %v, want 421", err)
	}
}

func TestMessageRateLimit(t *testing.T) {
	clk := clock.NewFake(time.Now())
	addr, _ := start(t, Config{MaxMessageBytes: 1 << 20, MessageLimiter: limits.New(1, clk), Clock: clk}, &fakeInbox{})
	if err := send(t, addr, "s@example.com", []string{"a@localhost"}, simpleBody); err != nil {
		t.Fatal(err)
	}
	err := send(t, addr, "s@example.com", []string{"a@localhost"}, simpleBody)
	if code(err) != 451 {
		t.Fatalf("err = %v, want 451", err)
	}
}

func TestMailboxRateLimit(t *testing.T) {
	clk := clock.NewFake(time.Now())
	inbox := &fakeInbox{}
	addr, _ := start(t, Config{MaxMessageBytes: 1 << 20, MailboxLimiter: limits.New(1, clk), Clock: clk}, inbox)
	if err := send(t, addr, "s@example.com", []string{"flooded@localhost"}, simpleBody); err != nil {
		t.Fatal(err)
	}
	err := send(t, addr, "s@example.com", []string{"flooded+again@localhost"}, simpleBody)
	if code(err) != 451 {
		t.Fatalf("err = %v, want 451 (plus-tags share a mailbox limit)", err)
	}
	if err := send(t, addr, "s@example.com", []string{"other@localhost"}, simpleBody); err != nil {
		t.Fatalf("other mailboxes must be unaffected: %v", err)
	}
}

func TestShutdownLetsInFlightDataFinishAndRefusesNewConnections(t *testing.T) {
	inbox := &fakeInbox{}
	addr, srv := start(t, Config{MaxMessageBytes: 1 << 20}, inbox)
	c, tp := rawConn(t, addr)
	defer c.Close()
	cmd(t, tp, "EHLO t", 250)
	cmd(t, tp, "MAIL FROM:<s@example.com>", 250)
	cmd(t, tp, "RCPT TO:<alice@localhost>", 250)
	cmd(t, tp, "DATA", 354)
	if err := tp.PrintfLine("Subject: draining"); err != nil {
		t.Fatal(err)
	}

	shutdownDone := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		shutdownDone <- srv.Shutdown(ctx)
	}()
	time.Sleep(100 * time.Millisecond)

	if conn, err := net.DialTimeout("tcp", addr, 300*time.Millisecond); err == nil {
		// Some platforms accept then reset; either way no greeting may arrive.
		_ = conn.SetDeadline(time.Now().Add(300 * time.Millisecond))
		if _, _, err := textproto.NewConn(conn).ReadResponse(220); err == nil {
			t.Fatal("server accepted a new session during shutdown")
		}
		conn.Close()
	}

	_ = tp.PrintfLine("")
	_ = tp.PrintfLine("finishing the message")
	_ = tp.PrintfLine(".")
	line, err := tp.ReadLine()
	if err != nil || !strings.HasPrefix(line, "250") {
		t.Fatalf("in-flight DATA reply = %q, %v", line, err)
	}
	if err := <-shutdownDone; err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if got := inbox.get("alice"); len(got) != 1 || !strings.Contains(got[0], "finishing the message") {
		t.Fatalf("in-flight message not persisted: %v", got)
	}
}

func TestConnectionCap(t *testing.T) {
	addr, _ := start(t, Config{MaxMessageBytes: 1 << 20, MaxConnections: 2}, &fakeInbox{})
	var held []net.Conn
	for i := 0; i < 2; i++ {
		c, _ := rawConn(t, addr)
		held = append(held, c)
	}
	defer func() {
		for _, c := range held {
			c.Close()
		}
	}()
	c, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(3 * time.Second))
	if _, _, err := textproto.NewConn(c).ReadResponse(220); code(err) != 421 {
		t.Fatalf("third connection err = %v, want 421", err)
	}
	// Capacity frees up when a session ends.
	held[0].Close()
	time.Sleep(100 * time.Millisecond)
	c2, _ := rawConn(t, addr)
	c2.Close()
}

func TestCommandCapPerSession(t *testing.T) {
	addr, _ := start(t, Config{MaxMessageBytes: 1 << 20, MaxCommands: 5}, &fakeInbox{})
	c, tp := rawConn(t, addr)
	defer c.Close()
	for i := 0; i < 5; i++ {
		cmd(t, tp, "NOOP", 250)
	}
	if line, err := tp.ReadLine(); err != nil || !strings.HasPrefix(line, "421") {
		t.Fatalf("after the cap: %q %v, want 421", line, err)
	}
}

func TestOverlongCommandLineIsRejected(t *testing.T) {
	addr, _ := start(t, Config{MaxMessageBytes: 1 << 20}, &fakeInbox{})
	c, tp := rawConn(t, addr)
	defer c.Close()
	if line := cmdLine(t, tp, "NOOP "+strings.Repeat("x", 20000)); !strings.HasPrefix(line, "500") {
		t.Fatalf("reply = %q, want 500", line)
	}
	cmd(t, tp, "NOOP", 250) // the session survives
}
