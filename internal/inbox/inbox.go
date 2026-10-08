// Package inbox joins the store and the hub: every change that goes through
// the Service is also announced to subscribers (SSE streams, long polls).
package inbox

import (
	"context"
	"time"

	"phantom-mail/internal/hub"
	"phantom-mail/internal/message"
	"phantom-mail/internal/store"
)

// Service is the application-level mailbox API used by SMTP and HTTP.
type Service struct {
	store store.Store
	hub   *hub.Hub
}

// New returns a Service over st publishing to h.
func New(st store.Store, h *hub.Hub) *Service { return &Service{store: st, hub: h} }

// Accepts implements smtpd.Inbox.
func (s *Service) Accepts(mailbox string) error { return s.store.Accepts(mailbox) }

// Deliver stores a message and announces it. Evictions caused by the new
// message are announced as deletions.
func (s *Service) Deliver(mailbox string, raw []byte) error {
	res, err := s.store.Put(mailbox, raw)
	if err != nil {
		return err
	}
	s.announceDeleted(res.Evicted)
	s.hub.Publish(mailbox, hub.Event{Type: hub.EventMessage, Summary: res.Summary})
	return nil
}

func (s *Service) announceDeleted(refs []store.Ref) {
	for _, r := range refs {
		s.hub.Publish(r.Mailbox, hub.Event{Type: hub.EventDeleted, ID: r.ID})
	}
}

// List returns messages newest first.
func (s *Service) List(mailbox, after string, limit int) ([]message.Summary, error) {
	return s.store.List(mailbox, after, limit)
}

// Get returns a summary and the raw message.
func (s *Service) Get(mailbox, id string) (message.Summary, []byte, error) {
	return s.store.Get(mailbox, id)
}

// Delete removes one message and announces it.
func (s *Service) Delete(mailbox, id string) error {
	if err := s.store.Delete(mailbox, id); err != nil {
		return err
	}
	s.announceDeleted([]store.Ref{{Mailbox: mailbox, ID: id}})
	return nil
}

// DeleteMailbox removes every message in a mailbox.
func (s *Service) DeleteMailbox(mailbox string) error {
	refs, err := s.store.DeleteMailbox(mailbox)
	s.announceDeleted(refs)
	return err
}

// Sweep removes expired messages and announces the removals.
func (s *Service) Sweep() int {
	refs := s.store.Sweep()
	s.announceDeleted(refs)
	return len(refs)
}

// Stats reports store totals.
func (s *Service) Stats() store.Stats { return s.store.Stats() }

// Subscribe returns a channel of mailbox events and a cancel function.
func (s *Service) Subscribe(mailbox string) (<-chan hub.Event, func()) {
	return s.hub.Subscribe(mailbox)
}

// Wait returns messages newer than after (all messages when after is empty),
// holding the call open until one exists, the timeout elapses (nil result),
// or ctx ends. It subscribes before checking the store so a message arriving
// in between is never missed.
func (s *Service) Wait(ctx context.Context, mailbox, after string, timeout time.Duration) ([]message.Summary, error) {
	ch, cancel := s.hub.Subscribe(mailbox)
	defer cancel()
	list, err := s.store.List(mailbox, after, 100)
	if err != nil || len(list) > 0 {
		return list, err
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timer.C:
			return nil, nil
		case ev, ok := <-ch:
			if !ok {
				return nil, nil
			}
			if ev.Type != hub.EventMessage {
				continue
			}
			list, err := s.store.List(mailbox, after, 100)
			if err != nil || len(list) > 0 {
				return list, err
			}
		}
	}
}
