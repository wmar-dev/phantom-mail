package httpapi

import (
	"mime"
	"net/http"
	"strings"
	"testing"
)

const attachmentMsg = "From: s@example.com\r\nSubject: att\r\nMIME-Version: 1.0\r\nContent-Type: multipart/mixed; boundary=\"b\"\r\n\r\n" +
	"--b\r\nContent-Type: text/plain\r\n\r\nsee attached\r\n" +
	"--b\r\nContent-Type: application/pdf\r\nContent-Disposition: attachment; filename=\"report.pdf\"\r\nContent-Transfer-Encoding: base64\r\n\r\nJVBERi0xLjQ=\r\n" +
	"--b\r\nContent-Type: text/html\r\nContent-Disposition: attachment; filename=\"page.html\"\r\n\r\n<script>alert(1)</script>\r\n--b--\r\n"

func TestAttachmentDownload(t *testing.T) {
	e := newEnv(t)
	id := e.deliver("alice", []byte(attachmentMsg))
	resp := e.get("/api/v1/mailboxes/alice/messages/" + id + "/attachments/0")
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if got := readBody(t, resp); got != "%PDF-1.4" {
		t.Fatalf("body = %q", got)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/pdf" {
		t.Errorf("content type = %q", ct)
	}
	disp, params, err := mime.ParseMediaType(resp.Header.Get("Content-Disposition"))
	if err != nil || disp != "attachment" || params["filename"] != "report.pdf" {
		t.Errorf("content disposition = %q (%v)", resp.Header.Get("Content-Disposition"), err)
	}
	if resp.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Error("missing nosniff")
	}
	if csp := resp.Header.Get("Content-Security-Policy"); !strings.Contains(csp, "sandbox") || !strings.Contains(csp, "default-src 'none'") {
		t.Errorf("CSP = %q", csp)
	}
}

func TestHostileAttachmentIsNeverRenderable(t *testing.T) {
	e := newEnv(t)
	id := e.deliver("alice", []byte(attachmentMsg))
	resp := e.get("/api/v1/mailboxes/alice/messages/" + id + "/attachments/1")
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if !strings.HasPrefix(resp.Header.Get("Content-Disposition"), "attachment") {
		t.Errorf("disposition = %q", resp.Header.Get("Content-Disposition"))
	}
	if resp.Header.Get("X-Content-Type-Options") != "nosniff" || !strings.Contains(resp.Header.Get("Content-Security-Policy"), "sandbox") {
		t.Error("an HTML attachment must carry nosniff and a sandbox policy")
	}
}

func TestAttachmentFilenameCannotInjectHeaders(t *testing.T) {
	e := newEnv(t)
	raw := "From: s@example.com\r\nSubject: evil\r\nMIME-Version: 1.0\r\nContent-Type: multipart/mixed; boundary=\"b\"\r\n\r\n" +
		"--b\r\nContent-Type: text/plain\r\n\r\nx\r\n" +
		"--b\r\nContent-Type: application/octet-stream\r\nContent-Disposition: attachment; filename*=UTF-8''evil%22%0D%0AX-Injected%3A%201%0D%0A.txt\r\n\r\ndata\r\n--b--\r\n"
	id := e.deliver("alice", []byte(raw))
	resp := e.get("/api/v1/mailboxes/alice/messages/" + id + "/attachments/0")
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if resp.Header.Get("X-Injected") != "" {
		t.Fatal("header injection through the attachment filename")
	}
	if _, _, err := mime.ParseMediaType(resp.Header.Get("Content-Disposition")); err != nil {
		t.Fatalf("unparsable Content-Disposition %q: %v", resp.Header.Get("Content-Disposition"), err)
	}
}

func TestAttachmentNonASCIIFilename(t *testing.T) {
	e := newEnv(t)
	raw := "Subject: x\r\nMIME-Version: 1.0\r\nContent-Type: multipart/mixed; boundary=\"b\"\r\n\r\n--b\r\nContent-Type: text/plain\r\n\r\nx\r\n" +
		"--b\r\nContent-Type: application/octet-stream\r\nContent-Disposition: attachment; filename*=UTF-8''r%C3%A9sum%C3%A9.txt\r\n\r\ndata\r\n--b--\r\n"
	id := e.deliver("alice", []byte(raw))
	resp := e.get("/api/v1/mailboxes/alice/messages/" + id + "/attachments/0")
	_, params, err := mime.ParseMediaType(resp.Header.Get("Content-Disposition"))
	if err != nil || params["filename"] != "résumé.txt" {
		t.Fatalf("disposition %q -> %v %v", resp.Header.Get("Content-Disposition"), params, err)
	}
}

func TestAttachmentNotFound(t *testing.T) {
	e := newEnv(t)
	id := e.deliver("alice", []byte(attachmentMsg))
	base := "/api/v1/mailboxes/alice/messages/"
	for _, idx := range []string{"99", "-1", "abc", "1.5"} {
		wantError(t, e.get(base+id+"/attachments/"+idx), 404, "not_found")
	}
	wantError(t, e.get(base+"00000000000000000000000000000000/attachments/0"), 404, "not_found")
	wantError(t, e.get("/api/v1/mailboxes/bad%20name/messages/"+id+"/attachments/0"), 400, "invalid_mailbox")
	_ = http.StatusOK
}
