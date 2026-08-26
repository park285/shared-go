package lockutil

import (
	"sync"
	"testing"
	"time"
)

func TestKeyedMutexSerializesSameKey(t *testing.T) {
	var mu KeyedMutex

	mu.Lock("key")

	acquired := make(chan struct{})

	go func() {
		mu.Lock("key")
		close(acquired)
		mu.Unlock("key")
	}()

	select {
	case <-acquired:
		t.Fatal("Lock() acquired same key while held")
	case <-time.After(20 * time.Millisecond):
	}

	mu.Unlock("key")

	select {
	case <-acquired:
	case <-time.After(time.Second):
		t.Fatal("Lock() did not acquire after Unlock()")
	}
}

func TestKeyedMutexAllowsDifferentShards(t *testing.T) {
	var mu KeyedMutex

	mu.Lock("sid-1")
	defer mu.Unlock("sid-1")

	other := keyOnDifferentShard(t, "sid-1")
	acquired := make(chan struct{})

	go func() {
		mu.Lock(other)
		mu.Unlock(other)
		close(acquired)
	}()

	select {
	case <-acquired:
	case <-time.After(time.Second):
		t.Fatal("different shard blocked")
	}
}

func TestKeyedMutexSerializesDifferentKeysOnSameShard(t *testing.T) {
	var mu KeyedMutex

	const held = "sid-1"

	other := keyOnSameShard(t, held)

	mu.Lock(held)

	acquired := make(chan struct{})

	go func() {
		mu.Lock(other)
		close(acquired)
		mu.Unlock(other)
	}()

	select {
	case <-acquired:
		t.Fatalf("Lock(%q) acquired while same-shard key %q was held", other, held)
	case <-time.After(20 * time.Millisecond):
	}

	mu.Unlock(held)

	select {
	case <-acquired:
	case <-time.After(time.Second):
		t.Fatalf("Lock(%q) did not acquire after Unlock(%q)", other, held)
	}
}

func TestKeyedMutexRaceSafety(t *testing.T) {
	var (
		mu       KeyedMutex
		counters [4]int
		wg       sync.WaitGroup
	)

	keys := [4]string{"sid-1", "sid-2", "sid-3", "sid-4"}

	const (
		goroutines = 32
		iterations = 1000
	)

	for worker := range goroutines {
		wg.Go(func() {
			for index := range iterations {
				position := (worker + index) % len(keys)
				mu.Lock(keys[position])

				counters[position]++
				mu.Unlock(keys[position])
			}
		})
	}

	wg.Wait()

	got := 0

	for _, count := range counters {
		got += count
	}

	if want := goroutines * iterations; got != want {
		t.Fatalf("guarded increments = %d, want %d", got, want)
	}
}

func keyOnSameShard(t *testing.T, key string) string {
	t.Helper()

	shard := keyedMutexShard(key)

	for index := range 4096 {
		candidate := key + string(rune(index+1))
		if keyedMutexShard(candidate) == shard {
			return candidate
		}
	}

	t.Fatal("could not find same shard")

	return ""
}

func keyOnDifferentShard(t *testing.T, key string) string {
	t.Helper()

	shard := keyedMutexShard(key)

	for index := range 1024 {
		candidate := key + string(rune(index+1))
		if keyedMutexShard(candidate) != shard {
			return candidate
		}
	}

	t.Fatal("could not find different shard")

	return ""
}
