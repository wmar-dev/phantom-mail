package httpapi

import (
	"strings"
	"testing"
)

func TestHTMLEndpointSecurityHeaders(t *testing.T) {
	e := newEnv(t)
	raw := "From: s@example.com\r\nSubject: evil\r\nContent-Type: text/html\r\n\r\n<script>alert(1)</script><img src=\"https://tracker.example/p.gif\"><p>code 123456</p>\r\n"
	id := e.deliver("alice", []byte(raw))
	resp := e.get("/api/v1/mailboxes/alice/messages/" + id + "/html")
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") || !strings.Contains(ct, "utf-8") {
		t.Fatalf("content type = %q", ct)
	}
	csp := resp.Header.Get("Content-Security-Policy")
	for _, want := range []string{"default-src 'none'", "sandbox", "base-uri 'none'", "form-action 'none'"} {
		if !strings.Contains(csp, want) {
			t.Errorf("CSP %q lacks %q", csp, want)
		}
	}
	for _, forbidden := range []string{"script-src", "http:", "https:", "'unsafe-eval'", "allow-scripts", "allow-same-origin", "*"} {
		if strings.Contains(csp, forbidden) {
			t.Errorf("CSP %q must not contain %q", csp, forbidden)
		}
	}
	if got := resp.Header.Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q", got)
	}
	if got := resp.Header.Get("Referrer-Policy"); got != "no-referrer" {
		t.Errorf("Referrer-Policy = %q", got)
	}
	if resp.Header.Get("Set-Cookie") != "" {
		t.Error("the HTML endpoint must not set cookies")
	}
	if body := readBody(t, resp); !strings.Contains(body, "code 123456") {
		t.Errorf("body = %q", body)
	}
}

func TestHTMLEndpointTextOnlyIsEscapedAndWrapped(t *testing.T) {
	e := newEnv(t)
	id := e.deliver("alice", plain("t", "<script>alert(1)</script> & 482913"))
	resp := e.get("/api/v1/mailboxes/alice/messages/" + id + "/html")
	body := readBody(t, resp)
	if strings.Contains(body, "<script>") {
		t.Fatalf("text body was not escaped: %q", body)
	}
	if !strings.Contains(body, "&lt;script&gt;") || !strings.Contains(body, "&amp; 482913") || !strings.Contains(body, "<pre") {
		t.Fatalf("body = %q", body)
	}
}

func TestHTMLEndpointEmptyMessage(t *testing.T) {
	e := newEnv(t)
	id := e.deliver("alice", []byte("Subject: empty\r\n\r\n"))
	resp := e.get("/api/v1/mailboxes/alice/messages/" + id + "/html")
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

func TestHTMLEndpointNotFound(t *testing.T) {
	e := newEnv(t)
	wantError(t, e.get("/api/v1/mailboxes/alice/messages/00000000000000000000000000000000/html"), 404, "not_found")
}
