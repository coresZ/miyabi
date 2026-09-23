package tasks

import (
	"context"
	"fmt"
	"log/slog"

	"golang.org/x/sync/errgroup"
)

// PoolQueue defines the queue operations required by the worker Pool.
type PoolQueue interface {
	Recover(context.Context, []Kind) error
	Claim(context.Context, []Kind) (*Job, error)
	Finish(context.Context, int, error) error
}

// PoolBus defines the notification wake operations required by the worker Pool.
type PoolBus interface {
	Pending() <-chan struct{}
}

// Pool coordinates concurrent background workers executing registered task handlers.
type Pool struct {
	queue    PoolQueue
	bus      PoolBus
	registry *Registry
	size     int
	logger   *slog.Logger
}

// NewPool initializes a new worker Pool.
func NewPool(queue PoolQueue, bus PoolBus, registry *Registry, size int, logger *slog.Logger) *Pool {
	if logger == nil {
		logger = slog.Default()
	}
	return &Pool{
		queue:    queue,
		bus:      bus,
		registry: registry,
		size:     size,
		logger:   logger,
	}
}

// Run starts the worker goroutines and waits for context cancellation.
func (pool *Pool) Run(ctx context.Context) error {
	kinds := pool.registry.Kinds()
	if err := pool.queue.Recover(ctx, kinds); err != nil {
		return err
	}
	group, ctx := errgroup.WithContext(ctx)
	for range pool.size {
		group.Go(func() error { return pool.runWorker(ctx, kinds) })
	}
	return group.Wait()
}

func (pool *Pool) runWorker(ctx context.Context, kinds []Kind) error {
	for ctx.Err() == nil {
		job, err := pool.queue.Claim(ctx, kinds)
		if ctx.Err() != nil {
			return nil
		}
		if err != nil {
			return err
		}
		if job == nil {
			select {
			case <-ctx.Done():
				return nil
			case <-pool.bus.Pending():
				continue
			}
		}
		handler, ok := pool.registry.Get(job.Type)
		if !ok {
			return fmt.Errorf("no handler registered for task type %s", job.Type)
		}
		pool.logger.InfoContext(ctx, "task started", "task_id", job.ID, "type", string(job.Type))
		runError := handler.Handle(ctx, *job)
		if ctx.Err() != nil {
			// Keep running state for startup recovery, rather than reporting a
			// service shutdown as a failed user task.
			return nil
		}
		if err := pool.queue.Finish(ctx, job.ID, runError); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("persist task result: %w", err)
		}
		if runError != nil {
			pool.logger.ErrorContext(ctx, "task failed", "task_id", job.ID, "type", string(job.Type), "error", runError)
		} else {
			pool.logger.InfoContext(ctx, "task completed", "task_id", job.ID, "type", string(job.Type))
		}
	}
	return nil
}
