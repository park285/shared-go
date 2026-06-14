package promptguard

import (
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
