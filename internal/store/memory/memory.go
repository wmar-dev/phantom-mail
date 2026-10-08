// Package memory is an in-memory store for tests and ephemeral runs.
package memory

import (
	"sync"

	"phantom-mail/internal/store"
)

type blobs struct {
	mu sync.RWMutex
	m  map[string][]byte
}

func key(mailbox, id string) string { return mailbox + "/" + id }

func (b *blobs) Write(mailbox, id string, raw []byte) error {
	cp := append([]byte(nil), raw...)
	b.mu.Lock()
	b.m[key(mailbox, id)] = cp
	b.mu.Unlock()
	return nil
}

func (b *blobs) Read(mailbox, id string) ([]byte, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	raw, ok := b.m[key(mailbox, id)]
	if !ok {
		return nil, store.ErrNotFound
	}
	return append([]byte(nil), raw...), nil
}

func (b *blobs) Remove(mailbox, id string) error {
	b.mu.Lock()
	delete(b.m, key(mailbox, id))
	b.mu.Unlock()
	return nil
}

func (b *blobs) Cleanup(string) {}

// New returns an empty in-memory store.
func New(opts store.Options) store.Store {
	return store.NewCore(&blobs{m: map[string][]byte{}}, opts, nil)
}
