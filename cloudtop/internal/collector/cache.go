package collector

import (
	"sync"
	"time"
)

// Cache interface for flexible caching backends
type Cache interface {
	Get(key string) (interface{}, bool)
	Set(key string, value interface{})
	Delete(key string)
	Clear()
}

// MemoryCache is an in-memory cache with TTL
type MemoryCache struct {
	mu      sync.RWMutex
	items   map[string]*cacheItem
	ttl     time.Duration
	maxSize int
}

type cacheItem struct {
	value      interface{}
	expiration time.Time
}

// NewMemoryCache creates a new in-memory cache
func NewMemoryCache(ttl time.Duration, maxSize int) *MemoryCache {
	cache := &MemoryCache{
		items:   make(map[string]*cacheItem),
		ttl:     ttl,
		maxSize: maxSize,
	}

	// Start cleanup goroutine
	go cache.cleanup()

	return cache
}

// Get retrieves a value from cache
func (c *MemoryCache) Get(key string) (interface{}, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	item, ok := c.items[key]
	if !ok {
		return nil, false
	}

	if time.Now().After(item.expiration) {
		return nil, false
	}

	return item.value, true
}

// Set stores a value in cache
func (c *MemoryCache) Set(key string, value interface{}) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Simple eviction: if cache is full, remove oldest items
	if c.maxSize > 0 && len(c.items) >= c.maxSize {
		c.evictOldest()
	}

	c.items[key] = &cacheItem{
		value:      value,
		expiration: time.Now().Add(c.ttl),
	}
}

// Delete removes a value from cache
func (c *MemoryCache) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.items, key)
}

// Clear removes all items from cache
func (c *MemoryCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items = make(map[string]*cacheItem)
}

// evictOldest removes items that have expired or the oldest items
func (c *MemoryCache) evictOldest() {
	now := time.Now()
	var oldestKey string
	var oldestTime time.Time

	// First, remove all expired items
	for key, item := range c.items {
		if now.After(item.expiration) {
			delete(c.items, key)
		} else if oldestKey == "" || item.expiration.Before(oldestTime) {
			oldestKey = key
			oldestTime = item.expiration
		}
	}

	// If still over capacity, remove oldest
	if len(c.items) >= c.maxSize && oldestKey != "" {
		delete(c.items, oldestKey)
	}
}

// cleanup periodically removes expired items
func (c *MemoryCache) cleanup() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		c.mu.Lock()
		now := time.Now()
		for key, item := range c.items {
			if now.After(item.expiration) {
				delete(c.items, key)
			}
		}
		c.mu.Unlock()
	}
}

// NoopCache is a cache that does nothing (disabled caching)
type NoopCache struct{}

func NewNoopCache() *NoopCache {
	return &NoopCache{}
}

func (c *NoopCache) Get(key string) (interface{}, bool) {
	return nil, false
}

func (c *NoopCache) Set(key string, value interface{}) {}

func (c *NoopCache) Delete(key string) {}

func (c *NoopCache) Clear() {}
