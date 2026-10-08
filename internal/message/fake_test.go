package message

import "time"

// fakeNow is a fixed clock so ID tests exercise the same-millisecond path.
type fakeNow struct{}

func (fakeNow) Now() time.Time { return time.Date(2026, 10, 7, 10, 0, 0, 123456789, time.UTC) }
