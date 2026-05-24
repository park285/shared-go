package lifecycle

import (
	"sync/atomic"
	"testing"
)

func TestManaged_Close_CallsCleanup(t *testing.T) {
	t.Parallel()

	var called atomic.Bool
	m := NewManaged(func() { called.Store(true) })
	m.Close()

	if !called.Load() {
		t.Fatal("cleanup function was not called")
	}
}

func TestManaged_Close_NilReceiver(t *testing.T) {
	t.Parallel()

	var m *Managed
	m.Close()
}
