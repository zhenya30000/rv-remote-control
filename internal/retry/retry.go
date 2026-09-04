package retry

import (
	"context"
	"log/slog"
	"time"
)

const (
	initialDelay  = 1 * time.Second
	maxDelay      = 30 * time.Second
	stableSession = 30 * time.Second
)

func Run(
	ctx context.Context,
	name string,
	fn func(context.Context) error,
) {
	delay := initialDelay

	for ctx.Err() == nil {
		started := time.Now()
		err := fn(ctx)

		if ctx.Err() != nil {
			return
		}

		if time.Since(started) >= stableSession {
			delay = initialDelay
		}

		slog.Warn(
			"component stopped; retrying",
			"component", name,
			"error", err,
			"delay", delay,
		)

		timer := time.NewTimer(delay)

		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return
		case <-timer.C:
		}

		delay *= 2
		if delay > maxDelay {
			delay = maxDelay
		}
	}
}
