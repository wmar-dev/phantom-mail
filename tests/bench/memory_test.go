package bench

import (
	"bufio"
	"fmt"
	"net/http"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// SC-007: 50 simultaneously active mailboxes, each with a live event stream,
// receiving mail concurrently, lose nothing, keep list requests fast, and keep
// memory bounded.
func TestFiftyActiveMailboxesUnderLoad(t *testing.T) {
	if testing.Short() {
		t.Skip("short mode")
	}
	const boxes, senders, perSender = 50, 8, 50 // 400 messages, 8 per mailbox
	a := startApp(t)
	base := "http://" + a.HTTPAddr() + "/api/v1/mailboxes/"

	// One live event stream per mailbox.
	var received [boxes]atomic.Int64
	var streams sync.WaitGroup
	for i := 0; i < boxes; i++ {
		resp, err := http.Get(fmt.Sprintf("%sload%d/events", base, i))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { resp.Body.Close() })
		streams.Add(1)
		go func(i int) {
			defer streams.Done()
			sc := bufio.NewScanner(resp.Body)
			for sc.Scan() {
				if strings.HasPrefix(sc.Text(), "event: message") {
					received[i].Add(1)
				}
			}
		}(i)
	}

	// Readers hammer list requests while mail is arriving.
	stopReaders := make(chan struct{})
	var latMu sync.Mutex
	var lats []time.Duration
	var readers sync.WaitGroup
	for r := 0; r < 4; r++ {
		readers.Add(1)
		go func(r int) {
			defer readers.Done()
			c := &http.Client{}
			for i := r; ; i += 7 {
				select {
				case <-stopReaders:
					return
				default:
				}
				d, code, err := timeGET(c, fmt.Sprintf("%sload%d/messages", base, i%boxes))
				if err != nil || code != 200 {
					t.Errorf("list: %d %v", code, err)
					return
				}
				latMu.Lock()
				lats = append(lats, d)
				latMu.Unlock()
				time.Sleep(time.Millisecond)
			}
		}(r)
	}

	var sent sync.WaitGroup
	for s := 0; s < senders; s++ {
		sent.Add(1)
		go func(s int) {
			defer sent.Done()
			for m := 0; m < perSender; m++ {
				n := s*perSender + m
				if err := send(a.SMTPAddr(), fmt.Sprintf("load%d@localhost", n%boxes), n); err != nil {
					t.Errorf("send: %v", err)
					return
				}
			}
		}(s)
	}
	sent.Wait()
	deadline := time.Now().Add(5 * time.Second)
	for {
		missing := 0
		for i := 0; i < boxes; i++ {
			if received[i].Load() < senders*perSender/boxes {
				missing++
			}
		}
		if missing == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d of %d live streams are missing events", missing, boxes)
		}
		time.Sleep(20 * time.Millisecond)
	}
	close(stopReaders)
	readers.Wait()

	// Nothing lost on the storage side either.
	c := &http.Client{}
	total := 0
	for i := 0; i < boxes; i++ {
		var list struct {
			Messages []struct{} `json:"messages"`
		}
		getJSON(t, c, fmt.Sprintf("%sload%d/messages", base, i), &list)
		total += len(list.Messages)
	}
	if total != senders*perSender {
		t.Fatalf("stored %d messages, want %d", total, senders*perSender)
	}

	p95 := percentile(lats, 0.95)
	limit := time.Duration(scale(50)) * time.Millisecond
	t.Logf("%d list requests during the load: p95 = %v (limit %v); strict=%v", len(lats), p95, limit, strict())
	if p95 >= limit {
		t.Errorf("list p95 = %v under load", p95)
	}

	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	t.Logf("heap in use after the load: %.1f MiB (gate < 64 MiB)", float64(m.HeapAlloc)/(1<<20))
	if m.HeapAlloc > 64<<20 {
		t.Errorf("heap = %d bytes", m.HeapAlloc)
	}
}
