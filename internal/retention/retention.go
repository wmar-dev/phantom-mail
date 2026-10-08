// Package retention runs the background sweep that physically removes expired
// messages and enforces the total size cap. Reads already treat expired
// messages as gone, so the sweep timing never affects what clients see.
package retention

import (
	"context"
	"log/slog"
	"time"
)

// Sweeper removes expired messages and reports how many it removed.
type Sweeper interface {
	Sweep() int
}

// Run sweeps once immediately (so a restart after downtime cleans up at once),
// then every interval, until ctx is cancelled. A panic in one sweep is logged
// and does not stop later sweeps.
func Run(ctx context.Context, s Sweeper, interval time.Duration, log *slog.Logger) {
	sweep := func() {
		defer func() {
			if r := recover(); r != nil {
				log.Error("retention sweep panicked", "panic", r)
			}
		}()
		if n := s.Sweep(); n > 0 {
			log.Info("expired messages removed", "count", n)
		}
	}
	sweep()
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			sweep()
		}
	}
}
