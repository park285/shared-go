package telemetry

import (
	"context"
	"testing"
	"time"
)

func TestFlushContext_DetachesCancellation(t *testing.T) {
	parent, cancel := context.WithCancel(t.Context())
	cancel()

	ctx, cancelFlush := flushContext(parent)
	defer cancelFlush()

	if err := ctx.Err(); err != nil {
		t.Fatalf("flush context must survive a canceled parent, got err=%v", err)
	}
}

func TestFlushContext_HasBoundedDeadline(t *testing.T) {
	ctx, cancelFlush := flushContext(t.Context())
	defer cancelFlush()

	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("flush context must carry a deadline to bound the flush window")
	}

	remaining := time.Until(deadline)
	if remaining <= 0 || remaining > 5*time.Second+time.Second {
		t.Fatalf("expected deadline within ~5s, got remaining=%v", remaining)
	}
}

func TestFlushContext_PreservesParentValues(t *testing.T) {
	type ctxKey string

	const k ctxKey = "trace-key"

	parent := context.WithValue(t.Context(), k, "v")

	ctx, cancelFlush := flushContext(parent)
	defer cancelFlush()

	if got := ctx.Value(k); got != "v" {
		t.Fatalf("flush context must preserve parent values, got %v", got)
	}
}
