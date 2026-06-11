package lifecycle

import (
	"sync"
	"sync/atomic"
	"testing"
)

func TestCleanupCloser_Close_CallsCleanup(t *testing.T) {
	t.Parallel()

	var called atomic.Bool
	c := NewCleanupCloser(func() { called.Store(true) })
	c.Close()

	if !called.Load() {
		t.Fatal("cleanup function was not called")
	}
}

func TestCleanupCloser_Close_NilCleanup(t *testing.T) {
	t.Parallel()

	c := NewCleanupCloser(nil)
	c.Close()
}

func TestCleanupCloser_Close_NilReceiver(t *testing.T) {
	t.Parallel()

	var c *CleanupCloser
	c.Close()
}

func TestCleanupCloser_Close_Idempotent(t *testing.T) {
	t.Parallel()

	var count atomic.Int32
	c := NewCleanupCloser(func() { count.Add(1) })
	c.Close()
	c.Close()
	c.Close()

	if got := count.Load(); got != 1 {
		t.Fatalf("cleanup ran %d times across repeated Close(), want 1", got)
	}
}

func TestCleanupCloser_Close_ConcurrentRunsOnce(t *testing.T) {
	t.Parallel()

	var count atomic.Int32
	c := NewCleanupCloser(func() { count.Add(1) })

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.Close()
		}()
	}
	wg.Wait()

	if got := count.Load(); got != 1 {
		t.Fatalf("cleanup ran %d times under concurrent Close(), want 1", got)
	}
}
