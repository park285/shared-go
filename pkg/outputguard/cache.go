package outputguard

import (
	"crypto/sha256"
	"encoding/binary"
	"sync"
	"time"
)

const (
	defaultProtectedCacheCapacity = 256
	defaultProtectedCacheTTL      = time.Hour
)

type protectedCacheEntry struct {
	index     *protectedIndex
	expiresAt time.Time
}

type protectedIndexCache struct {
	mu       sync.Mutex
	entries  map[[sha256.Size]byte]protectedCacheEntry
	order    [][sha256.Size]byte
	capacity int
	ttl      time.Duration
	now      func() time.Time
}

func newProtectedIndexCache(capacity int, ttl time.Duration, now func() time.Time) *protectedIndexCache {
	if capacity <= 0 {
		capacity = defaultProtectedCacheCapacity
	}
	if ttl <= 0 {
		ttl = defaultProtectedCacheTTL
	}
	if now == nil {
		now = time.Now
	}

	return &protectedIndexCache{
		entries:  make(map[[sha256.Size]byte]protectedCacheEntry, capacity),
		order:    make([][sha256.Size]byte, 0, capacity),
		capacity: capacity,
		ttl:      ttl,
		now:      now,
	}
}

func (cache *protectedIndexCache) loadOrBuild(protectedTexts []string) *protectedIndex {
	key := protectedTextsDigest(protectedTexts)
	if index, ok := cache.get(key); ok {
		return index
	}

	index := buildProtectedIndex(protectedTexts)
	cache.put(key, index)

	return index
}

func (cache *protectedIndexCache) get(key [sha256.Size]byte) (*protectedIndex, bool) {
	now := cache.now()
	cache.mu.Lock()
	defer cache.mu.Unlock()

	entry, ok := cache.entries[key]
	if !ok {
		return nil, false
	}
	if !now.Before(entry.expiresAt) {
		delete(cache.entries, key)
		cache.removeFromOrder(key)

		return nil, false
	}

	return entry.index, true
}

func (cache *protectedIndexCache) put(key [sha256.Size]byte, index *protectedIndex) {
	now := cache.now()
	cloned := cloneProtectedIndex(index)
	cache.mu.Lock()
	defer cache.mu.Unlock()

	if _, exists := cache.entries[key]; exists {
		cache.entries[key] = protectedCacheEntry{index: cloned, expiresAt: now.Add(cache.ttl)}

		return
	}
	if len(cache.entries) >= cache.capacity && len(cache.order) > 0 {
		oldest := cache.order[0]
		cache.order = cache.order[1:]
		delete(cache.entries, oldest)
	}
	cache.entries[key] = protectedCacheEntry{index: cloned, expiresAt: now.Add(cache.ttl)}
	cache.order = append(cache.order, key)
}

func cloneProtectedIndex(source *protectedIndex) *protectedIndex {
	if source == nil {
		return &protectedIndex{}
	}
	cloned := &protectedIndex{
		entries:      make([]protectedEntry, len(source.entries)),
		tokenAnchors: make(map[uint64][]anchorRef, len(source.tokenAnchors)),
		runeAnchors:  make(map[uint64][]anchorRef, len(source.runeAnchors)),
	}
	for i := range source.entries {
		cloned.entries[i] = protectedEntry{
			runes:        append([]rune(nil), source.entries[i].runes...),
			tokens:       append([]tokenSpan(nil), source.entries[i].tokens...),
			runeFallback: source.entries[i].runeFallback,
		}
	}
	for hash, refs := range source.tokenAnchors {
		cloned.tokenAnchors[hash] = append([]anchorRef(nil), refs...)
	}
	for hash, refs := range source.runeAnchors {
		cloned.runeAnchors[hash] = append([]anchorRef(nil), refs...)
	}

	return cloned
}

func (cache *protectedIndexCache) removeFromOrder(key [sha256.Size]byte) {
	for i := range cache.order {
		if cache.order[i] == key {
			cache.order = append(cache.order[:i], cache.order[i+1:]...)

			return
		}
	}
}

func (cache *protectedIndexCache) len() int {
	cache.mu.Lock()
	defer cache.mu.Unlock()

	return len(cache.entries)
}

func protectedTextsDigest(protectedTexts []string) [sha256.Size]byte {
	hash := sha256.New()
	var length [8]byte
	for _, text := range protectedTexts {
		binary.BigEndian.PutUint64(length[:], uint64(len(text)))
		_, _ = hash.Write(length[:])
		_, _ = hash.Write([]byte(text))
	}

	var digest [sha256.Size]byte
	copy(digest[:], hash.Sum(nil))

	return digest
}
