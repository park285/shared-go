package promptguard

import (
	"sync"
	"time"
)

type ttlEntry[V any] struct {
	value     V
	expiresAt time.Time
}

// ttlCache는 Guard 판정 결과를 크기·TTL 상한 안에서 보관한다. Guard 구현 세부라 공개하지 않는다.
type ttlCache[K comparable, V any] struct {
	mu      sync.RWMutex
	entries map[K]ttlEntry[V]
	maxSize int
	ttl     time.Duration
	now     func() time.Time
}

func newTTLCache[K comparable, V any](maxSize int, ttl time.Duration, now func() time.Time) *ttlCache[K, V] {
	if maxSize <= 0 {
		maxSize = 1024
	}

	if now == nil {
		now = time.Now
	}

	return &ttlCache[K, V]{
		entries: make(map[K]ttlEntry[V]),
		maxSize: maxSize,
		ttl:     ttl,
		now:     now,
	}
}

func (c *ttlCache[K, V]) Get(key K) (V, bool) {
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

func (c *ttlCache[K, V]) Set(key K, value V) {
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

func (c *ttlCache[K, V]) Len() int {
	if c == nil {
		return 0
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	return len(c.entries)
}

func (c *ttlCache[K, V]) ensureCapacity() {
	if len(c.entries) < c.maxSize {
		return
	}

	c.deleteExpiredEntries(c.now())

	if len(c.entries) < c.maxSize {
		return
	}

	c.deleteOneEntry()
}

func (c *ttlCache[K, V]) deleteExpiredEntries(now time.Time) {
	for key, entry := range c.entries {
		if !entry.expiresAt.IsZero() && now.After(entry.expiresAt) {
			delete(c.entries, key)
		}

		if len(c.entries) < c.maxSize {
			return
		}
	}
}

func (c *ttlCache[K, V]) deleteOneEntry() {
	for key := range c.entries {
		delete(c.entries, key)

		return
	}
}

func (c *ttlCache[K, V]) newEntry(value V) ttlEntry[V] {
	entry := ttlEntry[V]{value: value}

	if c.ttl > 0 {
		entry.expiresAt = c.now().Add(c.ttl)
	}

	return entry
}

func (c *ttlCache[K, V]) resolveExpired(key K) (V, bool) {
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
