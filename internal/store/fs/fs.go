// Package fs is the volume-backed store: one file per message at
// <dir>/<mailbox>/<id>.eml, with an in-memory index rebuilt at startup.
package fs

import (
	"errors"
	"fmt"
	"io"
	iofs "io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"

	"phantom-mail/internal/mailbox"
	"phantom-mail/internal/message"
	"phantom-mail/internal/store"
)

const (
	ext        = ".eml"
	tmpMarker  = ".eml.tmp-"
	headerRead = 64 << 10 // bytes read per file when rebuilding the index
)

type blobs struct {
	root string
	seq  atomic.Uint64
}

func (b *blobs) dir(mailbox string) string { return filepath.Join(b.root, mailbox) }

func (b *blobs) path(mailbox, id string) string {
	return filepath.Join(b.dir(mailbox), id+ext)
}

// Write stores raw atomically: write a temporary file, fsync it, rename it
// into place, then fsync the directory. A crash never leaves a partial
// message that would be served.
func (b *blobs) Write(mailbox, id string, raw []byte) error {
	dir := b.dir(mailbox)
	final := b.path(mailbox, id)
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		if err = os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
		tmp := filepath.Join(dir, fmt.Sprintf("%s%s%d", id, tmpMarker, b.seq.Add(1)))
		err = writeFile(tmp, raw)
		if err == nil {
			err = os.Rename(tmp, final)
			if err != nil {
				_ = os.Remove(tmp)
			}
		}
		if err == nil {
			syncDir(dir)
			return nil
		}
		if !errors.Is(err, iofs.ErrNotExist) {
			return err
		}
		// The mailbox directory was removed between MkdirAll and the write
		// (its last message was just deleted): try again.
	}
	return err
}

func writeFile(path string, raw []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err = f.Write(raw); err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(path)
	}
	return err
}

func syncDir(dir string) {
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}
}

func (b *blobs) Read(mailbox, id string) ([]byte, error) {
	raw, err := os.ReadFile(b.path(mailbox, id))
	if errors.Is(err, iofs.ErrNotExist) {
		return nil, store.ErrNotFound
	}
	return raw, err
}

func (b *blobs) Remove(mailbox, id string) error {
	err := os.Remove(b.path(mailbox, id))
	if errors.Is(err, iofs.ErrNotExist) {
		return nil
	}
	return err
}

// Cleanup removes the mailbox directory if it is empty. os.Remove refuses to
// delete a non-empty directory, so a racing delivery is never harmed.
func (b *blobs) Cleanup(mailbox string) { _ = os.Remove(b.dir(mailbox)) }

// New opens (creating if needed) a store rooted at dir and rebuilds its index
// from the files found there. Only file names, sizes, and each file's header
// block are read.
func New(dir string, opts store.Options) (store.Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("data directory: %w", err)
	}
	b := &blobs{root: dir}
	restored, err := b.scan()
	if err != nil {
		return nil, err
	}
	return store.NewCore(b, opts, restored), nil
}

func (b *blobs) scan() ([]message.Summary, error) {
	boxes, err := os.ReadDir(b.root)
	if err != nil {
		return nil, err
	}
	var out []message.Summary
	for _, d := range boxes {
		if !d.IsDir() || !mailbox.Valid(d.Name()) {
			continue
		}
		name := d.Name()
		files, err := os.ReadDir(b.dir(name))
		if err != nil {
			continue
		}
		kept := 0
		for _, f := range files {
			fn := f.Name()
			if strings.Contains(fn, tmpMarker) {
				_ = os.Remove(filepath.Join(b.dir(name), fn)) // leftover of an interrupted write
				continue
			}
			if f.IsDir() || !strings.HasSuffix(fn, ext) {
				kept++ // not ours; leave it alone
				continue
			}
			id := strings.TrimSuffix(fn, ext)
			if !message.ValidID(id) {
				kept++
				continue
			}
			sum, ok := b.summarize(name, id)
			if !ok {
				kept++
				continue
			}
			kept++
			out = append(out, sum)
		}
		if kept == 0 {
			b.Cleanup(name)
		}
	}
	return out, nil
}

func (b *blobs) summarize(name, id string) (message.Summary, bool) {
	f, err := os.Open(b.path(name, id))
	if err != nil {
		return message.Summary{}, false
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return message.Summary{}, false
	}
	head, err := io.ReadAll(io.LimitReader(f, headerRead))
	if err != nil {
		return message.Summary{}, false
	}
	s := message.SummaryFromRaw(name, id, head, false)
	s.Size = int(info.Size())
	return s, true
}
