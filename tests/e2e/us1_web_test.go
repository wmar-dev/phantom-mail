package e2e

import (
	"strings"
	"testing"
	"time"
)

type summary struct {
	ID      string `json:"id"`
	Subject string `json:"subject"`
	Mailbox string `json:"mailbox"`
}

// User Story 1: receive mail for a never-used address and read it in the web
// interface, live.
func TestUS1_ReadVerificationEmailInTheWebInterface(t *testing.T) {
	a := startApp(t)
	base := "http://" + a.HTTPAddr()

	// The web interface is served.
	resp, html := httpGet(t, base+"/")
	if resp.StatusCode != 200 || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/html") {
		t.Fatalf("GET / = %d %q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	for _, want := range []string{"Phantom Mail", "/app.js", "/app.css"} {
		if !strings.Contains(html, want) {
			t.Errorf("index page lacks %q", want)
		}
	}
	for _, asset := range []string{"/app.js", "/lib.js", "/app.css"} {
		r, body := httpGet(t, base+asset)
		if r.StatusCode != 200 || len(body) == 0 {
			t.Errorf("GET %s = %d (%d bytes)", asset, r.StatusCode, len(body))
		}
	}
	if csp := resp.Header.Get("Content-Security-Policy"); !strings.Contains(csp, "default-src") {
		t.Errorf("index page has no CSP: %q", csp)
	}

	// The tester opens the mailbox (live stream first, as the UI does).
	events := sseEvents(t, base+"/api/v1/mailboxes/alice/events")
	var list struct {
		Messages []summary `json:"messages"`
	}
	getJSON(t, base+"/api/v1/mailboxes/alice/messages", &list)
	if len(list.Messages) != 0 {
		t.Fatalf("a never-used mailbox should be empty: %+v", list)
	}

	// Mail arrives for an address nobody registered.
	sendMail(t, a, "alice@localhost", "Your verification code", "Your code is 482913")
	done := time.Now()

	select {
	case ev := <-events:
		if ev[0] != "message" || !strings.Contains(ev[1], "Your verification code") {
			t.Fatalf("event = %v", ev)
		}
		if took := time.Since(done); took > time.Second {
			t.Fatalf("event arrived %v after delivery, want under 1s", took)
		}
	case <-time.After(time.Second):
		t.Fatal("no live event within 1s of delivery")
	}

	getJSON(t, base+"/api/v1/mailboxes/alice/messages", &list)
	if len(list.Messages) != 1 || list.Messages[0].Subject != "Your verification code" {
		t.Fatalf("list = %+v", list)
	}
	var msg struct {
		Text string `json:"text"`
	}
	getJSON(t, base+"/api/v1/mailboxes/alice/messages/"+list.Messages[0].ID, &msg)
	if !strings.Contains(msg.Text, "482913") {
		t.Fatalf("text = %q", msg.Text)
	}

	// A second email appears without reloading.
	sendMail(t, a, "alice@localhost", "Second", "again")
	select {
	case ev := <-events:
		if !strings.Contains(ev[1], "Second") {
			t.Fatalf("event = %v", ev)
		}
	case <-time.After(time.Second):
		t.Fatal("second email did not arrive live")
	}
}
