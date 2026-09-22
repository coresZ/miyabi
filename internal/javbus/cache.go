package javbus

import (
	"sync"
	"time"
)

type detailCacheEntry struct {
	html      string
	gid       string
	uc        string
	img       string
	fetchedAt time.Time
}

type detailCache struct {
	mu      sync.RWMutex
	entries map[string]detailCacheEntry
	ttl     time.Duration
}

func newDetailCache(ttl time.Duration) *detailCache {
	return &detailCache{
		entries: make(map[string]detailCacheEntry),
		ttl:     ttl,
	}
}

func (c *detailCache) get(code string) (detailCacheEntry, bool) {
	c.mu.RLock()
	entry, ok := c.entries[code]
	c.mu.RUnlock()
	if !ok {
		return detailCacheEntry{}, false
	}
	if time.Since(entry.fetchedAt) > c.ttl {
		c.mu.Lock()
		delete(c.entries, code)
		c.mu.Unlock()
		return detailCacheEntry{}, false
	}
	return entry, true
}

func (c *detailCache) set(code string, html, gid, uc, img string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Bound cache size to prevent unbounded memory growth.
	if len(c.entries) >= 512 {
		now := time.Now()
		for k, v := range c.entries {
			if now.Sub(v.fetchedAt) > c.ttl {
				delete(c.entries, k)
			}
		}
	}

	c.entries[code] = detailCacheEntry{
		html:      html,
		gid:       gid,
		uc:        uc,
		img:       img,
		fetchedAt: time.Now(),
	}
}
