package integration

import (
	"bufio"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"

	"phantom-mail/internal/clock"
	"phantom-mail/internal/httpapi"
	"phantom-mail/internal/hub"
	"phantom-mail/internal/inbox"
	"phantom-mail/internal/store"
	"phantom-mail/internal/store/memory"
)

const contractPath = "../../specs/001-disposable-inbox-service/contracts/openapi.yaml"

// contractRoutes reads "METHOD /path" pairs from the OpenAPI contract. The
// contract is plain block-style YAML with a fixed layout, so a line scanner
// is enough and avoids a YAML dependency.
func contractRoutes(t *testing.T, src string) []string {
	t.Helper()
	var routes []string
	inPaths := false
	path := ""
	method := regexp.MustCompile(`^    (get|post|put|delete|patch):\s*$`)
	sc := bufio.NewScanner(strings.NewReader(src))
	for sc.Scan() {
		line := sc.Text()
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
	sort.Strings(routes)
	return routes
}

// requiredFields reads `required: [a, b, c]` from a component schema.
func requiredFields(t *testing.T, src, schema string) []string {
	t.Helper()
	lines := strings.Split(src, "\n")
	for i, l := range lines {
		if l == "    "+schema+":" {
			for _, m := range lines[i+1:] {
				if m != "" && !strings.HasPrefix(m, "     ") {
					break
				}
				if strings.Contains(m, "required: [") {
					inner := m[strings.Index(m, "[")+1 : strings.Index(m, "]")]
					var out []string
					for _, f := range strings.Split(inner, ",") {
						out = append(out, strings.TrimSpace(f))
					}
					sort.Strings(out)
					return out
				}
			}
		}
	}
	t.Fatalf("schema %s has no required list", schema)
	return nil
}

func newAPI(t *testing.T) (*httptest.Server, *httpapi.Server, *inbox.Service) {
	t.Helper()
	svc := inbox.New(memory.New(store.Options{Clock: clock.Real{}}), hub.New())
	srv := httpapi.New(httpapi.Config{}, svc)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(func() { srv.Close(); ts.Close() })
	return ts, srv, svc
}

func TestEveryContractPathHasARouteAndViceVersa(t *testing.T) {
	b, err := os.ReadFile(contractPath)
	if err != nil {
		t.Fatal(err)
	}
	want := contractRoutes(t, string(b))
	if len(want) < 10 {
		t.Fatalf("parsed only %d routes from the contract: %v", len(want), want)
	}
	_, srv, _ := newAPI(t)
	got := srv.Routes()
	sort.Strings(got)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("routes differ from the contract.\ncontract:\n  %s\nserver:\n  %s",
			strings.Join(want, "\n  "), strings.Join(got, "\n  "))
	}
}

func TestOpenAPIIsServedAndEqualsTheContract(t *testing.T) {
	b, err := os.ReadFile(contractPath)
	if err != nil {
		t.Fatal(err)
	}
	ts, _, _ := newAPI(t)
	resp, err := http.Get(ts.URL + "/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	served, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 || !strings.Contains(resp.Header.Get("Content-Type"), "yaml") {
		t.Fatalf("GET /openapi.yaml = %d %q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	if string(served) != string(b) {
		t.Fatal("the served OpenAPI document differs from specs/.../contracts/openapi.yaml; copy the contract to internal/httpapi/openapi.yaml")
	}
}

func TestResponsesCarryTheFieldsTheContractRequires(t *testing.T) {
	b, _ := os.ReadFile(contractPath)
	src := string(b)
	ts, _, svc := newAPI(t)
	raw := "X-Original-To: alice@localhost\r\nFrom: s@example.com\r\nSubject: s\r\nMIME-Version: 1.0\r\nContent-Type: multipart/mixed; boundary=b\r\n\r\n--b\r\nContent-Type: text/plain\r\n\r\nhi\r\n--b\r\nContent-Type: application/pdf\r\nContent-Disposition: attachment; filename=a.pdf\r\n\r\nx\r\n--b--\r\n"
	if err := svc.Deliver("alice", []byte(raw)); err != nil {
		t.Fatal(err)
	}
	list := getJSONMap(t, ts.URL+"/api/v1/mailboxes/alice/messages")
	first := list["messages"].([]any)[0].(map[string]any)
	assertKeys(t, "MessageSummary", first, requiredFields(t, src, "MessageSummary"))

	full := getJSONMap(t, ts.URL+"/api/v1/mailboxes/alice/messages/"+first["id"].(string))
	assertKeys(t, "Message", full, append(requiredFields(t, src, "MessageSummary"), requiredFields(t, src, "Message")...))
	att := full["attachments"].([]any)[0].(map[string]any)
	assertKeys(t, "AttachmentInfo", att, requiredFields(t, src, "AttachmentInfo"))

	resp, err := http.Get(ts.URL + "/api/v1/mailboxes/alice/messages/00000000000000000000000000000000")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	errBody := decode(t, resp.Body)
	assertKeys(t, "Error", errBody, requiredFields(t, src, "Error"))

	health := getJSONMap(t, ts.URL+"/healthz")
	assertKeys(t, "health", health, []string{"status", "uptime_seconds", "messages", "store_bytes"})
}
