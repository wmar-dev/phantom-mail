package clock

import (
	"testing"
	"time"
)

func TestFakeClock(t *testing.T) {
	start := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	f := NewFake(start)
	if !f.Now().Equal(start) {
		t.Fatal("start time wrong")
	}
	f.Advance(90 * time.Minute)
	if !f.Now().Equal(start.Add(90 * time.Minute)) {
		t.Fatal("Advance wrong")
	}
	f.Set(start)
	if !f.Now().Equal(start) {
		t.Fatal("Set wrong")
	}
}

func TestRealClockMoves(t *testing.T) {
	var c Clock = Real{}
	a := c.Now()
	time.Sleep(2 * time.Millisecond)
	if !c.Now().After(a) {
		t.Fatal("real clock did not advance")
	}
}
