package httpapi

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"phantom-mail/internal/hub"
	"phantom-mail/internal/inbox"
	"phantom-mail/internal/store"
	"phantom-mail/internal/store/memory"
)

type env struct {
	t   *testing.T
	svc *inbox.Service
	hub *hub.Hub
	srv *Server
	ts  *httptest.Server
}

func newEnv(t *testing.T, mutate ...func(*Config)) *env {
	t.Helper()
	h := hub.New()
	svc := inbox.New(memory.New(store.Options{}), h)
	cfg := Config{HeartbeatInterval: 30 * time.Millisecond}
	for _, m := range mutate {
		m(&cfg)
	}
	srv := New(cfg, svc)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(func() {
		srv.Close()
		ts.Close()
	})
	return &env{t: t, svc: svc, hub: h, srv: srv, ts: ts}
}

func plain(subject, body string) []byte {
	return []byte(fmt.Sprintf("From: Sender <sender@example.com>\r\nTo: alice@localhost\r\nSubject: %s\r\n\r\n%s\r\n", subject, body))
}

func (e *env) deliver(mailbox string, raw []byte) string {
	e.t.Helper()
	if err := e.svc.Deliver(mailbox, raw); err != nil {
		e.t.Fatal(err)
	}
	list, err := e.svc.List(mailbox, "", 1)
	if err != nil || len(list) == 0 {
		e.t.Fatalf("list after deliver: %v %v", list, err)
	}
	return list[0].ID
}

func (e *env) do(method, path string, hdr map[string]string, body io.Reader) *http.Response {
	e.t.Helper()
	req, err := http.NewRequest(method, e.ts.URL+path, body)
	if err != nil {
		e.t.Fatal(err)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	e.t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func (e *env) get(path string) *http.Response { return e.do("GET", path, nil, nil) }

func readJSON(t *testing.T, resp *http.Response, v any) {
	t.Helper()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, v); err != nil {
		t.Fatalf("bad JSON %q: %v", b, err)
	}
}

func readBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

type apiError struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func wantError(t *testing.T, resp *http.Response, status int, code string) {
	t.Helper()
	if resp.StatusCode != status {
		t.Fatalf("status = %d, want %d (body %q)", resp.StatusCode, status, readBody(t, resp))
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("error content type = %q", ct)
	}
	var e apiError
	readJSON(t, resp, &e)
	if e.Error.Code != code || e.Error.Message == "" {
		t.Fatalf("error = %+v, want code %q with a message", e.Error, code)
	}
}
