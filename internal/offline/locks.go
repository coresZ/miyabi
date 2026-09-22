package offline

import (
	"context"
	"sync"

	"github.com/ppxb/miyabi/internal/syncx"
)

type offlineOperation struct {
	lock  syncx.ContextLock
	users int
}

// offlineOperations provides per-(account, magnet) mutual exclusion so that
// concurrent submissions or updates to the same magnet are serialized while
// unrelated magnets progress independently.
type offlineOperations struct {
	mu      sync.Mutex
	entries map[string]*offlineOperation
}

func (operations *offlineOperations) Lock(ctx context.Context, accountID, hash string) (func(), error) {
	key := accountID + ":" + hash
	operations.mu.Lock()
	if operations.entries == nil {
		operations.entries = make(map[string]*offlineOperation)
	}
	entry := operations.entries[key]
	if entry == nil {
		entry = &offlineOperation{}
		operations.entries[key] = entry
	}
	entry.users++
	operations.mu.Unlock()
	release := func() {
		operations.mu.Lock()
		defer operations.mu.Unlock()
		entry.users--
		if entry.users == 0 {
			delete(operations.entries, key)
		}
	}
	if err := entry.lock.Lock(ctx); err != nil {
		release()
		return nil, err
	}
	return func() { entry.lock.Unlock(); release() }, nil
}
