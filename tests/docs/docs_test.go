// Package docs verifies the documentation: runnable examples really run,
// every setting and endpoint is documented, links resolve, and the
// dependency policy holds. Any code block whose fence reads "bash verify"
// is executed against a freshly started instance.
package docs

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

const root = "../.."

type block struct {
	file string
	line int
	text string
}

// docFiles are the files whose examples are executed and whose links are checked.
func docFiles(t *testing.T) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(root, "docs", "*.md"))
	if err != nil {
		t.Fatal(err)
	}
	files = append(files, filepath.Join(root, "README.md"), filepath.Join(root, "specs", "001-disposable-inbox-service", "quickstart.md"))
	sort.Strings(files)
	return files
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// verifiedBlocks returns the blocks fenced as "bash verify", in file order.
func verifiedBlocks(t *testing.T, path string) []block {
	t.Helper()
	var out []block
	sc := bufio.NewScanner(strings.NewReader(readFile(t, path)))
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	n, in := 0, false
	var cur block
	var body []string
	for sc.Scan() {
		n++
		l := sc.Text()
		switch {
		case !in && strings.HasPrefix(l, "```"):
			in = true
			info := strings.Fields(strings.TrimPrefix(l, "```"))
			keep := len(info) >= 2 && info[0] == "bash" && info[1] == "verify"
			cur, body = block{file: path, line: n}, nil
			if !keep {
				cur.line = -1
			}
		case in && strings.HasPrefix(l, "```"):
			in = false
			if cur.line > 0 {
				cur.text = strings.Join(body, "\n") + "\n"
				out = append(out, cur)
			}
		case in:
			body = append(body, l)
		}
	}
	return out
}

var (
	buildOnce sync.Once
	binPath   string
	buildErr  error
)

func binary(t *testing.T) string {
	t.Helper()
	buildOnce.Do(func() {
		dir, err := os.MkdirTemp("", "pm-docs-")
		if err != nil {
			buildErr = err
			return
		}
		binPath = filepath.Join(dir, "phantom-mail")
		cmd := exec.Command("go", "build", "-o", binPath, "phantom-mail/cmd/phantom-mail")
		cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
		if out, err := cmd.CombinedOutput(); err != nil {
			buildErr = fmt.Errorf("go build: %v\n%s", err, out)
		}
	})
	if buildErr != nil {
		t.Fatal(buildErr)
	}
	return binPath
}

func freeAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().String()
}

// startInstance runs the real binary on free ports with the default (local) configuration.
func startInstance(t *testing.T) (httpAddr, smtpAddr string) {
	t.Helper()
	httpAddr, smtpAddr = freeAddr(t), freeAddr(t)
	cmd := exec.Command(binary(t))
	cmd.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		"PM_HTTP_ADDR=" + httpAddr,
		"PM_SMTP_ADDR=" + smtpAddr,
		"PM_DATA_DIR=" + t.TempDir(),
	}
	var logs bytes.Buffer
	cmd.Stdout, cmd.Stderr = &logs, &logs
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Signal(os.Interrupt)
		done := make(chan struct{})
		go func() { _ = cmd.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			_ = cmd.Process.Kill()
		}
	})
	for deadline := time.Now().Add(10 * time.Second); ; {
		if resp, err := http.Get("http://" + httpAddr + "/healthz"); err == nil {
			resp.Body.Close()
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("instance did not start:\n%s", logs.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// Every command in the documentation that is marked "bash verify" must succeed
// against a real instance. The blocks of one file run in order against one
// instance, because later examples read what earlier ones sent.
func TestVerifiedExamplesRun(t *testing.T) {
	if testing.Short() {
		t.Skip("short mode")
	}
	total := 0
	for _, file := range docFiles(t) {
		blocks := verifiedBlocks(t, file)
		if len(blocks) == 0 {
			continue
		}
		rel, _ := filepath.Rel(root, file)
		t.Run(rel, func(t *testing.T) {
			httpAddr, smtpAddr := startInstance(t)
			for _, b := range blocks {
				total++
				name := fmt.Sprintf("line_%d", b.line)
				t.Run(name, func(t *testing.T) {
					for _, tool := range []string{"curl", "jq", "go", "grep", "xargs"} {
						if regexp.MustCompile(`\b` + tool + `\b`).MatchString(b.text) {
							if _, err := exec.LookPath(tool); err != nil {
								t.Skipf("%s is not installed here (the Docker test image has it)", tool)
							}
						}
					}
					script := strings.ReplaceAll(b.text, "localhost:8080", httpAddr)
					script = strings.ReplaceAll(script, "localhost:2525", smtpAddr)
					ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
					defer cancel()
					cmd := exec.CommandContext(ctx, "bash", "-e", "-o", "pipefail", "-c", script)
					cmd.Dir = root
					cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + os.Getenv("HOME"), "GOCACHE=" + os.Getenv("GOCACHE"), "GOFLAGS=" + os.Getenv("GOFLAGS")}
					if out, err := cmd.CombinedOutput(); err != nil {
						t.Fatalf("%s:%d failed: %v\n--- script ---\n%s--- output ---\n%s", rel, b.line, err, script, out)
					}
				})
			}
		})
	}
	if total == 0 {
		t.Fatal("no 'bash verify' blocks found; the documentation examples are not being verified")
	}
	t.Logf("executed %d verified examples", total)
}

// ---- settings ----

var settingRE = regexp.MustCompile(`PM_[A-Z][A-Z_]*[A-Z]`)

// testOnly settings are not part of the service configuration.
var testOnly = map[string]bool{"PM_SKIP_DOCKER_TESTS": true, "PM_PERF_STRICT": true}

func tokens(text string) map[string]bool {
	out := map[string]bool{}
	for _, loc := range settingRE.FindAllStringIndex(text, -1) {
		if loc[1] < len(text) && (text[loc[1]] == '_' || text[loc[1]] == '*') {
			continue // a family such as PM_RATE_*
		}
		out[text[loc[0]:loc[1]]] = true
	}
	return out
}

func TestEverySettingIsDocumentedAndNothingExtraIs(t *testing.T) {
	inCode := tokens(readFile(t, filepath.Join(root, "internal", "config", "config.go")))
	if len(inCode) < 15 {
		t.Fatalf("found only %d settings in config.go", len(inCode))
	}
	ref := readFile(t, filepath.Join(root, "docs", "configuration.md"))
	for name := range inCode {
		if !strings.Contains(ref, "### "+name+"\n") {
			t.Errorf("%s has no '### %s' section in docs/configuration.md", name, name)
		}
		if !strings.Contains(ref, "| [`"+name+"`](#"+strings.ToLower(name)+")") {
			t.Errorf("%s is missing from the quick-reference table in docs/configuration.md", name)
		}
	}
	for _, file := range docFiles(t) {
		for name := range tokens(readFile(t, file)) {
			if !inCode[name] && !testOnly[name] {
				rel, _ := filepath.Rel(root, file)
				t.Errorf("%s mentions %s, which is not a setting in config.go", rel, name)
			}
		}
	}
}

// ---- API ----

func contractRoutes(t *testing.T) []string {
	t.Helper()
	src := readFile(t, filepath.Join(root, "specs", "001-disposable-inbox-service", "contracts", "openapi.yaml"))
	var routes []string
	inPaths, path := false, ""
	method := regexp.MustCompile(`^    (get|post|put|delete|patch):\s*$`)
	for _, line := range strings.Split(src, "\n") {
		switch {
		case line == "paths:":
			inPaths = true
		case inPaths && line != "" && line[0] != ' ':
			inPaths = false
		case inPaths && strings.HasPrefix(line, "  /") && strings.HasSuffix(line, ":"):
			path = strings.TrimSuffix(strings.TrimSpace(line), ":")
		case inPaths && method.MatchString(line):
			routes = append(routes, strings.ToUpper(method.FindStringSubmatch(line)[1])+" "+path)
		}
	}
	return routes
}

func TestEveryAPIRouteIsDocumented(t *testing.T) {
	routes := contractRoutes(t)
	if len(routes) < 10 {
		t.Fatalf("parsed only %d routes from the contract", len(routes))
	}
	api := readFile(t, filepath.Join(root, "docs", "api.md"))
	for _, r := range routes {
		if !strings.Contains(api, "`"+r+"`") {
			t.Errorf("docs/api.md does not document `%s`", r)
		}
	}
	for _, code := range []string{"invalid_mailbox", "bad_request", "unauthorized", "not_found", "rate_limited", "internal"} {
		if !strings.Contains(api, "`"+code+"`") {
			t.Errorf("docs/api.md does not describe the error code %s", code)
		}
	}
}

// ---- links ----

var (
	linkRE    = regexp.MustCompile(`\]\(([^)\s]+)\)`)
	headingRE = regexp.MustCompile(`(?m)^#{1,6}\s+(.+?)\s*$`)
)

// slug approximates GitHub's heading anchors.
func slug(h string) string {
	h = strings.ToLower(strings.TrimSpace(h))
	h = strings.NewReplacer("`", "", "*", "").Replace(h)
	var b strings.Builder
	for _, r := range h {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		case r == ' ':
			b.WriteRune('-')
		}
	}
	return b.String()
}

func anchors(t *testing.T, path string) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	for _, m := range headingRE.FindAllStringSubmatch(readFile(t, path), -1) {
		out[slug(m[1])] = true
	}
	return out
}

func TestLinksAndAnchorsResolve(t *testing.T) {
	for _, file := range docFiles(t) {
		text := readFile(t, file)
		rel, _ := filepath.Rel(root, file)
		for _, m := range linkRE.FindAllStringSubmatch(text, -1) {
			target := m[1]
			if strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://") || strings.HasPrefix(target, "mailto:") {
				continue
			}
			path, anchor, _ := strings.Cut(target, "#")
			dest := file
			if path != "" {
				dest = filepath.Join(filepath.Dir(file), path)
				if _, err := os.Stat(dest); err != nil {
					t.Errorf("%s links to %s, which does not exist", rel, target)
					continue
				}
			}
			if anchor != "" && strings.HasSuffix(dest, ".md") && !anchors(t, dest)[anchor] {
				t.Errorf("%s links to %s but that file has no heading '#%s'", rel, target, anchor)
			}
		}
	}
}

func TestIndexLinksEveryDocument(t *testing.T) {
	index := readFile(t, filepath.Join(root, "docs", "index.md"))
	files, _ := filepath.Glob(filepath.Join(root, "docs", "*.md"))
	for _, f := range files {
		name := filepath.Base(f)
		if name != "index.md" && !strings.Contains(index, "]("+name+")") {
			t.Errorf("docs/index.md does not link to %s", name)
		}
	}
}

// ---- policy and examples ----

func TestNoThirdPartyDependencies(t *testing.T) {
	mod := readFile(t, filepath.Join(root, "go.mod"))
	if strings.Contains(mod, "require") {
		t.Errorf("go.mod has require lines; the project uses only the standard library:\n%s", mod)
	}
	if b, err := os.ReadFile(filepath.Join(root, "go.sum")); err == nil && len(bytes.TrimSpace(b)) > 0 {
		t.Error("go.sum is not empty")
	}
}

func TestProductionComposeExampleIsValid(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not installed")
	}
	cmd := exec.Command("docker", "compose", "-f", filepath.Join(root, "docs", "examples", "compose.prod.yaml"), "config", "-q")
	if out, err := cmd.CombinedOutput(); err != nil {
		if strings.Contains(string(out), "daemon") || strings.Contains(string(out), "not a docker command") {
			t.Skipf("docker compose unavailable: %s", out)
		}
		t.Fatalf("compose.prod.yaml is invalid: %v\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(root, "docs", "examples", "Caddyfile")); err != nil {
		t.Fatal("docs/examples/Caddyfile referenced by compose.prod.yaml is missing")
	}
}
