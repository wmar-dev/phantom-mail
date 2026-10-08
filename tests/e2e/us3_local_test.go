package e2e

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// buildBinary compiles the real command once per test binary.
func buildBinary(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "phantom-mail")
	cmd := exec.Command("go", "build", "-o", bin, "phantom-mail/cmd/phantom-mail")
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	return bin
}

// User Story 3: from a clean state the binary starts with nothing but
// environment variables, receives mail, and serves it back, with no cloud
// account, credentials or outbound network access.
func TestUS3_BinaryRunsLocallyFromEnvironmentOnly(t *testing.T) {
	bin := buildBinary(t)
	httpPort, smtpPort := freePort(t), freePort(t)
	dataDir := filepath.Join(t.TempDir(), "data")

	started := time.Now()
	cmd := exec.Command(bin)
	// A deliberately minimal environment: no credentials, no proxy settings.
	cmd.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		fmt.Sprintf("PM_HTTP_ADDR=127.0.0.1:%d", httpPort),
		fmt.Sprintf("PM_SMTP_ADDR=127.0.0.1:%d", smtpPort),
		"PM_DATA_DIR=" + dataDir,
		"PM_DOMAINS=localhost",
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

	base := fmt.Sprintf("http://127.0.0.1:%d", httpPort)
	deadline := time.Now().Add(10 * time.Second)
	for {
		resp, err := http.Get(base + "/healthz")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("server never became healthy; logs:\n%s", logs.String())
		}
		time.Sleep(20 * time.Millisecond)
	}

	msg := "From: Sender <sender@example.com>\r\nTo: dev@localhost\r\nSubject: local test\r\n\r\nhello from the local test\r\n"
	if err := smtpSend(fmt.Sprintf("127.0.0.1:%d", smtpPort), "dev@localhost", msg); err != nil {
		t.Fatalf("delivery: %v\n%s", err, logs.String())
	}
	var list struct {
		Messages []summary `json:"messages"`
	}
	getJSON(t, base+"/api/v1/mailboxes/dev/messages", &list)
	if len(list.Messages) != 1 || list.Messages[0].Subject != "local test" {
		t.Fatalf("list = %+v", list)
	}
	if took := time.Since(started); took > 30*time.Second {
		t.Fatalf("cold start to a readable message took %v, want under 30s", took)
	}
	if files, _ := filepath.Glob(filepath.Join(dataDir, "dev", "*.eml")); len(files) != 1 {
		t.Fatalf("expected one stored message under the data dir, got %v", files)
	}

	// Graceful stop on SIGTERM with a clean exit status.
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-exited:
		if err != nil {
			t.Fatalf("exit: %v\n%s", err, logs.String())
		}
	case <-time.After(10 * time.Second):
		t.Fatal("process did not exit after SIGTERM")
	}

	// Logs are structured JSON on stdout, one object per line.
	sc := bufio.NewScanner(&logs)
	lines := 0
	for sc.Scan() {
		var obj map[string]any
		if err := json.Unmarshal(sc.Bytes(), &obj); err != nil {
			t.Fatalf("log line is not JSON: %q", sc.Text())
		}
		if obj["msg"] == nil || obj["level"] == nil || obj["time"] == nil {
			t.Fatalf("log line lacks msg/level/time: %q", sc.Text())
		}
		lines++
	}
	if lines < 2 {
		t.Fatalf("expected startup and shutdown log lines, got %d", lines)
	}
}

func TestUS3_BadConfigurationFailsFastWithAClearMessage(t *testing.T) {
	bin := buildBinary(t)
	cmd := exec.Command(bin)
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "PM_RETENTION=tomorrow", "PM_STORAGE=s3"}
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatal("expected a non-zero exit for invalid configuration")
	}
	if ee, ok := err.(*exec.ExitError); !ok || ee.ExitCode() != 2 {
		t.Fatalf("exit = %v, want status 2", err)
	}
	for _, want := range []string{"PM_RETENTION", "PM_STORAGE"} {
		if !strings.Contains(string(out), want) {
			t.Errorf("error output does not mention %s:\n%s", want, out)
		}
	}
}
