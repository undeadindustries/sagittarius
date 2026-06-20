package credentials

import (
	"sync"
	"time"
)

const defaultCacheTTL = 30 * time.Second

type cacheEntry struct {
	value     string
	expiresAt time.Time
}

type readThroughCache struct {
	mu    sync.RWMutex
	ttl   time.Duration
	items map[string]cacheEntry
}

func newReadThroughCache(ttl time.Duration) *readThroughCache {
	return &readThroughCache{
		ttl:   ttl,
		items: make(map[string]cacheEntry),
	}
}

func (c *readThroughCache) get(key string) (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	entry, ok := c.items[key]
	if !ok || time.Now().After(entry.expiresAt) {
		return "", false
	}
	return entry.value, true
}

func (c *readThroughCache) set(key, value string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items[key] = cacheEntry{
		value:     value,
		expiresAt: time.Now().Add(c.ttl),
	}
}

func (c *readThroughCache) delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.items, key)
}

func (c *readThroughCache) clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	clear(c.items)
}

var apiKeyCache = newReadThroughCache(defaultCacheTTL)

func resetAPIKeyCache() {
	apiKeyCache.clear()
}

// SetCacheTTLForTesting adjusts cache TTL (restore with ResetForTesting).
func SetCacheTTLForTesting(ttl time.Duration) {
	apiKeyCache.mu.Lock()
	defer apiKeyCache.mu.Unlock()
	apiKeyCache.ttl = ttl
}
