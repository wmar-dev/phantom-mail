// Package bench holds the performance benchmarks and the gates that back the
// numbers in the plan: ingest throughput, read latency, memory, and startup.
// Run the benchmarks with "make bench"; the gates run with "go test".
package bench

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/smtp"
	"sort"
	"testing"
	"time"

	"phantom-mail/internal/app"
	"phantom-mail/internal/clock"
	"phantom-mail/internal/config"
)

func startApp(tb testing.TB, mutate ...func(*config.Config)) *app.App {
	tb.Helper()
	cfg := config.Defaults()
	cfg.HTTPAddr, cfg.SMTPAddr, cfg.DataDir = "127.0.0.1:0", "127.0.0.1:0", tb.TempDir()
	// Benchmarks hammer one address; the production rate limits would get in the way.
	cfg.RateSMTPConn, cfg.RateSMTPMessage, cfg.RateMailbox, cfg.RateHTTP = 1e7, 1e7, 1e7, 1e7
	cfg.MaxMailboxes = 100000
	for _, m := range mutate {
		m(&cfg)
	}
	a, err := app.New(cfg, app.NewLogger(io.Discard, "error"), clock.Real{})
	if err != nil {
		tb.Fatal(err)
	}
	if err := a.Start(); err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() { _ = a.Shutdown(ctx5s()) })
	return a
}

func send(addr, to string, i int) error {
	msg := fmt.Sprintf("From: Sender <s@example.com>\r\nTo: %s\r\nSubject: bench message %d\r\n\r\nYour code is %06d\r\n", to, i, i%1000000)
	return smtp.SendMail(addr, nil, "s@example.com", []string{to}, []byte(msg))
}

func percentile(d []time.Duration, p float64) time.Duration {
	if len(d) == 0 {
		return 0
	}
	s := append([]time.Duration(nil), d...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	return s[int(float64(len(s)-1)*p)]
}

func timeGET(c *http.Client, url string) (time.Duration, int, error) {
	start := time.Now()
	resp, err := c.Get(url)
	if err != nil {
		return 0, 0, err
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	return time.Since(start), resp.StatusCode, nil
}

func getJSON(tb testing.TB, c *http.Client, url string, v any) {
	tb.Helper()
	resp, err := c.Get(url)
	if err != nil {
		tb.Fatal(err)
	}
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		tb.Fatal(err)
	}
}
