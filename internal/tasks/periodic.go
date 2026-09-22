package tasks

import (
	"context"
	"log/slog"
	"time"
)

// RunPeriodic runs fn periodically at the given interval or whenever a signal is received on wake.
// It executes fn once immediately upon start. If wake is nil, fn only runs on ticker ticks.
func RunPeriodic(ctx context.Context, logger *slog.Logger, name string, interval time.Duration, wake <-chan struct{}, fn func(context.Context) error) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if err := fn(ctx); err != nil && ctx.Err() == nil {
			logger.ErrorContext(ctx, name, "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-wake:
		}
	}
}
