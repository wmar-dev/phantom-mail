package httpapi

import (
	"bufio"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

// sseReader parses a text/event-stream response.
type sseReader struct {
	t    *testing.T
	sc   *bufio.Scanner
	resp *http.Response
	ch   chan string
}

func openSSE(t *testing.T, e *env, mailbox string) *sseReader {
	t.Helper()
	resp := e.get("/api/v1/mailboxes/" + mailbox + "/events")
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d: %s", resp.StatusCode, readBody(t, resp))
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("content type = %q", ct)
	}
	r := &sseReader{t: t, sc: bufio.NewScanner(resp.Body), resp: resp, ch: make(chan string, 256)}
	go func() {
		for r.sc.Scan() {
			r.ch <- r.sc.Text()
		}
		close(r.ch)
	}()
	return r
}

// next returns the next line, failing after d.
func (r *sseReader) next(d time.Duration) string {
	r.t.Helper()
	select {
	case l, ok := <-r.ch:
		if !ok {
			r.t.Fatal("event stream closed")
		}
		return l
	case <-time.After(d):
		r.t.Fatal("timed out waiting for event stream data")
	}
	return ""
}

// event reads lines until a complete event with the given name and returns its data.
func (r *sseReader) event(name string, d time.Duration) string {
	r.t.Helper()
	deadline := time.Now().Add(d)
	cur := ""
	for time.Now().Before(deadline) {
		l := r.next(time.Until(deadline))
		switch {
		case strings.HasPrefix(l, "event: "):
			cur = strings.TrimPrefix(l, "event: ")
		case strings.HasPrefix(l, "data: ") && cur == name:
			return strings.TrimPrefix(l, "data: ")
		case l == "":
			cur = ""
		}
	}
	r.t.Fatalf("no %q event", name)
	return ""
}

func TestSSEDeliversNewMessages(t *testing.T) {
	e := newEnv(t)
	r := openSSE(t, e, "alice")
	time.Sleep(50 * time.Millisecond) // subscription is registered before the headers are sent
	id := e.deliver("alice", plain("Your code", "482913"))
	data := r.event("message", 2*time.Second)
	var s struct {
		ID      string `json:"id"`
		Subject string `json:"subject"`
		Mailbox string `json:"mailbox"`
	}
	if err := json.Unmarshal([]byte(data), &s); err != nil {
		t.Fatalf("bad event data %q: %v", data, err)
	}
	if s.ID != id || s.Subject != "Your code" || s.Mailbox != "alice" {
		t.Fatalf("event = %+v", s)
	}
}

func TestSSEOnlyForOwnMailbox(t *testing.T) {
	e := newEnv(t)
	r := openSSE(t, e, "alice")
	e.deliver("bob", plain("not for alice", "x"))
	id := e.deliver("alice", plain("for alice", "x"))
	data := r.event("message", 2*time.Second)
	if !strings.Contains(data, id) || strings.Contains(data, "not for alice") {
		t.Fatalf("data = %q", data)
	}
}

func TestSSEDeletedEvent(t *testing.T) {
	e := newEnv(t)
	id := e.deliver("alice", plain("x", "y"))
	r := openSSE(t, e, "alice")
	time.Sleep(50 * time.Millisecond)
	if err := e.svc.Delete("alice", id); err != nil {
		t.Fatal(err)
	}
	data := r.event("deleted", 2*time.Second)
	if !strings.Contains(data, id) {
		t.Fatalf("data = %q", data)
	}
}

func TestSSEHeartbeat(t *testing.T) {
	e := newEnv(t)
	r := openSSE(t, e, "alice")
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if l := r.next(time.Second); strings.HasPrefix(l, ":") {
			return
		}
	}
	t.Fatal("no heartbeat comment received")
}

func TestSSEClientDisconnectReleasesSubscription(t *testing.T) {
	e := newEnv(t)
	r := openSSE(t, e, "alice")
	deadline := time.Now().Add(time.Second)
	for e.hub.Subscribers("alice") != 1 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if e.hub.Subscribers("alice") != 1 {
		t.Fatal("subscription not registered")
	}
	r.resp.Body.Close()
	deadline = time.Now().Add(2 * time.Second)
	for e.hub.Subscribers("alice") != 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if n := e.hub.Subscribers("alice"); n != 0 {
		t.Fatalf("%d subscriptions still registered after disconnect", n)
	}
}

func TestSSEInvalidMailbox(t *testing.T) {
	e := newEnv(t)
	wantError(t, e.get("/api/v1/mailboxes/bad%20name/events"), 400, "invalid_mailbox")
}

func TestSSEPerClientLimit(t *testing.T) {
	e := newEnv(t, func(c *Config) { c.MaxSSEPerClient = 2 })
	openSSE(t, e, "a")
	openSSE(t, e, "b")
	wantError(t, e.get("/api/v1/mailboxes/c/events"), 429, "rate_limited")
}

func TestServerCloseEndsStreams(t *testing.T) {
	e := newEnv(t)
	r := openSSE(t, e, "alice")
	e.srv.Close()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case _, ok := <-r.ch:
			if !ok {
				return
			}
		case <-deadline:
			t.Fatal("stream still open after Close")
		}
	}
}
