package httpapi

import (
	"net/http"
	"testing"
	"time"
)

func TestDeleteMessage(t *testing.T) {
	e := newEnv(t)
	keep := e.deliver("alice", plain("keep", "x"))
	gone := e.deliver("alice", plain("gone", "x"))
	resp := e.do("DELETE", "/api/v1/mailboxes/alice/messages/"+gone, nil, nil)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	wantError(t, e.do("DELETE", "/api/v1/mailboxes/alice/messages/"+gone, nil, nil), 404, "not_found")
	wantError(t, e.get("/api/v1/mailboxes/alice/messages/"+gone), 404, "not_found")
	var lr listResp
	readJSON(t, e.get("/api/v1/mailboxes/alice/messages"), &lr)
	if len(lr.Messages) != 1 || lr.Messages[0].ID != keep {
		t.Fatalf("list = %+v", lr)
	}
}

func TestDeleteMessageErrors(t *testing.T) {
	e := newEnv(t)
	wantError(t, e.do("DELETE", "/api/v1/mailboxes/alice/messages/not-an-id", nil, nil), 404, "not_found")
	wantError(t, e.do("DELETE", "/api/v1/mailboxes/bad%20name/messages/00000000000000000000000000000000", nil, nil), 400, "invalid_mailbox")
}

func TestEmptyMailbox(t *testing.T) {
	e := newEnv(t)
	for i := 0; i < 3; i++ {
		e.deliver("alice", plain("m", "x"))
	}
	e.deliver("bob", plain("bob's", "x"))
	if resp := e.do("DELETE", "/api/v1/mailboxes/alice/messages", nil, nil); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var lr listResp
	readJSON(t, e.get("/api/v1/mailboxes/alice/messages"), &lr)
	if len(lr.Messages) != 0 {
		t.Fatalf("alice still has %d messages", len(lr.Messages))
	}
	readJSON(t, e.get("/api/v1/mailboxes/bob/messages"), &lr)
	if len(lr.Messages) != 1 {
		t.Fatal("deleting alice's mailbox must not touch bob's")
	}
}

func TestEmptyMailboxIsIdempotent(t *testing.T) {
	e := newEnv(t)
	for i := 0; i < 2; i++ {
		if resp := e.do("DELETE", "/api/v1/mailboxes/never-used/messages", nil, nil); resp.StatusCode != http.StatusNoContent {
			t.Fatalf("attempt %d status = %d", i, resp.StatusCode)
		}
	}
	wantError(t, e.do("DELETE", "/api/v1/mailboxes/bad%20name/messages", nil, nil), 400, "invalid_mailbox")
}

func TestDeleteAnnouncesDeletedEvents(t *testing.T) {
	e := newEnv(t)
	id := e.deliver("alice", plain("x", "y"))
	r := openSSE(t, e, "alice")
	e.do("DELETE", "/api/v1/mailboxes/alice/messages/"+id, nil, nil)
	if data := r.event("deleted", 2*time.Second); data == "" || !contains(data, id) {
		t.Fatalf("data = %q", data)
	}
	id2 := e.deliver("alice", plain("second", "y"))
	r.event("message", 2*time.Second)
	e.do("DELETE", "/api/v1/mailboxes/alice/messages", nil, nil)
	if data := r.event("deleted", 2*time.Second); !contains(data, id2) {
		t.Fatalf("data = %q", data)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
