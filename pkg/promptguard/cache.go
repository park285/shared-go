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
	now     func() time.Time
}

func NewTTLCache[K comparable, V any](maxSize int, ttl time.Duration) *TTLCache[K, V] {
	return newTTLCache[K, V](maxSize, ttl, time.Now)
}

func newTTLCache[K comparable, V any](maxSize int, ttl time.Duration, now func() time.Time) *TTLCache[K, V] {
	if maxSize <= 0 {
		maxSize = 1024
	}

	if now == nil {
		now = time.Now
	}

	return &TTLCache[K, V]{
		entries: make(map[K]ttlEntry[V]),
		maxSize: maxSize,
		ttl:     ttl,
		now:     now,
	}
}

func (c *TTLCache[K, V]) Get(key K) (V, bool) {
	if c == nil {
		var zero V

		return zero, false
	}

	now := c.now()
	c.mu.RLock()

	entry, ok := c.entries[key]
	c.mu.RUnlock()

	if !ok {
		var zero V

		return zero, false
	}

	if entry.expired(now) {
		return c.resolveExpired(key)
	}

	return entry.value, true
}

func (c *TTLCache[K, V]) Set(key K, value V) {
	if c == nil {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if _, ok := c.entries[key]; ok {
		c.entries[key] = c.newEntry(value)

		return
	}

	c.ensureCapacity()

	c.entries[key] = c.newEntry(value)
}

func (c *TTLCache[K, V]) Len() int {
	if c == nil {
		return 0
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	return len(c.entries)
}

func (c *TTLCache[K, V]) keys() []K {
	if c == nil {
		return nil
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	keys := make([]K, 0, len(c.entries))
	for key := range c.entries {
		keys = append(keys, key)
	}

	return keys
}

func (c *TTLCache[K, V]) ensureCapacity() {
	if len(c.entries) < c.maxSize {
		return
	}

	c.deleteExpiredEntries(c.now())

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
		entry.expiresAt = c.now().Add(c.ttl)
	}

	return entry
}

func (c *TTLCache[K, V]) resolveExpired(key K) (V, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry, ok := c.entries[key]
	if !ok {
		var zero V

		return zero, false
	}

	if !entry.expired(c.now()) {
		return entry.value, true
	}

	delete(c.entries, key)

	var zero V

	return zero, false
}

func (e ttlEntry[V]) expired(now time.Time) bool {
	return !e.expiresAt.IsZero() && now.After(e.expiresAt)
}
