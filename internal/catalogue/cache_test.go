package catalogue

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

func TestResponseCacheSharesLoadsWithoutSharingCallerCancellation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cache := newResponseCache[int](2, time.Minute)
		var calls atomic.Int32
		finish := make(chan struct{})
		load := func(ctx context.Context) (int, error) {
			calls.Add(1)
			select {
			case <-finish:
				return 42, nil
			case <-ctx.Done():
				return 0, ctx.Err()
			}
		}
		first, cancel := context.WithCancel(t.Context())
		firstResult := make(chan error, 1)
		go func() {
			_, err := cache.get(first, "same", load)
			firstResult <- err
		}()
		synctest.Wait()
		secondResult := make(chan int, 1)
		go func() {
			value, err := cache.get(t.Context(), "same", load)
			if err != nil {
				t.Error(err)
			}
			secondResult <- value
		}()
		synctest.Wait()
		cancel()
		if err := <-firstResult; !errors.Is(err, context.Canceled) {
			t.Fatalf("first caller error = %v", err)
		}
		close(finish)
		if got := <-secondResult; got != 42 || calls.Load() != 1 {
			t.Fatalf("second caller = %d, upstream calls = %d", got, calls.Load())
		}
	})
}

func TestResponseCacheCancelsAbandonedLoad(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cache := newResponseCache[int](2, time.Minute)
		ctx, cancel := context.WithCancel(t.Context())
		upstreamCanceled := make(chan struct{})
		finishOldLoad := make(chan struct{})
		finished := make(chan struct{})
		go func() {
			_, _ = cache.get(ctx, "abandon", func(callCtx context.Context) (int, error) {
				defer close(finished)
				select {
				case <-callCtx.Done():
					close(upstreamCanceled)
					<-finishOldLoad
					return 0, callCtx.Err()
				}
			})
		}()
		synctest.Wait()
		cancel()
		synctest.Wait()
		select {
		case <-upstreamCanceled:
		default:
			t.Fatal("abandoned upstream request was not canceled")
		}
		close(finishOldLoad)
		<-finished
	})
}

func TestResponseCacheEvictsLeastRecentlyUsedAtCapacity(t *testing.T) {
	cache := newResponseCache[string](2, time.Hour)
	ctx := t.Context()
	load := func(val string) func(context.Context) (string, error) {
		return func(context.Context) (string, error) { return val, nil }
	}
	_, _ = cache.get(ctx, "a", load("val-a"))
	_, _ = cache.get(ctx, "b", load("val-b"))
	// Access "a" so "b" becomes the least recently used
	_, _ = cache.get(ctx, "a", load("val-a"))
	// Insert "c", which should evict "b"
	_, _ = cache.get(ctx, "c", load("val-c"))

	var reloadedB bool
	_, _ = cache.get(ctx, "b", func(context.Context) (string, error) {
		reloadedB = true
		return "reloaded-b", nil
	})
	if !reloadedB {
		t.Fatal("b was not evicted")
	}
}
