package fs_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"phantom-mail/internal/clock"
	"phantom-mail/internal/store"
	"phantom-mail/internal/store/fs"
	"phantom-mail/internal/store/storetest"
)

var epoch = time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)

func TestFSStoreContract(t *testing.T) {
	storetest.Run(t, func(t *testing.T, opts store.Options) store.Store {
		s, err := fs.New(t.TempDir(), opts)
		if err != nil {
			t.Fatal(err)
		}
		return s
	})
}

func newFS(t *testing.T, dir string, opts store.Options) (store.Store, *clock.Fake) {
	t.Helper()
	clk := clock.NewFake(epoch)
	opts.Clock = clk
	s, err := fs.New(dir, opts)
	if err != nil {
		t.Fatal(err)
	}
	return s, clk
}

func TestRestartRebuildsIndex(t *testing.T) {
	dir := t.TempDir()
	s, clk := newFS(t, dir, store.Options{})
	var ids []string
	for i := 0; i < 3; i++ {
		clk.Advance(time.Second)
		r, err := s.Put("alice", storetest.Msg("subject "+string(rune('A'+i)), "body"))
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, r.Summary.ID)
	}
	if _, err := s.Put("bob", []byte(storetest.Msg("bob's", "x"))); err != nil {
		t.Fatal(err)
	}

	s2, _ := newFS(t, dir, store.Options{})
	list, err := s2.List("alice", "", 100)
	if err != nil || len(list) != 3 {
		t.Fatalf("list after restart = %d, %v", len(list), err)
	}
	for i, m := range list {
		if m.ID != ids[2-i] || m.Subject != "subject "+string(rune('A'+2-i)) || m.From == "" {
			t.Fatalf("restored[%d] = %+v", i, m)
		}
		if !m.ReceivedAt.Equal(epoch.Add(time.Duration(3-i) * time.Second)) {
			t.Fatalf("received time lost: %v", m.ReceivedAt)
		}
	}
	if st := s2.Stats(); st.Messages != 4 || st.Mailboxes != 2 {
		t.Fatalf("stats = %+v", st)
	}
	_, raw, err := s2.Get("alice", ids[0])
	if err != nil || !strings.Contains(string(raw), "subject A") {
		t.Fatalf("Get after restart = %q, %v", raw, err)
	}
}

func TestRestartRestoresAttachmentCountLazily(t *testing.T) {
	dir := t.TempDir()
	s, _ := newFS(t, dir, store.Options{})
	raw := "From: s@example.com\nSubject: att\nMIME-Version: 1.0\nContent-Type: multipart/mixed; boundary=\"b\"\n\n--b\nContent-Type: text/plain\n\nhi\n--b\nContent-Type: application/octet-stream\nContent-Disposition: attachment; filename=\"x\"\n\nDATA\n--b--\n"
	if _, err := s.Put("alice", []byte(raw)); err != nil {
		t.Fatal(err)
	}
	s2, _ := newFS(t, dir, store.Options{})
	list, _ := s2.List("alice", "", 10)
	if len(list) != 1 || list[0].AttachmentCount != 1 {
		t.Fatalf("attachment count after restart = %+v", list)
	}
}

func TestStrayAndCorruptFilesAreIgnored(t *testing.T) {
	dir := t.TempDir()
	s, _ := newFS(t, dir, store.Options{})
	r, err := s.Put("alice", storetest.Msg("real", "x"))
	if err != nil {
		t.Fatal(err)
	}
	box := filepath.Join(dir, "alice")
	junk := map[string]string{
		"notes.txt":                             "not a message",
		"zzzz.eml":                              "bad id in the file name",
		r.Summary.ID + ".eml.tmp-123":           "half written message",
		"00000000000000000000000000000000000.x": "x",
	}
	for name, body := range junk {
		if err := os.WriteFile(filepath.Join(box, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(dir, "Not_A_Valid Mailbox!"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "stray-file-at-root"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	s2, _ := newFS(t, dir, store.Options{})
	list, _ := s2.List("alice", "", 100)
	if len(list) != 1 || list[0].ID != r.Summary.ID {
		t.Fatalf("list = %+v", list)
	}
	if st := s2.Stats(); st.Messages != 1 || st.Mailboxes != 1 {
		t.Fatalf("stats = %+v", st)
	}
}

func TestWritesAreAtomicNoPartialFilesLeftBehind(t *testing.T) {
	dir := t.TempDir()
	s, _ := newFS(t, dir, store.Options{})
	for i := 0; i < 20; i++ {
		if _, err := s.Put("alice", storetest.Msg("x", "y")); err != nil {
			t.Fatal(err)
		}
	}
	entries, _ := os.ReadDir(filepath.Join(dir, "alice"))
	if len(entries) != 20 {
		t.Fatalf("files = %d", len(entries))
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".eml") {
			t.Fatalf("temporary file left behind: %s", e.Name())
		}
	}
}

func TestEmptyMailboxDirectoryRemoved(t *testing.T) {
	dir := t.TempDir()
	exists := func(box string) bool {
		_, err := os.Stat(filepath.Join(dir, box))
		return err == nil
	}

	t.Run("delete", func(t *testing.T) {
		s, _ := newFS(t, dir, store.Options{})
		r, _ := s.Put("del", storetest.Msg("x", "y"))
		if !exists("del") {
			t.Fatal("mailbox dir not created")
		}
		if err := s.Delete("del", r.Summary.ID); err != nil {
			t.Fatal(err)
		}
		if exists("del") {
			t.Fatal("empty mailbox dir not removed after delete")
		}
	})
	t.Run("delete mailbox", func(t *testing.T) {
		s, _ := newFS(t, dir, store.Options{})
		_, _ = s.Put("clear", storetest.Msg("x", "y"))
		_, _ = s.DeleteMailbox("clear")
		if exists("clear") {
			t.Fatal("empty mailbox dir not removed after DeleteMailbox")
		}
	})
	t.Run("expiry", func(t *testing.T) {
		s, clk := newFS(t, dir, store.Options{Retention: time.Hour})
		_, _ = s.Put("exp", storetest.Msg("x", "y"))
		clk.Advance(2 * time.Hour)
		s.Sweep()
		if exists("exp") {
			t.Fatal("empty mailbox dir not removed after expiry")
		}
	})
	t.Run("eviction", func(t *testing.T) {
		raw := storetest.Msg("x", strings.Repeat("y", 200))
		s, clk := newFS(t, dir, store.Options{MaxTotalBytes: int64(len(raw)) + 10})
		_, _ = s.Put("old", raw)
		clk.Advance(time.Second)
		_, _ = s.Put("new", raw)
		if exists("old") {
			t.Fatal("empty mailbox dir not removed after eviction")
		}
		if !exists("new") {
			t.Fatal("new mailbox dir missing")
		}
	})
}

func TestMailboxLimitDoesNotLeaveDirectoryBehind(t *testing.T) {
	dir := t.TempDir()
	s, _ := newFS(t, dir, store.Options{MaxMailboxes: 1})
	_, _ = s.Put("a", storetest.Msg("x", "y"))
	if _, err := s.Put("b", storetest.Msg("x", "y")); err == nil {
		t.Fatal("expected ErrMailboxLimit")
	}
	if _, err := os.Stat(filepath.Join(dir, "b")); err == nil {
		t.Fatal("directory created for refused mailbox")
	}
}

func TestHostileIDsAndNamesNeverReachTheFilesystem(t *testing.T) {
	dir := t.TempDir()
	secret := filepath.Join(dir, "outside.eml")
	if err := os.WriteFile(secret, []byte("Subject: secret\n\nx"), 0o600); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(dir, "data")
	s, _ := newFS(t, root, store.Options{})
	if _, err := s.Put("alice", storetest.Msg("ok", "x")); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"../outside", "../../outside", "..%2foutside", "/etc/passwd", "alice/../../outside", "", "\x00"} {
		if _, _, err := s.Get("alice", id); err == nil {
			t.Errorf("Get(%q) succeeded", id)
		}
		if err := s.Delete("alice", id); err == nil {
			t.Errorf("Delete(%q) succeeded", id)
		}
	}
	for _, box := range []string{"../data", "..", "alice/../..", "a/b", "A"} {
		if _, err := s.Put(box, storetest.Msg("x", "y")); err == nil {
			t.Errorf("Put(%q) succeeded", box)
		}
		if _, err := s.DeleteMailbox(box); err == nil {
			t.Errorf("DeleteMailbox(%q) succeeded", box)
		}
	}
	if _, err := os.Stat(secret); err != nil {
		t.Fatalf("a file outside the data directory was affected: %v", err)
	}
}
