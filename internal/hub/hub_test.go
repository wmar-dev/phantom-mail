package hub

import (
	"runtime"
	"sync"
	"testing"
	"time"

	"phantom-mail/internal/message"
)

func recv(t *testing.T, ch <-chan Event) Event {
	t.Helper()
	select {
	case ev, ok := <-ch:
		if !ok {
			t.Fatal("channel closed")
		}
		return ev
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}
	return Event{}
}

func TestSubscribePublish(t *testing.T) {
	h := New()
	ch, cancel := h.Subscribe("alice")
	defer cancel()
	h.Publish("alice", Event{Type: EventMessage, Summary: message.Summary{ID: "1", Mailbox: "alice"}})
	ev := recv(t, ch)
	if ev.Type != EventMessage || ev.Summary.ID != "1" {
		t.Fatalf("event = %+v", ev)
	}
}

func TestOnlyMatchingMailboxReceives(t *testing.T) {
	h := New()
	a, cancelA := h.Subscribe("alice")
	b, cancelB := h.Subscribe("bob")
	defer cancelA()
	defer cancelB()
	h.Publish("alice", Event{Type: EventMessage})
	recv(t, a)
	select {
	case ev := <-b:
		t.Fatalf("bob received %+v", ev)
	case <-time.After(20 * time.Millisecond):
	}
}

func TestMultipleSubscribersEachReceive(t *testing.T) {
	h := New()
	var chans []<-chan Event
	for i := 0; i < 5; i++ {
		ch, cancel := h.Subscribe("alice")
		defer cancel()
		chans = append(chans, ch)
	}
	h.Publish("alice", Event{Type: EventDeleted, ID: "x"})
	for _, ch := range chans {
		if ev := recv(t, ch); ev.ID != "x" {
			t.Fatalf("event = %+v", ev)
		}
	}
}

func TestSlowSubscriberNeverBlocksPublisher(t *testing.T) {
	h := New()
	slow, cancelSlow := h.Subscribe("alice") // never read
	defer cancelSlow()
	fast, cancelFast := h.Subscribe("alice")
	defer cancelFast()

	done := make(chan struct{})
	go func() {
		for i := 0; i < 1000; i++ {
			h.Publish("alice", Event{Type: EventMessage})
			select { // drain the fast one so it keeps up
			case <-fast:
			default:
			}
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("publisher blocked by slow subscriber")
	}
	if len(slow) == 0 {
		t.Fatal("slow subscriber should hold buffered events")
	}
}

func TestUnsubscribeFreesResources(t *testing.T) {
	h := New()
	_, cancel := h.Subscribe("alice")
	if h.Subscribers("alice") != 1 {
		t.Fatal("expected 1 subscriber")
	}
	cancel()
	cancel() // idempotent
	if h.Subscribers("alice") != 0 {
		t.Fatal("subscriber not removed")
	}
	h.Publish("alice", Event{}) // must not panic with no subscribers
}

func TestChannelClosedOnCancel(t *testing.T) {
	h := New()
	ch, cancel := h.Subscribe("alice")
	cancel()
	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("expected closed channel")
		}
	case <-time.After(time.Second):
		t.Fatal("channel not closed")
	}
}

func TestNoGoroutineLeaks(t *testing.T) {
	before := runtime.NumGoroutine()
	h := New()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, cancel := h.Subscribe("alice")
			h.Publish("alice", Event{})
			cancel()
		}()
	}
	wg.Wait()
	time.Sleep(20 * time.Millisecond)
	if after := runtime.NumGoroutine(); after > before+2 {
		t.Fatalf("goroutines before=%d after=%d", before, after)
	}
}
