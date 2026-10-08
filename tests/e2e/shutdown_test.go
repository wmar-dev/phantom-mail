package e2e

import (
	"bytes"
	"fmt"
	"net"
	"net/http"
	"net/textproto"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// User Story 4: when the platform stops a container it sends SIGTERM. A
// message that is mid-transfer must still be accepted and persisted, new
// connections must be refused, and the process must exit cleanly in time.
func TestUS4_GracefulShutdownOnSIGTERM(t *testing.T) {
	bin := buildBinary(t)
	httpPort, smtpPort := freePort(t), freePort(t)
	data := filepath.Join(t.TempDir(), "data")
	cmd := exec.Command(bin)
	cmd.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		fmt.Sprintf("PM_HTTP_ADDR=127.0.0.1:%d", httpPort),
		fmt.Sprintf("PM_SMTP_ADDR=127.0.0.1:%d", smtpPort),
		"PM_DATA_DIR=" + data,
	}
	var logs bytes.Buffer
	cmd.Stdout, cmd.Stderr = &logs, &logs
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	t.Cleanup(func() {
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
		}
	})

	smtpAddr := fmt.Sprintf("127.0.0.1:%d", smtpPort)
	var conn net.Conn
	for deadline := time.Now().Add(10 * time.Second); ; {
		c, err := net.Dial("tcp", smtpAddr)
		if err == nil {
			conn = c
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("server did not start:\n%s", logs.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(20 * time.Second))
	tp := textproto.NewConn(conn)
	expect := func(code int) {
		t.Helper()
		if _, _, err := tp.ReadResponse(code); err != nil {
			t.Fatalf("expected %d: %v", code, err)
		}
	}
	expect(220)
	send := func(line string, code int) {
		t.Helper()
		if err := tp.PrintfLine("%s", line); err != nil {
			t.Fatal(err)
		}
		expect(code)
	}
	send("EHLO t", 250)
	send("MAIL FROM:<s@example.com>", 250)
	send("RCPT TO:<drain@localhost>", 250)
	send("DATA", 354)
	_ = tp.PrintfLine("Subject: in flight during shutdown")
	_ = tp.PrintfLine("")
	_ = tp.PrintfLine("first half of the body")

	// The platform asks us to stop while the message is half transferred.
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)

	// New SMTP connections are refused (or never greeted).
	if c, err := net.DialTimeout("tcp", smtpAddr, 500*time.Millisecond); err == nil {
		_ = c.SetDeadline(time.Now().Add(500 * time.Millisecond))
		if _, _, err := textproto.NewConn(c).ReadResponse(220); err == nil {
			t.Error("a new SMTP session was accepted after SIGTERM")
		}
		c.Close()
	}
	// The process is still alive: it is draining the in-flight message.
	select {
	case err := <-exited:
		t.Fatalf("process exited before the in-flight message finished: %v\n%s", err, logs.String())
	default:
	}

	// Finish the message: it must be accepted.
	_ = tp.PrintfLine("second half of the body")
	_ = tp.PrintfLine(".")
	expect(250)

	select {
	case err := <-exited:
		if err != nil {
			t.Fatalf("exit status: %v\n%s", err, logs.String())
		}
	case <-time.After(15 * time.Second):
		t.Fatal("process did not exit after the last message was accepted")
	}

	files, _ := filepath.Glob(filepath.Join(data, "drain", "*.eml"))
	if len(files) != 1 {
		t.Fatalf("the in-flight message was not persisted: %v", files)
	}
	raw, _ := os.ReadFile(files[0])
	if !strings.Contains(string(raw), "second half of the body") {
		t.Fatalf("persisted message is incomplete:\n%s", raw)
	}
	if _, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/healthz", httpPort)); err == nil {
		t.Error("HTTP port still answering after exit")
	}
}

func TestUS4_SIGINTAlsoShutsDownCleanly(t *testing.T) {
	bin := buildBinary(t)
	cmd := exec.Command(bin)
	cmd.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		fmt.Sprintf("PM_HTTP_ADDR=127.0.0.1:%d", freePort(t)),
		fmt.Sprintf("PM_SMTP_ADDR=127.0.0.1:%d", freePort(t)),
		"PM_STORAGE=memory",
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(500 * time.Millisecond)
	_ = cmd.Process.Signal(syscall.SIGINT)
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("exit: %v", err)
		}
	case <-time.After(10 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("no exit after SIGINT")
	}
}
