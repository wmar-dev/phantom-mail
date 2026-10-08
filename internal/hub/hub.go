// Package hub is a small in-process publish/subscribe fan-out keyed by
// mailbox. It powers the long-poll wait endpoint and Server-Sent Events.
package hub

import (
	"sync"

	"phantom-mail/internal/message"
)

// Event types.
const (
	EventMessage = "message" // a new message arrived (Summary is set)
	EventDeleted = "deleted" // a message was removed (ID is set)
)

// Event describes a change in a mailbox.
type Event struct {
	Type    string
	Summary message.Summary
	ID      string
}

const bufferSize = 32

// Hub fans events out to subscribers. Publishing never blocks: if a
// subscriber's buffer is full the event is dropped for that subscriber, who
// is expected to re-read the store (the wait handler does).
type Hub struct {
	mu   sync.Mutex
	subs map[string]map[*sub]struct{}
}

type sub struct{ ch chan Event }

// New returns an empty hub.
func New() *Hub { return &Hub{subs: map[string]map[*sub]struct{}{}} }

// Subscribe registers for events in a mailbox. The returned cancel function
// unsubscribes and closes the channel; it is safe to call more than once.
func (h *Hub) Subscribe(mailbox string) (<-chan Event, func()) {
	s := &sub{ch: make(chan Event, bufferSize)}
	h.mu.Lock()
	if h.subs[mailbox] == nil {
		h.subs[mailbox] = map[*sub]struct{}{}
	}
	h.subs[mailbox][s] = struct{}{}
	h.mu.Unlock()

	var once sync.Once
	cancel := func() {
		once.Do(func() {
			h.mu.Lock()
			delete(h.subs[mailbox], s)
			if len(h.subs[mailbox]) == 0 {
				delete(h.subs, mailbox)
			}
			close(s.ch) // under the lock so Publish never sends on a closed channel
			h.mu.Unlock()
		})
	}
	return s.ch, cancel
}

// Publish delivers ev to every subscriber of the mailbox without blocking.
func (h *Hub) Publish(mailbox string, ev Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for s := range h.subs[mailbox] {
		select {
		case s.ch <- ev:
		default:
		}
	}
}

// Subscribers reports how many subscribers a mailbox has.
func (h *Hub) Subscribers(mailbox string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.subs[mailbox])
}
