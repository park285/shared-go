package promptguard

import (
	"sync"
	"time"
)

type ttlEntry[V any] struct {
	value     V
	expiresAt time.Time
}

type TTLCache[K comparable, V any] struct {
	mu      sync.RWMutex
	entries map[K]ttlEntry[V]
	maxSize int
	ttl     time.Duration
}

func NewTTLCache[K comparable, V any](maxSize int, ttl time.Duration) *TTLCache[K, V] {
	if maxSize <= 0 {
		maxSize = 1024
	}

	return &TTLCache[K, V]{
		entries: make(map[K]ttlEntry[V]),
		maxSize: maxSize,
		ttl:     ttl,
	}
}

func (c *TTLCache[K, V]) Get(key K) (V, bool) {
	c.mu.RLock()

	entry, ok := c.entries[key]
	c.mu.RUnlock()

	if !ok {
		var zero V

		return zero, false
	}

	if !entry.expiresAt.IsZero() && time.Now().After(entry.expiresAt) {
		c.mu.Lock()
		delete(c.entries, key)
		c.mu.Unlock()

		var zero V

		return zero, false
	}

	return entry.value, true
}

func (c *TTLCache[K, V]) Set(key K, value V) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.ensureCapacity()

	c.entries[key] = c.newEntry(value)
}

func (c *TTLCache[K, V]) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return len(c.entries)
}

func (c *TTLCache[K, V]) ensureCapacity() {
	if len(c.entries) < c.maxSize {
		return
	}

	c.deleteExpiredEntries(time.Now())

	if len(c.entries) < c.maxSize {
		return
	}

	c.deleteOneEntry()
}

func (c *TTLCache[K, V]) deleteExpiredEntries(now time.Time) {
	for key, entry := range c.entries {
		if !entry.expiresAt.IsZero() && now.After(entry.expiresAt) {
			delete(c.entries, key)
		}

		if len(c.entries) < c.maxSize {
			return
		}
	}
}

func (c *TTLCache[K, V]) deleteOneEntry() {
	for key := range c.entries {
		delete(c.entries, key)

		return
	}
}

func (c *TTLCache[K, V]) newEntry(value V) ttlEntry[V] {
	entry := ttlEntry[V]{value: value}

	if c.ttl > 0 {
		entry.expiresAt = time.Now().Add(c.ttl)
	}

	return entry
}
