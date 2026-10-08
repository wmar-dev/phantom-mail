package httpapi

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

type listResp struct {
	Messages []struct {
		ID              string    `json:"id"`
		Mailbox         string    `json:"mailbox"`
		From            string    `json:"from"`
		To              []string  `json:"to"`
		Subject         string    `json:"subject"`
		ReceivedAt      time.Time `json:"received_at"`
		Size            int       `json:"size"`
		AttachmentCount int       `json:"attachment_count"`
	} `json:"messages"`
}

func TestListEmptyMailbox(t *testing.T) {
	e := newEnv(t)
	resp := e.get("/api/v1/mailboxes/nobody/messages")
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("content type = %q", ct)
	}
	body := readBody(t, resp)
	if strings.TrimSpace(body) != `{"messages":[]}` {
		t.Fatalf("body = %q, want an empty array not null", body)
	}
}

func TestListNewestFirstAndFields(t *testing.T) {
	e := newEnv(t)
	var ids []string
	for i := 0; i < 3; i++ {
		ids = append(ids, e.deliver("alice", plain(fmt.Sprintf("m%d", i), "x")))
	}
	var lr listResp
	readJSON(t, e.get("/api/v1/mailboxes/alice/messages"), &lr)
	if len(lr.Messages) != 3 {
		t.Fatalf("messages = %d", len(lr.Messages))
	}
	for i, m := range lr.Messages {
		if m.ID != ids[2-i] {
			t.Fatalf("order wrong at %d: %s", i, m.ID)
		}
	}
	m := lr.Messages[0]
	if m.Mailbox != "alice" || m.Subject != "m2" || m.From != "Sender <sender@example.com>" || m.Size == 0 || m.ReceivedAt.IsZero() {
		t.Fatalf("fields = %+v", m)
	}
	if m.To == nil {
		t.Fatal("to must be an array")
	}
}

func TestListLimitAndAfter(t *testing.T) {
	e := newEnv(t)
	var ids []string
	for i := 0; i < 5; i++ {
		ids = append(ids, e.deliver("alice", plain(fmt.Sprintf("m%d", i), "x")))
	}
	var lr listResp
	readJSON(t, e.get("/api/v1/mailboxes/alice/messages?limit=2"), &lr)
	if len(lr.Messages) != 2 || lr.Messages[0].ID != ids[4] {
		t.Fatalf("limit result = %+v", lr)
	}
	readJSON(t, e.get("/api/v1/mailboxes/alice/messages?after="+ids[2]), &lr)
	if len(lr.Messages) != 2 || lr.Messages[1].ID != ids[3] {
		t.Fatalf("after result = %+v", lr)
	}
}

func TestListRejectsBadParameters(t *testing.T) {
	e := newEnv(t)
	for _, q := range []string{"limit=0", "limit=101", "limit=abc", "limit=-1", "after=not-an-id"} {
		wantError(t, e.get("/api/v1/mailboxes/alice/messages?"+q), 400, "bad_request")
	}
}

func TestInvalidMailboxName(t *testing.T) {
	e := newEnv(t)
	for _, name := range []string{"bad%20name", "a%2Fb", strings.Repeat("a", 65), "a%40b"} {
		wantError(t, e.get("/api/v1/mailboxes/"+name+"/messages"), 400, "invalid_mailbox")
	}
}

func TestMailboxNamesAreNormalized(t *testing.T) {
	e := newEnv(t)
	e.deliver("alice", plain("hello", "x"))
	for _, name := range []string{"Alice", "ALICE", "alice%2Bshop"} {
		var lr listResp
		readJSON(t, e.get("/api/v1/mailboxes/"+name+"/messages"), &lr)
		if len(lr.Messages) != 1 {
			t.Fatalf("%s returned %d messages", name, len(lr.Messages))
		}
	}
}

type getResp struct {
	ID          string   `json:"id"`
	Mailbox     string   `json:"mailbox"`
	Subject     string   `json:"subject"`
	Text        string   `json:"text"`
	HTML        string   `json:"html"`
	To          []string `json:"to"`
	Attachments []struct {
		Index       int    `json:"index"`
		Filename    string `json:"filename"`
		ContentType string `json:"content_type"`
		Size        int    `json:"size"`
	} `json:"attachments"`
	AttachmentCount int `json:"attachment_count"`
}

func TestGetMessage(t *testing.T) {
	e := newEnv(t)
	raw := "X-Original-To: alice+shop@localhost\r\nFrom: s@example.com\r\nSubject: Code\r\nMIME-Version: 1.0\r\n" +
		"Content-Type: multipart/mixed; boundary=\"b\"\r\n\r\n" +
		"--b\r\nContent-Type: multipart/alternative; boundary=\"a\"\r\n\r\n" +
		"--a\r\nContent-Type: text/plain\r\n\r\nplain 482913\r\n" +
		"--a\r\nContent-Type: text/html\r\n\r\n<b>html 482913</b>\r\n--a--\r\n" +
		"--b\r\nContent-Type: application/pdf\r\nContent-Disposition: attachment; filename=\"a.pdf\"\r\nContent-Transfer-Encoding: base64\r\n\r\nJVBERg==\r\n--b--\r\n"
	id := e.deliver("alice", []byte(raw))
	var g getResp
	resp := e.get("/api/v1/mailboxes/alice/messages/" + id)
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	readJSON(t, resp, &g)
	if g.ID != id || g.Subject != "Code" || !strings.Contains(g.Text, "plain 482913") || !strings.Contains(g.HTML, "<b>html 482913</b>") {
		t.Fatalf("message = %+v", g)
	}
	if len(g.To) != 1 || g.To[0] != "alice+shop@localhost" {
		t.Fatalf("original recipient lost: %v", g.To)
	}
	if len(g.Attachments) != 1 || g.Attachments[0].Filename != "a.pdf" || g.Attachments[0].ContentType != "application/pdf" || g.Attachments[0].Size != 4 || g.AttachmentCount != 1 {
		t.Fatalf("attachments = %+v (count %d)", g.Attachments, g.AttachmentCount)
	}
}

func TestGetMissingMessage(t *testing.T) {
	e := newEnv(t)
	e.deliver("alice", plain("x", "y"))
	wantError(t, e.get("/api/v1/mailboxes/alice/messages/00000000000000000000000000000000"), 404, "not_found")
	wantError(t, e.get("/api/v1/mailboxes/alice/messages/not-an-id"), 404, "not_found")
	wantError(t, e.get("/api/v1/mailboxes/nobody/messages/00000000000000000000000000000000"), 404, "not_found")
}

func TestUnknownAPIRouteIsJSON404(t *testing.T) {
	e := newEnv(t)
	wantError(t, e.get("/api/v1/nope"), 404, "not_found")
}

func TestWrongMethodIs405(t *testing.T) {
	e := newEnv(t)
	resp := e.do("PUT", "/api/v1/mailboxes/alice/messages", nil, nil)
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

func TestHealthz(t *testing.T) {
	e := newEnv(t)
	e.deliver("alice", plain("x", "y"))
	resp := e.get("/healthz")
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var h struct {
		Status        string `json:"status"`
		UptimeSeconds int    `json:"uptime_seconds"`
		Messages      int    `json:"messages"`
		StoreBytes    int    `json:"store_bytes"`
	}
	readJSON(t, resp, &h)
	if h.Status != "ok" || h.Messages != 1 || h.StoreBytes == 0 || h.UptimeSeconds < 0 {
		t.Fatalf("health = %+v", h)
	}
}

func TestEveryResponseCarriesBasicSecurityHeaders(t *testing.T) {
	e := newEnv(t)
	id := e.deliver("alice", plain("x", "y"))
	for _, p := range []string{
		"/healthz", "/openapi.yaml", "/api/v1/mailboxes/alice/messages", "/api/v1/mailboxes/alice/messages/" + id,
		"/api/v1/nope", "/api/v1/mailboxes/bad%20name/messages",
	} {
		resp := e.get(p)
		if resp.Header.Get("X-Content-Type-Options") != "nosniff" || resp.Header.Get("Referrer-Policy") != "no-referrer" {
			t.Errorf("GET %s lacks nosniff/no-referrer: %v", p, resp.Header)
		}
	}
}

func TestNoCORSHeadersAreSent(t *testing.T) {
	e := newEnv(t)
	resp := e.do("GET", "/api/v1/mailboxes/alice/messages", map[string]string{"Origin": "https://evil.example"}, nil)
	for h := range resp.Header {
		if strings.HasPrefix(strings.ToLower(h), "access-control-") {
			t.Fatalf("unexpected CORS header %s: other sites must not be able to read the API from a browser", h)
		}
	}
}
