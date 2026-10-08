package bench

import (
	"fmt"
	"net/http"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"phantom-mail/internal/hub"
	"phantom-mail/internal/message"
)

// ingestRate delivers n messages over conns parallel SMTP connections
// (fsynced writes to disk) and returns messages per second.
func ingestRate(tb testing.TB, n, conns int) float64 {
	a := startApp(tb)
	var next atomic.Int64
	var wg sync.WaitGroup
	start := time.Now()
	for c := 0; c < conns; c++ {
		wg.Add(1)
		go func(c int) {
			defer wg.Done()
			for {
				i := int(next.Add(1))
				if i > n {
					return
				}
				if err := send(a.SMTPAddr(), fmt.Sprintf("box%d@localhost", i%50), i); err != nil {
					tb.Error(err)
					return
				}
			}
		}(c)
	}
	wg.Wait()
	return float64(n) / time.Since(start).Seconds()
}

// Plan goal: at least 100 messages/s with fsynced writes on one vCPU. Faster
// storage does better; the measured rate is logged and only the floor gates.
func TestIngestThroughputGate(t *testing.T) {
	if testing.Short() {
		t.Skip("short mode")
	}
	prev := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(prev)
	floor := 100.0
	if !strict() {
		floor = 20 // shared machine or race detector: only catch big regressions
	}
	rate := ingestRate(t, 400, 4)
	t.Logf("ingest: %.0f messages/s on 1 vCPU with fsynced writes (gate: >= %.0f)", rate, floor)
	if rate < floor {
		t.Fatalf("ingest rate %.0f msg/s is below the %.0f msg/s floor", rate, floor)
	}
}

func BenchmarkIngest(b *testing.B) {
	a := startApp(b)
	b.ResetTimer()
	var next atomic.Int64
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			i := int(next.Add(1))
			if err := send(a.SMTPAddr(), fmt.Sprintf("box%d@localhost", i%50), i); err != nil {
				b.Error(err)
				return
			}
		}
	})
	b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "msg/s")
}

func fill(tb testing.TB, a interface{ SMTPAddr() string }, box string, n int) {
	for i := 0; i < n; i++ {
		if err := send(a.SMTPAddr(), box+"@localhost", i); err != nil {
			tb.Fatal(err)
		}
	}
}

// Plan goal: list p95 < 10 ms and message fetch p95 < 20 ms at 100 messages.
func TestReadLatencyGates(t *testing.T) {
	if testing.Short() {
		t.Skip("short mode")
	}
	a := startApp(t)
	fill(t, a, "alice", 100)
	base := "http://" + a.HTTPAddr() + "/api/v1/mailboxes/alice/messages"
	c := &http.Client{}

	var list struct {
		Messages []struct {
			ID string `json:"id"`
		} `json:"messages"`
	}
	if _, code, err := timeGET(c, base); err != nil || code != 200 {
		t.Fatal(code, err)
	}
	getJSON(t, c, base, &list)
	if len(list.Messages) != 100 {
		t.Fatalf("setup: %d messages", len(list.Messages))
	}
	id := list.Messages[50].ID

	measure := func(url string) time.Duration {
		var ds []time.Duration
		for i := 0; i < 500; i++ {
			d, code, err := timeGET(c, url)
			if err != nil || code != 200 {
				t.Fatal(code, err)
			}
			ds = append(ds, d)
		}
		return percentile(ds, 0.95)
	}
	listP95 := measure(base)
	getP95 := measure(base + "/" + id)
	listLimit := time.Duration(scale(10)) * time.Millisecond
	getLimit := time.Duration(scale(20)) * time.Millisecond
	t.Logf("list p95 = %v (limit %v), get p95 = %v (limit %v); strict=%v", listP95, listLimit, getP95, getLimit, strict())
	if listP95 >= listLimit {
		t.Errorf("list p95 = %v", listP95)
	}
	if getP95 >= getLimit {
		t.Errorf("get p95 = %v", getP95)
	}
}

func BenchmarkListMessages(b *testing.B) {
	a := startApp(b)
	fill(b, a, "alice", 100)
	url := "http://" + a.HTTPAddr() + "/api/v1/mailboxes/alice/messages"
	c := &http.Client{}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, code, err := timeGET(c, url); err != nil || code != 200 {
			b.Fatal(code, err)
		}
	}
}

func BenchmarkMIMEParse(b *testing.B) {
	raw := []byte("From: s@example.com\r\nSubject: bench\r\nMIME-Version: 1.0\r\nContent-Type: multipart/alternative; boundary=x\r\n\r\n" +
		"--x\r\nContent-Type: text/plain\r\n\r\nYour code is 123456\r\n--x\r\nContent-Type: text/html\r\n\r\n<p>Your code is <b>123456</b></p>\r\n--x--\r\n")
	b.SetBytes(int64(len(raw)))
	for i := 0; i < b.N; i++ {
		if p := message.Parse(raw); p.Text == "" {
			b.Fatal("parse failed")
		}
	}
}

func BenchmarkHubFanOut100(b *testing.B) {
	h := hub.New()
	var chans []<-chan hub.Event
	for i := 0; i < 100; i++ {
		ch, cancel := h.Subscribe("alice")
		defer cancel()
		chans = append(chans, ch)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		h.Publish("alice", hub.Event{Type: hub.EventMessage})
		for _, ch := range chans {
			select {
			case <-ch:
			default:
			}
		}
	}
}
