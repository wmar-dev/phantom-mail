// Package store keeps received messages. Core implements the index, limits,
// retention and ordering rules once; the memory and fs packages only supply
// the Blobs backend that holds the raw message bytes.
package store

import (
	"errors"
	"sort"
	"sync"
	"time"

	"phantom-mail/internal/clock"
	"phantom-mail/internal/mailbox"
	"phantom-mail/internal/message"
)

// Errors returned by stores.
var (
	ErrNotFound       = errors.New("message not found")
	ErrMailboxLimit   = errors.New("mailbox limit reached")
	ErrInvalidMailbox = errors.New("invalid mailbox name")
)

// Options configure limits and retention. Zero values use the defaults.
type Options struct {
	Retention     time.Duration // default 24h
	MaxPerMailbox int           // default 100
	MaxMailboxes  int           // default 10000
	MaxTotalBytes int64         // default 1 GiB
	Clock         clock.Clock   // default wall clock
}

func (o *Options) applyDefaults() {
	if o.Retention <= 0 {
		o.Retention = 24 * time.Hour
	}
	if o.MaxPerMailbox <= 0 {
		o.MaxPerMailbox = 100
	}
	if o.MaxMailboxes <= 0 {
		o.MaxMailboxes = 10000
	}
	if o.MaxTotalBytes <= 0 {
		o.MaxTotalBytes = 1 << 30
	}
	if o.Clock == nil {
		o.Clock = clock.Real{}
	}
}

// Ref identifies one message.
type Ref struct {
	Mailbox string
	ID      string
}

// PutResult reports a stored message and anything evicted to make room.
type PutResult struct {
	Summary message.Summary
	Evicted []Ref
}

// Stats summarizes the store.
type Stats struct {
	Messages  int
	Mailboxes int
	Bytes     int64
}

// Store is the storage interface used by the rest of the service.
type Store interface {
	// Put stores a raw message. It returns ErrMailboxLimit when a new mailbox
	// would exceed the mailbox cap.
	Put(mailbox string, raw []byte) (PutResult, error)
	// Accepts reports whether a message for mailbox could be stored now.
	Accepts(mailbox string) error
	// List returns non-expired messages newest first. With after set, only
	// messages with a greater ID are returned. limit <= 0 means 100.
	List(mailbox, after string, limit int) ([]message.Summary, error)
	// Get returns a message summary and its raw bytes.
	Get(mailbox, id string) (message.Summary, []byte, error)
	Delete(mailbox, id string) error
	// DeleteMailbox removes every message in the mailbox (idempotent).
	DeleteMailbox(mailbox string) ([]Ref, error)
	// Sweep removes expired messages and enforces the total size cap.
	Sweep() []Ref
	Stats() Stats
}

// Blobs holds raw message bytes for a Core.
type Blobs interface {
	Write(mailbox, id string, raw []byte) error
	Read(mailbox, id string) ([]byte, error)
	Remove(mailbox, id string) error
	// Cleanup is called when a mailbox has no messages left.
	Cleanup(mailbox string)
}

type box struct {
	items   []message.Summary // ascending by ID (oldest first)
	pending int               // Puts that reserved this mailbox but have not indexed yet
}

// Core is a Store built on a Blobs backend.
type Core struct {
	opts  Options
	blobs Blobs
	ids   *message.IDGen

	mu    sync.RWMutex
	boxes map[string]*box
	total int64
	count int
}

// NewCore returns a store over blobs, pre-populated with restored summaries
// (used by the fs store after scanning its directory).
func NewCore(blobs Blobs, opts Options, restored []message.Summary) *Core {
	opts.applyDefaults()
	c := &Core{opts: opts, blobs: blobs, ids: message.NewIDGen(opts.Clock), boxes: map[string]*box{}}
	for _, s := range restored {
		b := c.boxes[s.Mailbox]
		if b == nil {
			b = &box{}
			c.boxes[s.Mailbox] = b
		}
		b.items = append(b.items, s)
		c.total += int64(s.Size)
		c.count++
	}
	for _, b := range c.boxes {
		sort.Slice(b.items, func(i, j int) bool { return b.items[i].ID < b.items[j].ID })
	}
	return c
}

func (c *Core) expired(now time.Time, s message.Summary) bool {
	return now.Sub(s.ReceivedAt) >= c.opts.Retention
}

func search(items []message.Summary, id string) int {
	return sort.Search(len(items), func(i int) bool { return items[i].ID >= id })
}

// Accepts implements Store.
func (c *Core) Accepts(name string) error {
	if !mailbox.Valid(name) {
		return ErrInvalidMailbox
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if _, ok := c.boxes[name]; !ok && len(c.boxes) >= c.opts.MaxMailboxes {
		return ErrMailboxLimit
	}
	return nil
}

// Put implements Store.
func (c *Core) Put(name string, raw []byte) (PutResult, error) {
	if !mailbox.Valid(name) {
		return PutResult{}, ErrInvalidMailbox
	}
	c.mu.Lock()
	b := c.boxes[name]
	if b == nil {
		if len(c.boxes) >= c.opts.MaxMailboxes {
			c.mu.Unlock()
			return PutResult{}, ErrMailboxLimit
		}
		b = &box{}
		c.boxes[name] = b
	}
	b.pending++
	c.mu.Unlock()

	// Parsing and the (fsynced) write happen outside the lock so concurrent
	// deliveries overlap.
	id := c.ids.New()
	sum := message.SummaryFromRaw(name, id, raw, true)
	werr := c.blobs.Write(name, id, raw)

	c.mu.Lock()
	defer c.mu.Unlock()
	b.pending--
	if werr != nil {
		c.dropIfEmpty(name, b)
		return PutResult{}, werr
	}
	i := sort.Search(len(b.items), func(i int) bool { return b.items[i].ID > id })
	b.items = append(b.items, message.Summary{})
	copy(b.items[i+1:], b.items[i:])
	b.items[i] = sum
	c.total += int64(sum.Size)
	c.count++

	var evicted []Ref
	for len(b.items) > c.opts.MaxPerMailbox {
		evicted = append(evicted, c.remove(name, b.items[0].ID))
	}
	for c.total > c.opts.MaxTotalBytes {
		bn, bid, ok := c.oldestExcept(id)
		if !ok {
			break
		}
		evicted = append(evicted, c.remove(bn, bid))
	}
	return PutResult{Summary: sum, Evicted: evicted}, nil
}

// oldestExcept finds the globally oldest message other than keepID.
// Caller holds the lock.
func (c *Core) oldestExcept(keepID string) (name, id string, ok bool) {
	for n, b := range c.boxes {
		for _, s := range b.items { // at most two iterations: items are ascending
			if s.ID == keepID {
				continue
			}
			if !ok || s.ID < id {
				name, id, ok = n, s.ID, true
			}
			break
		}
	}
	return name, id, ok
}

// remove deletes one message from the index and the backend. Caller holds
// the lock and guarantees the message exists.
func (c *Core) remove(name, id string) Ref {
	b := c.boxes[name]
	i := search(b.items, id)
	s := b.items[i]
	b.items = append(b.items[:i], b.items[i+1:]...)
	_ = c.blobs.Remove(name, id)
	c.total -= int64(s.Size)
	c.count--
	c.dropIfEmpty(name, b)
	return Ref{Mailbox: name, ID: id}
}

func (c *Core) dropIfEmpty(name string, b *box) {
	if len(b.items) == 0 && b.pending == 0 {
		delete(c.boxes, name)
		c.blobs.Cleanup(name)
	}
}

// List implements Store.
func (c *Core) List(name, after string, limit int) ([]message.Summary, error) {
	if !mailbox.Valid(name) {
		return nil, ErrInvalidMailbox
	}
	if limit <= 0 {
		limit = 100
	}
	now := c.opts.Clock.Now()
	out := []message.Summary{}
	c.mu.RLock()
	if b := c.boxes[name]; b != nil {
		for i := len(b.items) - 1; i >= 0 && len(out) < limit; i-- {
			s := b.items[i]
			if (after != "" && s.ID <= after) || c.expired(now, s) {
				break // older items are older still
			}
			out = append(out, s)
		}
	}
	c.mu.RUnlock()

	var fixed []message.Summary
	for i := range out {
		if out[i].AttachmentCount >= 0 {
			continue
		}
		n := 0
		if raw, err := c.blobs.Read(name, out[i].ID); err == nil {
			n = len(message.Parse(raw).Attachments)
		}
		out[i].AttachmentCount = n
		fixed = append(fixed, out[i])
	}
	if len(fixed) > 0 {
		c.mu.Lock()
		if b := c.boxes[name]; b != nil {
			for _, f := range fixed {
				if i := search(b.items, f.ID); i < len(b.items) && b.items[i].ID == f.ID {
					b.items[i].AttachmentCount = f.AttachmentCount
				}
			}
		}
		c.mu.Unlock()
	}
	return out, nil
}

// Get implements Store.
func (c *Core) Get(name, id string) (message.Summary, []byte, error) {
	if !mailbox.Valid(name) {
		return message.Summary{}, nil, ErrInvalidMailbox
	}
	now := c.opts.Clock.Now()
	c.mu.RLock()
	var (
		s     message.Summary
		found bool
	)
	if b := c.boxes[name]; b != nil {
		if i := search(b.items, id); i < len(b.items) && b.items[i].ID == id {
			s, found = b.items[i], !c.expired(now, b.items[i])
		}
	}
	c.mu.RUnlock()
	if !found {
		return message.Summary{}, nil, ErrNotFound
	}
	raw, err := c.blobs.Read(name, id)
	if err != nil {
		return message.Summary{}, nil, ErrNotFound
	}
	if s.AttachmentCount < 0 {
		s.AttachmentCount = len(message.Parse(raw).Attachments)
	}
	return s, raw, nil
}

// Delete implements Store.
func (c *Core) Delete(name, id string) error {
	if !mailbox.Valid(name) {
		return ErrInvalidMailbox
	}
	now := c.opts.Clock.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	b := c.boxes[name]
	if b == nil {
		return ErrNotFound
	}
	i := search(b.items, id)
	if i >= len(b.items) || b.items[i].ID != id || c.expired(now, b.items[i]) {
		return ErrNotFound
	}
	c.remove(name, id)
	return nil
}

// DeleteMailbox implements Store.
func (c *Core) DeleteMailbox(name string) ([]Ref, error) {
	if !mailbox.Valid(name) {
		return nil, ErrInvalidMailbox
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	b := c.boxes[name]
	if b == nil {
		return nil, nil
	}
	ids := make([]string, len(b.items))
	for i, s := range b.items {
		ids[i] = s.ID
	}
	refs := make([]Ref, 0, len(ids))
	for _, id := range ids {
		refs = append(refs, c.remove(name, id))
	}
	return refs, nil
}

// Sweep implements Store.
func (c *Core) Sweep() []Ref {
	now := c.opts.Clock.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	var removed []Ref
	for name, b := range c.boxes {
		var ids []string
		for _, s := range b.items {
			if !c.expired(now, s) {
				break
			}
			ids = append(ids, s.ID)
		}
		for _, id := range ids {
			removed = append(removed, c.remove(name, id))
		}
	}
	for c.total > c.opts.MaxTotalBytes {
		bn, bid, ok := c.oldestExcept("")
		if !ok {
			break
		}
		removed = append(removed, c.remove(bn, bid))
	}
	return removed
}

// Stats implements Store.
func (c *Core) Stats() Stats {
	c.mu.RLock()
	defer c.mu.RUnlock()
	st := Stats{Messages: c.count, Bytes: c.total}
	for _, b := range c.boxes {
		if len(b.items) > 0 {
			st.Mailboxes++
		}
	}
	return st
}
