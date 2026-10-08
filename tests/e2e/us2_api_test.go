package e2e

import (
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"
)

type apiMsg struct {
	ID          string `json:"id"`
	Subject     string `json:"subject"`
	Text        string `json:"text"`
	Attachments []struct {
		Index    int    `json:"index"`
		Filename string `json:"filename"`
	} `json:"attachments"`
}

// User Story 2: a script signs up somewhere, waits for the verification email
// over HTTP only, extracts the code, and cleans up. No manual steps.
func TestUS2_ScriptedVerificationCodeFlow(t *testing.T) {
	a := startApp(t)
	api := "http://" + a.HTTPAddr() + "/api/v1/mailboxes/test-run-7f3a"
	start := time.Now()

	// "Sign up" on another site: it mails a code shortly after the script starts waiting.
	go func() {
		time.Sleep(100 * time.Millisecond)
		sendMail(t, a, "test-run-7f3a@localhost", "Confirm your account", "Your verification code is 904217. It expires in 10 minutes.")
	}()

	var waited struct {
		Messages []apiMsg `json:"messages"`
	}
	resp := getJSON(t, api+"/messages/wait?timeout=10", &waited)
	if resp.StatusCode != 200 || len(waited.Messages) != 1 {
		t.Fatalf("wait = %d %+v", resp.StatusCode, waited)
	}
	var msg apiMsg
	getJSON(t, api+"/messages/"+waited.Messages[0].ID, &msg)
	code := regexp.MustCompile(`\b\d{6}\b`).FindString(msg.Text)
	if code != "904217" {
		t.Fatalf("extracted code %q from %q", code, msg.Text)
	}
	if took := time.Since(start); took > 2*time.Second {
		t.Fatalf("send-to-code took %v, want under 2s", took)
	}

	// Clean up and confirm the mailbox is empty.
	for _, req := range []struct{ method, path string }{
		{"DELETE", "/messages/" + msg.ID},
		{"DELETE", "/messages"},
	} {
		r, _ := http.NewRequest(req.method, api+req.path, nil)
		res, err := http.DefaultClient.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		want := http.StatusNoContent
		if req.path != "/messages" && res.StatusCode != want {
			t.Fatalf("%s %s = %d", req.method, req.path, res.StatusCode)
		}
	}
	var after struct {
		Messages []apiMsg `json:"messages"`
	}
	getJSON(t, api+"/messages", &after)
	if len(after.Messages) != 0 {
		t.Fatalf("mailbox not empty: %+v", after)
	}
}

func TestUS2_AttachmentsErrorsAndTimeouts(t *testing.T) {
	a := startApp(t)
	base := "http://" + a.HTTPAddr()

	raw := "From: s@example.com\r\nTo: files@localhost\r\nSubject: with file\r\nMIME-Version: 1.0\r\nContent-Type: multipart/mixed; boundary=b\r\n\r\n" +
		"--b\r\nContent-Type: text/plain\r\n\r\nattached\r\n--b\r\nContent-Type: text/plain; name=note.txt\r\nContent-Disposition: attachment; filename=note.txt\r\n\r\nhello file\r\n--b--\r\n"
	if err := smtpSend(a.SMTPAddr(), "files@localhost", raw); err != nil {
		t.Fatal(err)
	}
	var list struct {
		Messages []apiMsg `json:"messages"`
	}
	getJSON(t, base+"/api/v1/mailboxes/files/messages", &list)
	if len(list.Messages) != 1 {
		t.Fatalf("list = %+v", list)
	}
	var msg apiMsg
	getJSON(t, base+"/api/v1/mailboxes/files/messages/"+list.Messages[0].ID, &msg)
	if len(msg.Attachments) != 1 || msg.Attachments[0].Filename != "note.txt" {
		t.Fatalf("attachments = %+v", msg.Attachments)
	}
	resp, body := httpGet(t, base+"/api/v1/mailboxes/files/messages/"+msg.ID+"/attachments/0")
	if resp.StatusCode != 200 || strings.TrimSpace(body) != "hello file" {
		t.Fatalf("download = %d %q", resp.StatusCode, body)
	}

	// Errors are clear JSON.
	for path, want := range map[string]int{
		"/api/v1/mailboxes/bad%20name/messages":                             400,
		"/api/v1/mailboxes/files/messages/00000000000000000000000000000000": 404,
		"/api/v1/mailboxes/files/messages?limit=500":                        400,
	} {
		r, b := httpGet(t, base+path)
		if r.StatusCode != want || !strings.Contains(b, `"error"`) {
			t.Errorf("GET %s = %d %q, want %d with an error body", path, r.StatusCode, b, want)
		}
	}

	// An empty mailbox times out cleanly.
	r, _ := httpGet(t, base+"/api/v1/mailboxes/quiet/messages/wait?timeout=1")
	if r.StatusCode != http.StatusNoContent {
		t.Errorf("wait on empty mailbox = %d, want 204", r.StatusCode)
	}
}
