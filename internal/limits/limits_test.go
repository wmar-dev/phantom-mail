package limits

import (
	"testing"
	"time"

	"phantom-mail/internal/clock"
)

var epoch = time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)

func TestBurstThenDeny(t *testing.T) {
	clk := clock.NewFake(epoch)
	l := New(5, clk) // 5 per minute, burst 5
	for i := 0; i < 5; i++ {
		if !l.Allow("ip") {
			t.Fatalf("request %d denied within burst", i)
		}
	}
	if l.Allow("ip") {
		t.Fatal("request beyond burst allowed")
	}
}

func TestRefillOverTime(t *testing.T) {
	clk := clock.NewFake(epoch)
	l := New(60, clk) // one per second
	for i := 0; i < 60; i++ {
		l.Allow("ip")
	}
	if l.Allow("ip") {
		t.Fatal("expected deny")
	}
	clk.Advance(1100 * time.Millisecond)
	if !l.Allow("ip") {
		t.Fatal("token should have refilled")
	}
	if l.Allow("ip") {
		t.Fatal("only one token should have refilled")
	}
	clk.Advance(time.Hour)
	for i := 0; i < 60; i++ {
		if !l.Allow("ip") {
			t.Fatalf("burst %d denied after long idle", i)
		}
	}
	if l.Allow("ip") {
		t.Fatal("bucket must not exceed its capacity")
	}
}

func TestKeysAreIndependent(t *testing.T) {
	clk := clock.NewFake(epoch)
	l := New(1, clk)
	if !l.Allow("a") || l.Allow("a") {
		t.Fatal("a should be limited after one")
	}
	if !l.Allow("b") {
		t.Fatal("b should be unaffected")
	}
}

func TestIdleKeysAreCleanedUp(t *testing.T) {
	clk := clock.NewFake(epoch)
	l := New(10, clk)
	for i := 0; i < 500; i++ {
		l.Allow(string(rune('a'+i%26)) + string(rune('a'+i/26)))
	}
	if l.Len() == 0 {
		t.Fatal("expected tracked keys")
	}
	clk.Advance(time.Hour)
	l.Allow("trigger")
	l.Cleanup()
	if n := l.Len(); n > 1 {
		t.Fatalf("idle keys not removed: %d left", n)
	}
}

func TestNilLimiterAllowsEverything(t *testing.T) {
	var l *Limiter
	for i := 0; i < 1000; i++ {
		if !l.Allow("x") {
			t.Fatal("nil limiter must allow")
		}
	}
}

func TestConcurrentUse(t *testing.T) {
	l := New(100000, clock.Real{})
	done := make(chan struct{})
	for i := 0; i < 8; i++ {
		go func() {
			for j := 0; j < 1000; j++ {
				l.Allow("k")
			}
			done <- struct{}{}
		}()
	}
	for i := 0; i < 8; i++ {
		<-done
	}
}

func TestPeekDoesNotConsume(t *testing.T) {
	clk := clock.NewFake(epoch)
	l := New(2, clk)
	for i := 0; i < 10; i++ {
		if !l.Peek("k") {
			t.Fatal("Peek must not consume tokens")
		}
	}
	l.Allow("k")
	l.Allow("k")
	if l.Peek("k") {
		t.Fatal("Peek should report an exhausted key")
	}
	clk.Advance(31 * time.Second)
	if !l.Peek("k") {
		t.Fatal("token should have refilled")
	}
	var nilLimiter *Limiter
	if !nilLimiter.Peek("k") {
		t.Fatal("nil limiter allows everything")
	}
}
