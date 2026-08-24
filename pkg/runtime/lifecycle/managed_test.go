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

func TestManaged_Close_Idempotent(t *testing.T) {
	t.Parallel()

	var count atomic.Int32

	m := NewManaged(func() { count.Add(1) })
	m.Close()
	m.Close()

	if got := count.Load(); got != 1 {
		t.Fatalf("cleanup ran %d times across repeated Close(), want 1", got)
	}
}
