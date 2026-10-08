// Package smtpd is a minimal receive-only SMTP server. It accepts mail for
// configured domains only, never relays, never sends, and offers neither
// AUTH nor STARTTLS.
package smtpd

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"phantom-mail/internal/clock"
	"phantom-mail/internal/limits"
	"phantom-mail/internal/mailbox"
)

// Inbox receives accepted mail.
type Inbox interface {
	// Accepts reports whether a message for the mailbox can be stored now.
	Accepts(mailbox string) error
	// Deliver stores one raw message (with envelope headers prepended).
	Deliver(mailbox string, raw []byte) error
}

// Config configures a Server. Zero values use sensible defaults.
type Config struct {
	Hostname        string
	Domains         []string
	MaxMessageBytes int64
	MaxRecipients   int
	MaxConnections  int           // concurrent sessions; default 1000
	MaxCommands     int           // commands per session; default 1000
	ReadTimeout     time.Duration // per command
	DataTimeout     time.Duration // whole DATA phase

	// Optional limiters; nil means unlimited.
	ConnLimiter    *limits.Limiter // per remote address
	MessageLimiter *limits.Limiter // per remote address
	MailboxLimiter *limits.Limiter // per mailbox

	Logger *slog.Logger
	Clock  clock.Clock
}

// Server is an SMTP receiver.
type Server struct {
	cfg   Config
	inbox Inbox

	mu       sync.Mutex
	listener net.Listener
	sessions map[*session]struct{}
	closing  bool
	wg       sync.WaitGroup
}

// New returns a Server delivering to inbox.
func New(cfg Config, inbox Inbox) *Server {
	if cfg.Hostname == "" {
		cfg.Hostname = "localhost"
		if len(cfg.Domains) > 0 {
			cfg.Hostname = cfg.Domains[0]
		}
	}
	if cfg.MaxMessageBytes <= 0 {
		cfg.MaxMessageBytes = 10 << 20
	}
	if cfg.MaxRecipients <= 0 {
		cfg.MaxRecipients = 100
	}
	if cfg.MaxConnections <= 0 {
		cfg.MaxConnections = 1000
	}
	if cfg.MaxCommands <= 0 {
		cfg.MaxCommands = 1000
	}
	if cfg.ReadTimeout <= 0 {
		cfg.ReadTimeout = 60 * time.Second
	}
	if cfg.DataTimeout <= 0 {
		cfg.DataTimeout = 5 * time.Minute
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	if cfg.Clock == nil {
		cfg.Clock = clock.Real{}
	}
	return &Server{cfg: cfg, inbox: inbox, sessions: map[*session]struct{}{}}
}

// Serve accepts connections on l until Shutdown. It returns nil after a
// clean shutdown.
func (s *Server) Serve(l net.Listener) error {
	s.mu.Lock()
	if s.closing {
		s.mu.Unlock()
		_ = l.Close()
		return nil
	}
	s.listener = l
	s.mu.Unlock()
	for {
		conn, err := l.Accept()
		if err != nil {
			s.mu.Lock()
			closing := s.closing
			s.mu.Unlock()
			if closing {
				return nil
			}
			var ne net.Error
			if errors.As(err, &ne) && ne.Timeout() {
				time.Sleep(50 * time.Millisecond)
				continue
			}
			return err
		}
		sess := &session{srv: s, conn: conn}
		s.mu.Lock()
		if s.closing {
			s.mu.Unlock()
			_ = conn.Close()
			continue
		}
		busy := len(s.sessions) >= s.cfg.MaxConnections
		if !busy {
			s.sessions[sess] = struct{}{}
			s.wg.Add(1)
		}
		s.mu.Unlock()
		if busy {
			s.cfg.Logger.Warn("smtp connection refused", "remote", conn.RemoteAddr().String(), "reason", "too_many_connections")
			_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			_, _ = io.WriteString(conn, "421 4.3.2 Too many connections, try again later\r\n")
			_ = conn.Close()
			continue
		}
		go func() {
			defer s.wg.Done()
			sess.run()
			s.mu.Lock()
			delete(s.sessions, sess)
			s.mu.Unlock()
		}()
	}
}

// Shutdown stops accepting connections, closes idle sessions, lets sessions
// that are receiving a message finish, and waits for all of them. When ctx
// ends first the remaining connections are closed forcibly.
func (s *Server) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	s.closing = true
	if s.listener != nil {
		_ = s.listener.Close()
	}
	for sess := range s.sessions {
		if !sess.inData.Load() {
			_ = sess.conn.Close()
		}
	}
	s.mu.Unlock()

	done := make(chan struct{})
	go func() { s.wg.Wait(); close(done) }()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		s.mu.Lock()
		for sess := range s.sessions {
			_ = sess.conn.Close()
		}
		s.mu.Unlock()
		return ctx.Err()
	}
}

func (s *Server) isClosing() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closing
}

type rcpt struct {
	original string
	mailbox  string
}

type session struct {
	srv    *Server
	conn   net.Conn
	r      *bufio.Reader
	inData atomic.Bool

	ip      string
	helo    string
	from    string
	hasFrom bool
	rcpts   []rcpt
}

func (s *session) reply(format string, args ...any) {
	_ = s.conn.SetWriteDeadline(time.Now().Add(30 * time.Second))
	_, _ = fmt.Fprintf(s.conn, format+"\r\n", args...)
}

func (s *session) reset() {
	s.from, s.hasFrom, s.rcpts = "", false, nil
}

func isTimeout(err error) bool {
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}

var errLineTooLong = errors.New("line too long")

func (s *session) readLine() (string, error) {
	line, isPrefix, err := s.r.ReadLine()
	if err != nil {
		return "", err
	}
	if isPrefix {
		for isPrefix && err == nil {
			_, isPrefix, err = s.r.ReadLine()
		}
		return "", errLineTooLong
	}
	return string(line), nil
}

func (s *session) run() {
	defer s.conn.Close()
	cfg := s.srv.cfg
	host, _, err := net.SplitHostPort(s.conn.RemoteAddr().String())
	if err != nil {
		host = s.conn.RemoteAddr().String()
	}
	s.ip = host
	if s.srv.isClosing() {
		return
	}
	if !cfg.ConnLimiter.Allow(s.ip) {
		cfg.Logger.Warn("smtp connection refused", "remote", s.ip, "reason", "rate_limited")
		s.reply("421 4.7.0 Too many connections from your address, try again later")
		return
	}
	s.r = bufio.NewReaderSize(s.conn, 4096)
	s.reply("220 %s ESMTP phantom-mail ready", cfg.Hostname)

	for commands := 0; ; commands++ {
		if commands >= cfg.MaxCommands {
			s.reply("421 4.7.0 Too many commands in one session, closing connection")
			return
		}
		_ = s.conn.SetReadDeadline(time.Now().Add(cfg.ReadTimeout))
		line, err := s.readLine()
		if err != nil {
			switch {
			case errors.Is(err, errLineTooLong):
				s.reply("500 5.5.2 Line too long")
				continue
			case isTimeout(err):
				s.reply("421 4.4.2 Timeout, closing connection")
			}
			return
		}
		if !s.command(line) {
			return
		}
		if s.srv.isClosing() {
			return
		}
	}
}

// command handles one command line and reports whether the session continues.
func (s *session) command(line string) bool {
	verb, arg := line, ""
	if i := strings.IndexByte(line, ' '); i >= 0 {
		verb, arg = line[:i], strings.TrimSpace(line[i+1:])
	}
	switch strings.ToUpper(verb) {
	case "HELO", "EHLO":
		if arg == "" {
			s.reply("501 5.5.4 Syntax: %s hostname", strings.ToUpper(verb))
			return true
		}
		s.reset()
		s.helo = sanitize(arg, 255)
		if strings.EqualFold(verb, "EHLO") {
			s.reply("250-%s greets %s", s.srv.cfg.Hostname, s.ip)
			s.reply("250-SIZE %d", s.srv.cfg.MaxMessageBytes)
			s.reply("250 8BITMIME")
		} else {
			s.reply("250 %s", s.srv.cfg.Hostname)
		}
	case "MAIL":
		s.mail(arg)
	case "RCPT":
		s.rcpt(arg)
	case "DATA":
		return s.data()
	case "RSET":
		s.reset()
		s.reply("250 2.0.0 Ok")
	case "NOOP":
		s.reply("250 2.0.0 Ok")
	case "VRFY":
		s.reply("252 2.5.2 Cannot VRFY user, but will accept message")
	case "QUIT":
		s.reply("221 2.0.0 Bye")
		return false
	case "AUTH", "STARTTLS", "EXPN", "TURN", "ETRN":
		s.reply("502 5.5.1 Command not implemented")
	default:
		s.reply("500 5.5.2 Command not recognized")
	}
	return true
}

func hasControl(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 || s[i] == 0x7f {
			return true
		}
	}
	return false
}

func sanitize(s string, max int) string {
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s)
	if len(s) > max {
		s = s[:max]
	}
	return s
}

// splitPath parses "<addr> params..." as in MAIL FROM:/RCPT TO: arguments.
func splitPath(arg string) (addr string, params []string, ok bool) {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		return "", nil, false
	}
	if arg[0] == '<' {
		end := strings.IndexByte(arg, '>')
		if end < 0 {
			return "", nil, false
		}
		addr = arg[1:end]
		params = strings.Fields(arg[end+1:])
	} else {
		f := strings.Fields(arg)
		addr, params = f[0], f[1:]
	}
	return addr, params, !hasControl(addr)
}

func (s *session) mail(arg string) {
	if s.hasFrom {
		s.reply("503 5.5.1 Nested MAIL command")
		return
	}
	if len(arg) < 5 || !strings.EqualFold(arg[:5], "FROM:") {
		s.reply("501 5.5.4 Syntax: MAIL FROM:<address>")
		return
	}
	addr, params, ok := splitPath(arg[5:])
	if !ok {
		s.reply("501 5.1.7 Bad sender address syntax")
		return
	}
	cfg := s.srv.cfg
	for _, p := range params {
		if len(p) > 5 && strings.EqualFold(p[:5], "SIZE=") {
			n, err := strconv.ParseInt(p[5:], 10, 64)
			if err != nil || n < 0 {
				s.reply("501 5.5.4 Bad SIZE parameter")
				return
			}
			if n > cfg.MaxMessageBytes {
				s.reply("552 5.3.4 Message size exceeds limit of %d bytes", cfg.MaxMessageBytes)
				return
			}
		}
	}
	if !cfg.MessageLimiter.Allow(s.ip) {
		cfg.Logger.Warn("smtp message refused", "remote", s.ip, "reason", "rate_limited")
		s.reply("451 4.7.1 Rate limit exceeded, try again later")
		return
	}
	s.from, s.hasFrom = addr, true
	s.reply("250 2.1.0 Ok")
}

func (s *session) rcpt(arg string) {
	if !s.hasFrom {
		s.reply("503 5.5.1 Need MAIL before RCPT")
		return
	}
	if len(arg) < 3 || !strings.EqualFold(arg[:3], "TO:") {
		s.reply("501 5.5.4 Syntax: RCPT TO:<address>")
		return
	}
	cfg := s.srv.cfg
	addr, _, ok := splitPath(arg[3:])
	if !ok {
		s.reply("501 5.1.3 Bad recipient address syntax")
		return
	}
	local, domain, ok := mailbox.SplitAddress(addr)
	if !ok {
		s.reply("550 5.1.3 Invalid recipient address")
		return
	}
	if !mailbox.DomainServed(cfg.Domains, domain) {
		cfg.Logger.Info("recipient rejected", "remote", s.ip, "reason", "relay_denied", "domain", sanitize(domain, 100))
		s.reply("550 5.7.1 Relay access denied")
		return
	}
	box, err := mailbox.Normalize(local)
	if err != nil {
		s.reply("550 5.1.1 Invalid mailbox name")
		return
	}
	known := false
	for _, r := range s.rcpts {
		if r.mailbox == box {
			known = true
		}
	}
	if len(s.rcpts) >= cfg.MaxRecipients {
		s.reply("452 4.5.3 Too many recipients")
		return
	}
	if err := s.srv.inbox.Accepts(box); err != nil {
		cfg.Logger.Warn("recipient deferred", "remote", s.ip, "mailbox", box, "error", err.Error())
		s.reply("452 4.5.3 Mailbox limit reached, try again later")
		return
	}
	if !known && !cfg.MailboxLimiter.Allow(box) {
		cfg.Logger.Warn("recipient deferred", "remote", s.ip, "mailbox", box, "reason", "rate_limited")
		s.reply("451 4.7.1 Too much mail for this mailbox, try again later")
		return
	}
	s.rcpts = append(s.rcpts, rcpt{original: addr, mailbox: box})
	s.reply("250 2.1.5 Ok")
}

// data reads the message and delivers it. It reports whether the session
// continues.
func (s *session) data() bool {
	if !s.hasFrom || len(s.rcpts) == 0 {
		s.reply("503 5.5.1 Need MAIL and RCPT before DATA")
		return true
	}
	cfg := s.srv.cfg
	s.inData.Store(true)
	defer s.inData.Store(false)
	s.reply("354 End data with <CR><LF>.<CR><LF>")
	_ = s.conn.SetReadDeadline(time.Now().Add(cfg.DataTimeout))

	var buf []byte
	var total int64
	tooBig := false
	for {
		line, over, err := s.readDataLine()
		if err != nil {
			if isTimeout(err) {
				s.reply("421 4.4.2 Timeout, closing connection")
			}
			return false
		}
		if line == "." && !over {
			break
		}
		if len(line) > 1 && line[0] == '.' {
			line = line[1:] // RFC 5321 dot-unstuffing
		}
		total += int64(len(line)) + 2
		if over || total > cfg.MaxMessageBytes {
			tooBig = true
			buf = nil // stop accumulating but keep draining to the terminator
			continue
		}
		if !tooBig {
			buf = append(buf, line...)
			buf = append(buf, '\r', '\n')
		}
	}

	from, rcpts := s.from, s.rcpts
	s.reset()
	if tooBig {
		cfg.Logger.Warn("message rejected", "remote", s.ip, "reason", "too_big", "limit", cfg.MaxMessageBytes)
		s.reply("552 5.3.4 Message too big")
		return true
	}

	received := fmt.Sprintf("Received: from %s ([%s]) by %s with ESMTP; %s\r\n",
		s.helo, s.ip, cfg.Hostname, cfg.Clock.Now().UTC().Format("Mon, 02 Jan 2006 15:04:05 -0700"))
	returnPath := "Return-Path: <" + from + ">\r\n"

	// Group original recipients by mailbox so each mailbox gets one copy.
	order := []string{}
	byBox := map[string][]string{}
	for _, r := range rcpts {
		if _, ok := byBox[r.mailbox]; !ok {
			order = append(order, r.mailbox)
		}
		byBox[r.mailbox] = append(byBox[r.mailbox], r.original)
	}
	for _, box := range order {
		var hdr strings.Builder
		hdr.WriteString(returnPath)
		for _, o := range byBox[box] {
			hdr.WriteString("X-Original-To: " + o + "\r\n")
		}
		hdr.WriteString(received)
		raw := append([]byte(hdr.String()), buf...)
		if err := s.srv.inbox.Deliver(box, raw); err != nil {
			cfg.Logger.Error("delivery failed", "remote", s.ip, "mailbox", box, "error", err.Error())
			s.reply("452 4.3.1 Insufficient system storage, try again later")
			return true
		}
	}
	cfg.Logger.Info("message accepted", "remote", s.ip, "from", sanitize(from, 200),
		"mailboxes", len(order), "recipients", len(rcpts), "bytes", len(buf))
	s.reply("250 2.0.0 Ok: queued")
	return true
}

// readDataLine reads one line of the DATA section and returns it without its
// line ending. A line longer than the message limit is discarded as it is
// read (over = true) so memory use stays bounded.
func (s *session) readDataLine() (line string, over bool, err error) {
	var sb strings.Builder
	limit := int(s.srv.cfg.MaxMessageBytes) + 1024
	for {
		chunk, err := s.r.ReadSlice('\n')
		if !over {
			if sb.Len()+len(chunk) > limit {
				over = true
				sb.Reset()
			} else {
				sb.Write(chunk)
			}
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		if err != nil {
			return "", over, err
		}
		return strings.TrimRight(sb.String(), "\r\n"), over, nil
	}
}
