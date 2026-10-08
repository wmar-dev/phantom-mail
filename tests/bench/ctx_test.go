package bench

import (
	"context"
	"time"
)

func ctx5s() context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	_ = cancel // released when the timeout elapses; benchmarks are short-lived
	return ctx
}
