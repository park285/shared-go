package lifecycle

import (
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
