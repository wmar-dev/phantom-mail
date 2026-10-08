package httpapi

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"
)

func waitURL(box, query string) string {
	return "/api/v1/mailboxes/" + box + "/messages/wait" + query
}

func TestWaitReturnsExistingMessagesImmediately(t *testing.T) {
	e := newEnv(t)
	id := e.deliver("alice", plain("already here", "x"))
	start := time.Now()
	resp := e.get(waitURL("alice", "?timeout=5"))
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var lr listResp
	readJSON(t, resp, &lr)
	if len(lr.Messages) != 1 || lr.Messages[0].ID != id {
		t.Fatalf("messages = %+v", lr)
	}
	if time.Since(start) > time.Second {
		t.Fatal("wait should not block when a message already exists")
	}
}

func TestWaitHoldsUntilAMessageArrives(t *testing.T) {
	e := newEnv(t)
	type result struct {
		resp *http.Response
		took time.Duration
	}
	out := make(chan result, 1)
	go func() {
		start := time.Now()
		out <- result{e.get(waitURL("alice", "?timeout=10")), time.Since(start)}
	}()
	time.Sleep(150 * time.Millisecond)
	id := e.deliver("alice", plain("Your code", "482913"))
	select {
	case r := <-out:
		if r.resp.StatusCode != 200 || r.took > 3*time.Second {
			t.Fatalf("status %d after %v", r.resp.StatusCode, r.took)
		}
		var lr listResp
		readJSON(t, r.resp, &lr)
		if len(lr.Messages) != 1 || lr.Messages[0].ID != id {
			t.Fatalf("messages = %+v", lr)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("wait did not return after a message arrived")
	}
}

func TestWaitTimesOutWith204(t *testing.T) {
	e := newEnv(t)
	start := time.Now()
	resp := e.get(waitURL("alice", "?timeout=1"))
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if took := time.Since(start); took < 900*time.Millisecond || took > 3*time.Second {
		t.Fatalf("returned after %v, want about 1s", took)
	}
	if body := readBody(t, resp); body != "" {
		t.Fatalf("204 must have no body, got %q", body)
	}
}

func TestWaitAfterOnlyCountsNewerMessages(t *testing.T) {
	e := newEnv(t)
	old := e.deliver("alice", plain("old", "x"))
	if resp := e.get(waitURL("alice", "?timeout=1&after="+old)); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want 204 because nothing is newer", resp.StatusCode)
	}
	done := make(chan *http.Response, 1)
	go func() { done <- e.get(waitURL("alice", "?timeout=10&after="+old)) }()
	time.Sleep(100 * time.Millisecond)
	fresh := e.deliver("alice", plain("fresh", "x"))
	resp := <-done
	var lr listResp
	readJSON(t, resp, &lr)
	if len(lr.Messages) != 1 || lr.Messages[0].ID != fresh {
		t.Fatalf("messages = %+v, want only the newer message", lr)
	}
}

func TestWaitRejectsBadParameters(t *testing.T) {
	e := newEnv(t)
	for _, q := range []string{"?timeout=0", "?timeout=61", "?timeout=abc", "?timeout=-3", "?after=not-an-id"} {
		wantError(t, e.get(waitURL("alice", q)), 400, "bad_request")
	}
	wantError(t, e.get(waitURL("bad%20name", "")), 400, "invalid_mailbox")
}

func TestWaitDefaultTimeoutIsAccepted(t *testing.T) {
	e := newEnv(t)
	e.deliver("alice", plain("x", "y"))
	if resp := e.get(waitURL("alice", "")); resp.StatusCode != 200 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

func TestMultipleWaitersAreAllReleased(t *testing.T) {
	e := newEnv(t)
	const n = 5
	var wg sync.WaitGroup
	codes := make([]int, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			codes[i] = e.get(waitURL("alice", "?timeout=10")).StatusCode
		}(i)
	}
	deadline := time.Now().Add(2 * time.Second)
	for e.hub.Subscribers("alice") < n && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	e.deliver("alice", plain("one message", "x"))
	wg.Wait()
	for i, c := range codes {
		if c != 200 {
			t.Fatalf("waiter %d got %d", i, c)
		}
	}
}

func TestCancelledWaitReleasesItsSubscription(t *testing.T) {
	e := newEnv(t)
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, "GET", e.ts.URL+waitURL("alice", "?timeout=30"), nil)
	go func() { _, _ = http.DefaultClient.Do(req) }()
	deadline := time.Now().Add(2 * time.Second)
	for e.hub.Subscribers("alice") != 1 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	deadline = time.Now().Add(2 * time.Second)
	for e.hub.Subscribers("alice") != 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if n := e.hub.Subscribers("alice"); n != 0 {
		t.Fatalf("%d subscriptions leaked", n)
	}
}

func TestWaitEndsWhenServerCloses(t *testing.T) {
	e := newEnv(t)
	done := make(chan int, 1)
	go func() { done <- e.get(waitURL("alice", "?timeout=30")).StatusCode }()
	time.Sleep(150 * time.Millisecond)
	e.srv.Close()
	select {
	case code := <-done:
		if code != http.StatusNoContent {
			t.Fatalf("status = %d", code)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("long poll still open after Close")
	}
}
