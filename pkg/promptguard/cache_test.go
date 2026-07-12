package promptguard

import (
	"sync"
	"testing"
	"time"
)

func TestTTLCacheGetSetAndExpire(t *testing.T) {
	t.Parallel()

	cache := NewTTLCache[string, int](2, 20*time.Millisecond)
	cache.Set("a", 1)

	if got, ok := cache.Get("a"); !ok || got != 1 {
		t.Fatalf("Get() = (%v, %v), want (1, true)", got, ok)
	}

	time.Sleep(30 * time.Millisecond)

	if _, ok := cache.Get("a"); ok {
		t.Fatal("Get() expired entry ok = true, want false")
	}
}

func TestTTLCacheEvictionAndDefaults(t *testing.T) {
	t.Parallel()

	cache := NewTTLCache[string, int](0, 0)
	if cache.maxSize != 1024 {
		t.Fatalf("maxSize = %d, want %d", cache.maxSize, 1024)
	}

	small := NewTTLCache[string, int](1, 0)
	small.Set("a", 1)
	small.Set("b", 2)

	if small.Len() != 1 {
		t.Fatalf("Len() = %d, want %d", small.Len(), 1)
	}

	if _, ok := small.Get("b"); !ok {
		t.Fatal("Get(b) ok = false, want true")
	}
}

func TestTTLCacheUpdateAtCapacityPreservesOtherEntry(t *testing.T) {
	t.Parallel()

	for range 128 {
		cache := NewTTLCache[string, int](2, time.Minute)
		cache.Set("a", 1)
		cache.Set("b", 2)
		cache.Set("a", 3)

		if got, ok := cache.Get("a"); !ok || got != 3 {
			t.Fatalf("Get(a) = (%d, %v), want (3, true)", got, ok)
		}
		if got, ok := cache.Get("b"); !ok || got != 2 {
			t.Fatalf("Get(b) = (%d, %v), want (2, true)", got, ok)
		}
	}
}

func TestTTLCacheExpirationUsesInjectedClock(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 12, 0, 0, 0, 0, time.UTC)
	var mu sync.Mutex
	clock := func() time.Time {
		mu.Lock()
		defer mu.Unlock()

		return now
	}
	cache := newTTLCache[string, int](2, time.Minute, clock)
	cache.Set("a", 1)

	mu.Lock()
	now = now.Add(2 * time.Minute)
	mu.Unlock()

	if _, ok := cache.Get("a"); ok {
		t.Fatal("Get(a) ok = true after expiry, want false")
	}
}

func TestTTLCacheExpiredResolutionPreservesNewerEntry(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 12, 0, 0, 0, 0, time.UTC)
	cache := newTTLCache[string, int](2, time.Minute, func() time.Time { return now })
	cache.Set("a", 1)
	now = now.Add(2 * time.Minute)

	cache.mu.RLock()
	stale := cache.entries["a"]
	cache.mu.RUnlock()
	if !stale.expired(now) {
		t.Fatal("test setup: cached entry must be expired")
	}

	cache.mu.Lock()
	cache.entries["a"] = ttlEntry[int]{value: 2, expiresAt: now.Add(time.Minute)}
	cache.mu.Unlock()

	if got, ok := cache.resolveExpired("a"); !ok || got != 2 {
		t.Fatalf("resolveExpired(a) = (%d, %v), want (2, true)", got, ok)
	}
}
