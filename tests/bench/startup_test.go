package bench

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"phantom-mail/internal/clock"
	"phantom-mail/internal/message"
	"phantom-mail/internal/store"
	"phantom-mail/internal/store/fs"
)

// Plan goal: with 10,000 stored messages the service rebuilds its index and
// is ready in under one second.
func TestStartupWith10000StoredMessages(t *testing.T) {
	if testing.Short() {
		t.Skip("short mode")
	}
	dir := t.TempDir()
	clk := clock.NewFake(time.Now())
	ids := message.NewIDGen(clk)
	const boxes, perBox = 100, 100
	for b := 0; b < boxes; b++ {
		box := filepath.Join(dir, fmt.Sprintf("box%03d", b))
		if err := os.MkdirAll(box, 0o700); err != nil {
			t.Fatal(err)
		}
		for m := 0; m < perBox; m++ {
			clk.Advance(time.Millisecond)
			raw := fmt.Sprintf("Return-Path: <s@example.com>\r\nX-Original-To: box%03d@localhost\r\nFrom: Sender <s@example.com>\r\nSubject: message %d\r\n\r\nYour code is %06d\r\n", b, m, m)
			if err := os.WriteFile(filepath.Join(box, ids.New()+".eml"), []byte(raw), 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}

	limit := time.Duration(scale(1)) * time.Second
	if raceEnabled {
		limit = 5 * time.Second
	}
	start := time.Now()
	s, err := fs.New(dir, store.Options{Clock: clk, MaxPerMailbox: 1000, MaxMailboxes: 100000})
	took := time.Since(start)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("index of 10,000 messages rebuilt in %v (gate < %v)", took, limit)
	if took > limit {
		t.Errorf("startup took %v", took)
	}
	if st := s.Stats(); st.Messages != boxes*perBox || st.Mailboxes != boxes {
		t.Fatalf("stats = %+v", st)
	}
	list, _ := s.List("box042", "", 5)
	if len(list) != 5 || list[0].Subject != "message 99" || list[0].From == "" {
		t.Fatalf("restored summaries are wrong: %+v", list)
	}
}
