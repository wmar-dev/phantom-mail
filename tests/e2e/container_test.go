package e2e

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

func docker(t *testing.T, args ...string) (string, error) {
	t.Helper()
	out, err := exec.Command("docker", args...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// The container test needs a Docker daemon (and the base images, which are
// pulled once). It is skipped when Docker is unavailable or when
// PM_SKIP_DOCKER_TESTS is set, so the suite still runs fully offline.
func TestUS3_ContainerImage(t *testing.T) {
	if os.Getenv("PM_SKIP_DOCKER_TESTS") != "" {
		t.Skip("PM_SKIP_DOCKER_TESTS is set")
	}
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not installed")
	}
	if _, err := docker(t, "info"); err != nil {
		t.Skip("docker daemon not reachable")
	}

	const image = "phantom-mail:e2e-test"
	if out, err := docker(t, "build", "--target", "runtime", "-t", image, "../.."); err != nil {
		low := strings.ToLower(out)
		if strings.Contains(low, "failed to resolve") || strings.Contains(low, "pull access denied") ||
			strings.Contains(low, "no such host") || strings.Contains(low, "i/o timeout") || strings.Contains(low, "tls") {
			t.Skipf("cannot reach the registry to fetch base images (offline?):\n%s", out)
		}
		t.Fatalf("docker build failed:\n%s", out)
	}

	// Size: the image should stay tiny.
	sizeStr, err := docker(t, "image", "inspect", image, "--format", "{{.Size}}")
	if err != nil {
		t.Fatal(sizeStr)
	}
	size, _ := strconv.ParseInt(sizeStr, 10, 64)
	if size <= 0 || size > 15*1000*1000 {
		t.Errorf("image size = %d bytes, want under 15 MB", size)
	}
	// Runs as a numeric non-root user.
	user, _ := docker(t, "image", "inspect", image, "--format", "{{.Config.User}}")
	if user == "" || user == "0" || user == "root" || strings.HasPrefix(user, "0:") {
		t.Errorf("image user = %q, want a non-root user", user)
	}

	name := fmt.Sprintf("pm-e2e-%d", time.Now().UnixNano())
	if out, err := docker(t, "run", "-d", "--name", name,
		"-p", "127.0.0.1::8080", "-p", "127.0.0.1::2525",
		"-e", "PM_DOMAINS=localhost",
		"--health-interval=1s", "--health-start-period=1s", "--health-timeout=3s", image); err != nil {
		t.Fatalf("docker run: %s", out)
	}
	t.Cleanup(func() { _, _ = docker(t, "rm", "-f", name) })

	// Becomes healthy through the image's own HEALTHCHECK.
	deadline := time.Now().Add(30 * time.Second)
	for {
		status, _ := docker(t, "inspect", name, "--format", "{{.State.Health.Status}}")
		if status == "healthy" {
			break
		}
		if time.Now().After(deadline) {
			logs, _ := docker(t, "logs", name)
			t.Fatalf("container health = %q; logs:\n%s", status, logs)
		}
		time.Sleep(500 * time.Millisecond)
	}

	// Idle memory stays small (plan: under 20 MB).
	time.Sleep(2 * time.Second)
	if usage, err := docker(t, "stats", "--no-stream", "--format", "{{.MemUsage}}", name); err == nil {
		used := strings.TrimSpace(strings.SplitN(usage, "/", 2)[0])
		mib, ok := parseMiB(used)
		t.Logf("idle container memory: %s", used)
		if !ok {
			t.Logf("could not parse memory usage %q", usage)
		} else if mib > 20 {
			t.Errorf("idle memory = %.1f MiB, want under 20", mib)
		}
	}

	hostAddr := func(port string) string {
		out, err := docker(t, "port", name, port+"/tcp")
		if err != nil || out == "" {
			t.Fatalf("docker port %s: %s", port, out)
		}
		line := strings.Split(out, "\n")[0]
		return "127.0.0.1:" + line[strings.LastIndex(line, ":")+1:]
	}
	httpAddr, smtpAddr := hostAddr("8080"), hostAddr("2525")

	msg := "From: s@example.com\r\nTo: box@localhost\r\nSubject: via container\r\n\r\nhello\r\n"
	if err := smtpSend(smtpAddr, "box@localhost", msg); err != nil {
		t.Fatalf("delivery into the container: %v", err)
	}
	var list struct {
		Messages []summary `json:"messages"`
	}
	resp := getJSON(t, "http://"+httpAddr+"/api/v1/mailboxes/box/messages", &list)
	if resp.StatusCode != http.StatusOK || len(list.Messages) != 1 || list.Messages[0].Subject != "via container" {
		t.Fatalf("container list = %d %+v", resp.StatusCode, list)
	}
}

// parseMiB converts docker stats values such as "3.2MiB", "812KiB" or "1.1GiB".
func parseMiB(v string) (float64, bool) {
	units := []struct {
		suffix string
		toMiB  float64
	}{{"KiB", 1.0 / 1024}, {"MiB", 1}, {"GiB", 1024}, {"kB", 1000.0 / (1 << 20)}, {"MB", 1e6 / (1 << 20)}, {"GB", 1e9 / (1 << 20)}, {"B", 1.0 / (1 << 20)}}
	for _, u := range units {
		if strings.HasSuffix(v, u.suffix) {
			f, err := strconv.ParseFloat(strings.TrimSpace(strings.TrimSuffix(v, u.suffix)), 64)
			return f * u.toMiB, err == nil
		}
	}
	return 0, false
}
