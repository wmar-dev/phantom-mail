package retention

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"phantom-mail/internal/clock"
	"phantom-mail/internal/hub"
	"phantom-mail/internal/inbox"
	"phantom-mail/internal/store"
	"phantom-mail/internal/store/fs"
)

var epoch = time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)

func discard() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func msg(subject string, pad int) []byte {
	b := make([]byte, pad)
	for i := range b {
		b[i] = 'x'
	}
	return []byte("From: s@example.com\nSubject: " + subject + "\n\n" + string(b) + "\n")
}

func setup(t *testing.T, opts store.Options) (*inbox.Service, *clock.Fake, string) {
	t.Helper()
	dir := t.TempDir()
	clk := clock.NewFake(epoch)
	opts.Clock = clk
	st, err := fs.New(dir, opts)
	if err != nil {
		t.Fatal(err)
	}
	return inbox.New(st, hub.New()), clk, dir
}

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func files(dir string) int {
	n := 0
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			n++
		}
		return nil
	})
	return n
}

func TestJanitorDeletesExpiredMessagesAndFiles(t *testing.T) {
	svc, clk, dir := setup(t, store.Options{Retention: time.Hour})
	for i := 0; i < 3; i++ {
		if err := svc.Deliver("alice", msg("m", 10)); err != nil {
			t.Fatal(err)
		}
	}
	if err := svc.Deliver("bob", msg("b", 10)); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go Run(ctx, svc, 10*time.Millisecond, discard())

	time.Sleep(50 * time.Millisecond)
	if files(dir) != 4 {
		t.Fatal("nothing should be deleted before the retention period")
	}
	clk.Advance(2 * time.Hour)
	eventually(t, "expired files to be removed", func() bool { return files(dir) == 0 })
	if st := svc.Stats(); st.Messages != 0 || st.Mailboxes != 0 {
		t.Fatalf("stats = %+v", st)
	}
}

func TestExpiredMessagesAreInvisibleBetweenTicks(t *testing.T) {
	svc, clk, dir := setup(t, store.Options{Retention: time.Hour})
	if err := svc.Deliver("alice", msg("old", 10)); err != nil {
		t.Fatal(err)
	}
	list, _ := svc.List("alice", "", 10)
	id := list[0].ID

	// A janitor that effectively never ticks again after its first sweep.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go Run(ctx, svc, time.Hour, discard())
	time.Sleep(50 * time.Millisecond)

	clk.Advance(61 * time.Minute)
	if got, _ := svc.List("alice", "", 10); len(got) != 0 {
		t.Fatalf("expired message still listed: %+v", got)
	}
	if _, _, err := svc.Get("alice", id); err == nil {
		t.Fatal("expired message still readable")
	}
	if files(dir) != 1 {
		t.Fatal("the file is expected to still be on disk until the next sweep")
	}
}

func TestJanitorAnnouncesDeletions(t *testing.T) {
	svc, clk, _ := setup(t, store.Options{Retention: time.Hour})
	if err := svc.Deliver("alice", msg("m", 10)); err != nil {
		t.Fatal(err)
	}
	ch, cancelSub := svc.Subscribe("alice")
	defer cancelSub()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go Run(ctx, svc, 10*time.Millisecond, discard())
	clk.Advance(2 * time.Hour)
	select {
	case ev := <-ch:
		if ev.Type != hub.EventDeleted || ev.ID == "" {
			t.Fatalf("event = %+v", ev)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no deleted event for an expired message")
	}
}

func TestPerMailboxCapEvictsOldest(t *testing.T) {
	svc, clk, dir := setup(t, store.Options{MaxPerMailbox: 3})
	var ids []string
	for i := 0; i < 5; i++ {
		clk.Advance(time.Second)
		if err := svc.Deliver("alice", msg("m", 10)); err != nil {
			t.Fatal(err)
		}
		l, _ := svc.List("alice", "", 1)
		ids = append(ids, l[0].ID)
	}
	list, _ := svc.List("alice", "", 100)
	if len(list) != 3 || list[0].ID != ids[4] || list[2].ID != ids[2] {
		t.Fatalf("list = %+v", list)
	}
	if files(dir) != 3 {
		t.Fatalf("files on disk = %d, want 3", files(dir))
	}
}

func TestTotalBytesCapEvictsOldestAcrossMailboxes(t *testing.T) {
	one := len(msg("m", 200))
	svc, clk, dir := setup(t, store.Options{MaxTotalBytes: int64(one*2 + 10)})
	for i, box := range []string{"a", "b", "c", "a"} {
		clk.Advance(time.Second)
		if err := svc.Deliver(box, msg("m", 200)); err != nil {
			t.Fatalf("deliver %d: %v", i, err)
		}
	}
	if st := svc.Stats(); st.Messages != 2 || st.Bytes > int64(one*2+10) {
		t.Fatalf("stats = %+v", st)
	}
	if files(dir) != 2 {
		t.Fatalf("files on disk = %d, want 2", files(dir))
	}
	if l, _ := svc.List("a", "", 10); len(l) != 1 {
		t.Fatalf("the newest messages should survive: %+v", l)
	}
}

func TestSweepsOnceImmediatelyAtStart(t *testing.T) {
	svc, clk, dir := setup(t, store.Options{Retention: time.Hour})
	_ = svc.Deliver("alice", msg("m", 10))
	clk.Advance(3 * time.Hour) // expired while the service was "down"
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go Run(ctx, svc, time.Hour, discard())
	eventually(t, "the startup sweep", func() bool { return files(dir) == 0 })
}

type countingSweeper struct{ n atomic.Int64 }

func (c *countingSweeper) Sweep() int { c.n.Add(1); return 0 }

func TestRunStopsWhenContextIsCancelled(t *testing.T) {
	before := runtime.NumGoroutine()
	s := &countingSweeper{}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { Run(ctx, s, 5*time.Millisecond, discard()); close(done) }()
	eventually(t, "a few sweeps", func() bool { return s.n.Load() >= 3 })
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run did not return after cancellation")
	}
	n := s.n.Load()
	time.Sleep(30 * time.Millisecond)
	if s.n.Load() != n {
		t.Fatal("sweeps continued after cancellation")
	}
	if after := runtime.NumGoroutine(); after > before+2 {
		t.Fatalf("goroutines before=%d after=%d", before, after)
	}
}

func TestRunSurvivesAPanickingSweep(t *testing.T) {
	calls := atomic.Int64{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go Run(ctx, sweeperFunc(func() int {
		if calls.Add(1) == 1 {
			panic("boom")
		}
		return 0
	}), 5*time.Millisecond, discard())
	eventually(t, "sweeps to continue after a panic", func() bool { return calls.Load() >= 3 })
}

type sweeperFunc func() int

func (f sweeperFunc) Sweep() int { return f() }
