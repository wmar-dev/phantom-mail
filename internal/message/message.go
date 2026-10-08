// Package message defines the message model, ID generation, and a tolerant
// MIME parser built on the standard library.
package message

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

// Summary is the list view of a message. Attachment count is -1 only inside
// the store while it has not been computed yet; callers never see it.
type Summary struct {
	ID              string    `json:"id"`
	Mailbox         string    `json:"mailbox"`
	From            string    `json:"from"`
	To              []string  `json:"to"`
	Subject         string    `json:"subject"`
	ReceivedAt      time.Time `json:"received_at"`
	Size            int       `json:"size"`
	AttachmentCount int       `json:"attachment_count"`
}

// AttachmentInfo describes one attachment without its content.
type AttachmentInfo struct {
	Index       int    `json:"index"`
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	Size        int    `json:"size"`
}

// Attachment is an attachment with its decoded content.
type Attachment struct {
	AttachmentInfo
	Data []byte
}

// Message is the full view returned for a single message.
type Message struct {
	Summary
	Text        string           `json:"text"`
	HTML        string           `json:"html"`
	Attachments []AttachmentInfo `json:"attachments"`
}

// Parsed holds the decoded bodies and attachments of a raw message.
type Parsed struct {
	Text        string
	HTML        string
	Attachments []Attachment
}

// Full combines a summary with a parsed body into the API view.
func Full(s Summary, p Parsed) Message {
	infos := make([]AttachmentInfo, len(p.Attachments))
	for i, a := range p.Attachments {
		infos[i] = a.AttachmentInfo
	}
	s.AttachmentCount = len(infos)
	return Message{Summary: s, Text: p.Text, HTML: p.HTML, Attachments: infos}
}

// SummaryFromRaw builds the list view of a raw message. When countAttachments
// is false the attachment count is left at -1 to be computed later.
func SummaryFromRaw(mailbox, id string, raw []byte, countAttachments bool) Summary {
	h := ParseHeaders(raw)
	ts, _ := IDTime(id)
	to := h.To
	if to == nil {
		to = []string{}
	}
	s := Summary{
		ID:              id,
		Mailbox:         mailbox,
		From:            h.From,
		To:              to,
		Subject:         h.Subject,
		ReceivedAt:      ts,
		Size:            len(raw),
		AttachmentCount: -1,
	}
	if countAttachments {
		s.AttachmentCount = len(Parse(raw).Attachments)
	}
	return s
}

// Now is the minimal clock interface IDGen needs.
type Now interface{ Now() time.Time }

// IDGen produces time-ordered, strictly increasing IDs: 48 bits of
// millisecond timestamp followed by 80 random bits, as 32 hex characters.
// Within one millisecond the random part is incremented so order is kept.
type IDGen struct {
	clk      Now
	mu       sync.Mutex
	lastMs   uint64
	lastRand [10]byte
}

// NewIDGen returns a generator reading time from clk.
func NewIDGen(clk Now) *IDGen { return &IDGen{clk: clk} }

// New returns the next ID.
func (g *IDGen) New() string {
	ms := uint64(g.clk.Now().UnixMilli())
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.lastMs != 0 && ms <= g.lastMs {
		ms = g.lastMs
		if !incr(&g.lastRand) { // random part overflowed: move to next ms
			ms++
			g.fresh()
		}
	} else {
		g.fresh()
	}
	g.lastMs = ms
	var buf [16]byte
	buf[0], buf[1], buf[2] = byte(ms>>40), byte(ms>>32), byte(ms>>24)
	buf[3], buf[4], buf[5] = byte(ms>>16), byte(ms>>8), byte(ms)
	copy(buf[6:], g.lastRand[:])
	return hex.EncodeToString(buf[:])
}

func (g *IDGen) fresh() {
	_, _ = rand.Read(g.lastRand[:])
	g.lastRand[0] &= 0x7f // leave headroom so increments rarely overflow
}

// incr adds one to b (big-endian) and reports whether it did not overflow.
func incr(b *[10]byte) bool {
	for i := len(b) - 1; i >= 0; i-- {
		b[i]++
		if b[i] != 0 {
			return true
		}
	}
	return false
}

// ValidID reports whether id has the shape produced by IDGen.
func ValidID(id string) bool {
	if len(id) != 32 {
		return false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// IDTime extracts the embedded timestamp from an ID.
func IDTime(id string) (time.Time, bool) {
	if !ValidID(id) {
		return time.Time{}, false
	}
	b, err := hex.DecodeString(id[:12])
	if err != nil {
		return time.Time{}, false
	}
	var ms int64
	for _, x := range b {
		ms = ms<<8 | int64(x)
	}
	return time.UnixMilli(ms).UTC(), true
}
