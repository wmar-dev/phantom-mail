// Package storetest is a reusable contract test suite for store.Store
// implementations. Each implementation's tests call Run with a factory.
package storetest

import (
	"bytes"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"phantom-mail/internal/clock"
	"phantom-mail/internal/store"
)

// Factory builds a fresh, empty store for one test.
type Factory func(t *testing.T, opts store.Options) store.Store

var epoch = time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)

// Msg builds a small raw message with the given subject and body.
func Msg(subject, body string) []byte {
	return []byte(fmt.Sprintf("From: Sender <sender@example.com>\nTo: rcpt@localhost\nSubject: %s\n\n%s\n", subject, body))
}

const withAttachment = "From: s@example.com\nTo: a@localhost\nSubject: att\nMIME-Version: 1.0\n" +
	"Content-Type: multipart/mixed; boundary=\"b\"\n\n--b\nContent-Type: text/plain\n\nhi\n" +
	"--b\nContent-Type: application/octet-stream\nContent-Disposition: attachment; filename=\"x.bin\"\n" +
	"Content-Transfer-Encoding: base64\n\naGVsbG8=\n--b--\n"

func newStore(t *testing.T, f Factory, opts store.Options) (store.Store, *clock.Fake) {
	t.Helper()
	clk := clock.NewFake(epoch)
	opts.Clock = clk
	return f(t, opts), clk
}

func mustPut(t *testing.T, s store.Store, mailbox string, raw []byte) store.PutResult {
	t.Helper()
	r, err := s.Put(mailbox, raw)
	if err != nil {
		t.Fatalf("Put(%q): %v", mailbox, err)
	}
	return r
}

// Run executes the contract suite against the factory.
func Run(t *testing.T, f Factory) {
	t.Run("PutGetRoundTrip", func(t *testing.T) {
		s, _ := newStore(t, f, store.Options{})
		raw := Msg("Your code", "482913")
		r := mustPut(t, s, "alice", raw)
		if r.Summary.Mailbox != "alice" || r.Summary.Subject != "Your code" || r.Summary.ID == "" {
			t.Fatalf("summary = %+v", r.Summary)
		}
		if r.Summary.Size != len(raw) || !r.Summary.ReceivedAt.Equal(epoch) {
			t.Fatalf("size/time = %d %v", r.Summary.Size, r.Summary.ReceivedAt)
		}
		sum, got, err := s.Get("alice", r.Summary.ID)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, raw) || sum.ID != r.Summary.ID {
			t.Fatalf("Get mismatch: %q", got)
		}
	})

	t.Run("GetMissing", func(t *testing.T) {
		s, _ := newStore(t, f, store.Options{})
		mustPut(t, s, "alice", Msg("a", "b"))
		if _, _, err := s.Get("alice", "00000000000000000000000000000000dead"); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("err = %v, want ErrNotFound", err)
		}
		if _, _, err := s.Get("nobody", "x"); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("err = %v, want ErrNotFound", err)
		}
	})

	t.Run("ListNewestFirstLimitAfter", func(t *testing.T) {
		s, clk := newStore(t, f, store.Options{})
		var ids []string
		for i := 0; i < 5; i++ {
			clk.Advance(time.Second)
			ids = append(ids, mustPut(t, s, "alice", Msg(fmt.Sprintf("m%d", i), "x")).Summary.ID)
		}
		list, err := s.List("alice", "", 100)
		if err != nil || len(list) != 5 {
			t.Fatalf("list = %d, %v", len(list), err)
		}
		for i, m := range list {
			if want := ids[len(ids)-1-i]; m.ID != want {
				t.Fatalf("list[%d] = %s, want %s", i, m.ID, want)
			}
		}
		list, _ = s.List("alice", "", 2)
		if len(list) != 2 || list[0].ID != ids[4] || list[1].ID != ids[3] {
			t.Fatalf("limit list = %+v", list)
		}
		list, _ = s.List("alice", ids[2], 100)
		if len(list) != 2 || list[0].ID != ids[4] || list[1].ID != ids[3] {
			t.Fatalf("after list = %+v", list)
		}
		list, err = s.List("unused", "", 100)
		if err != nil || len(list) != 0 {
			t.Fatalf("unused mailbox = %v, %v", list, err)
		}
	})

	t.Run("IDsStrictlyIncreaseWithinOneInstant", func(t *testing.T) {
		s, _ := newStore(t, f, store.Options{})
		prev := ""
		for i := 0; i < 50; i++ {
			id := mustPut(t, s, "alice", Msg("x", "y")).Summary.ID
			if id <= prev {
				t.Fatalf("id %s <= %s", id, prev)
			}
			prev = id
		}
		list, _ := s.List("alice", "", 100)
		if list[0].ID != prev {
			t.Fatal("newest message is not first")
		}
	})

	t.Run("DeleteAndDeleteMailbox", func(t *testing.T) {
		s, _ := newStore(t, f, store.Options{})
		a := mustPut(t, s, "alice", Msg("a", "1")).Summary.ID
		b := mustPut(t, s, "alice", Msg("b", "2")).Summary.ID
		if err := s.Delete("alice", a); err != nil {
			t.Fatal(err)
		}
		if err := s.Delete("alice", a); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("second delete err = %v", err)
		}
		refs, err := s.DeleteMailbox("alice")
		if err != nil || len(refs) != 1 || refs[0].ID != b || refs[0].Mailbox != "alice" {
			t.Fatalf("DeleteMailbox = %v, %v", refs, err)
		}
		refs, err = s.DeleteMailbox("alice")
		if err != nil || len(refs) != 0 {
			t.Fatalf("idempotent DeleteMailbox = %v, %v", refs, err)
		}
		if st := s.Stats(); st.Messages != 0 || st.Bytes != 0 || st.Mailboxes != 0 {
			t.Fatalf("stats = %+v", st)
		}
	})

	t.Run("PerMailboxCapEvictsOldest", func(t *testing.T) {
		s, clk := newStore(t, f, store.Options{MaxPerMailbox: 3})
		var ids []string
		var evicted []store.Ref
		for i := 0; i < 5; i++ {
			clk.Advance(time.Second)
			r := mustPut(t, s, "alice", Msg(fmt.Sprintf("m%d", i), "x"))
			ids = append(ids, r.Summary.ID)
			evicted = append(evicted, r.Evicted...)
		}
		list, _ := s.List("alice", "", 100)
		if len(list) != 3 || list[0].ID != ids[4] || list[2].ID != ids[2] {
			t.Fatalf("list = %+v", list)
		}
		if len(evicted) != 2 || evicted[0].ID != ids[0] || evicted[1].ID != ids[1] {
			t.Fatalf("evicted = %+v", evicted)
		}
		if _, _, err := s.Get("alice", ids[0]); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("evicted message still readable: %v", err)
		}
	})

	t.Run("TotalBytesCapEvictsOldestAcrossMailboxes", func(t *testing.T) {
		raw := Msg("x", string(bytes.Repeat([]byte("y"), 100)))
		limit := int64(len(raw)*3 + 10)
		s, clk := newStore(t, f, store.Options{MaxTotalBytes: limit})
		var first string
		for i := 0; i < 6; i++ {
			clk.Advance(time.Second)
			r := mustPut(t, s, fmt.Sprintf("box%d", i%2), raw)
			if i == 0 {
				first = r.Summary.ID
			}
			if st := s.Stats(); st.Bytes > limit {
				t.Fatalf("after put %d bytes = %d > %d", i, st.Bytes, limit)
			}
		}
		if st := s.Stats(); st.Messages != 3 {
			t.Fatalf("messages = %d, want 3", st.Messages)
		}
		if _, _, err := s.Get("box0", first); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("oldest message survived: %v", err)
		}
	})

	t.Run("MailboxCountCap", func(t *testing.T) {
		s, _ := newStore(t, f, store.Options{MaxMailboxes: 2})
		mustPut(t, s, "a", Msg("1", "x"))
		mustPut(t, s, "b", Msg("2", "x"))
		if err := s.Accepts("c"); !errors.Is(err, store.ErrMailboxLimit) {
			t.Fatalf("Accepts(c) = %v, want ErrMailboxLimit", err)
		}
		if _, err := s.Put("c", Msg("3", "x")); !errors.Is(err, store.ErrMailboxLimit) {
			t.Fatalf("Put(c) = %v, want ErrMailboxLimit", err)
		}
		if err := s.Accepts("a"); err != nil {
			t.Fatalf("existing mailbox refused: %v", err)
		}
		mustPut(t, s, "a", Msg("4", "x"))
		if _, err := s.DeleteMailbox("a"); err != nil {
			t.Fatal(err)
		}
		if err := s.Accepts("c"); err != nil {
			t.Fatalf("slot not freed: %v", err)
		}
		mustPut(t, s, "c", Msg("5", "x"))
	})

	t.Run("RejectsInvalidMailboxNames", func(t *testing.T) {
		s, _ := newStore(t, f, store.Options{})
		for _, bad := range []string{"", "../x", "A", "a/b", "a+b"} {
			if _, err := s.Put(bad, Msg("x", "y")); err == nil {
				t.Errorf("Put(%q) succeeded", bad)
			}
			if _, err := s.List(bad, "", 10); err == nil {
				t.Errorf("List(%q) succeeded", bad)
			}
		}
	})

	t.Run("ExpiredMessagesAreInvisibleBeforeSweep", func(t *testing.T) {
		s, clk := newStore(t, f, store.Options{Retention: time.Hour})
		id := mustPut(t, s, "alice", Msg("old", "x")).Summary.ID
		clk.Advance(30 * time.Minute)
		newer := mustPut(t, s, "alice", Msg("new", "x")).Summary.ID
		clk.Advance(45 * time.Minute) // first is now 75m old, second 45m
		list, _ := s.List("alice", "", 100)
		if len(list) != 1 || list[0].ID != newer {
			t.Fatalf("list = %+v", list)
		}
		if _, _, err := s.Get("alice", id); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("expired Get err = %v", err)
		}
	})

	t.Run("SweepRemovesExpired", func(t *testing.T) {
		s, clk := newStore(t, f, store.Options{Retention: time.Hour})
		a := mustPut(t, s, "alice", Msg("a", "x")).Summary.ID
		b := mustPut(t, s, "bob", Msg("b", "x")).Summary.ID
		clk.Advance(2 * time.Hour)
		removed := s.Sweep()
		if len(removed) != 2 {
			t.Fatalf("removed = %+v", removed)
		}
		got := map[string]bool{removed[0].ID: true, removed[1].ID: true}
		if !got[a] || !got[b] {
			t.Fatalf("removed wrong messages: %+v", removed)
		}
		if st := s.Stats(); st.Messages != 0 || st.Bytes != 0 || st.Mailboxes != 0 {
			t.Fatalf("stats after sweep = %+v", st)
		}
		if again := s.Sweep(); len(again) != 0 {
			t.Fatalf("second sweep removed %v", again)
		}
	})

	t.Run("AttachmentCountInList", func(t *testing.T) {
		s, _ := newStore(t, f, store.Options{})
		mustPut(t, s, "alice", []byte(withAttachment))
		mustPut(t, s, "alice", Msg("plain", "x"))
		list, _ := s.List("alice", "", 100)
		if len(list) != 2 || list[0].AttachmentCount != 0 || list[1].AttachmentCount != 1 {
			t.Fatalf("attachment counts = %+v", list)
		}
	})

	t.Run("StatsTrackBytesAndCounts", func(t *testing.T) {
		s, _ := newStore(t, f, store.Options{})
		r1 := mustPut(t, s, "a", Msg("1", "x"))
		r2 := mustPut(t, s, "b", Msg("2", "yy"))
		st := s.Stats()
		if st.Messages != 2 || st.Mailboxes != 2 || st.Bytes != int64(r1.Summary.Size+r2.Summary.Size) {
			t.Fatalf("stats = %+v", st)
		}
	})

	t.Run("ConcurrentPuts", func(t *testing.T) {
		s, _ := newStore(t, f, store.Options{MaxPerMailbox: 1000})
		var wg sync.WaitGroup
		for w := 0; w < 8; w++ {
			wg.Add(1)
			go func(w int) {
				defer wg.Done()
				for i := 0; i < 25; i++ {
					if _, err := s.Put(fmt.Sprintf("box%d", w%3), Msg("c", "x")); err != nil {
						t.Error(err)
						return
					}
					_, _ = s.List(fmt.Sprintf("box%d", w%3), "", 10)
				}
			}(w)
		}
		wg.Wait()
		if st := s.Stats(); st.Messages != 200 {
			t.Fatalf("messages = %d, want 200", st.Messages)
		}
		seen := map[string]bool{}
		for b := 0; b < 3; b++ {
			list, _ := s.List(fmt.Sprintf("box%d", b), "", 1000)
			for _, m := range list {
				if seen[m.ID] {
					t.Fatalf("duplicate id %s", m.ID)
				}
				seen[m.ID] = true
			}
		}
	})
}
