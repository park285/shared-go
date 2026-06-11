package logging

import (
	"bytes"
	"fmt"
	"strings"
	"sync"
	"testing"
)

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// I5: Close가 drop>0이면 target에 요약 1줄을 남겨야 한다.
func TestAsyncDropWriter_CloseWritesDropSummary(t *testing.T) {
	t.Parallel()

	var target syncBuffer
	w := newAsyncDropWriter(&target, 1)

	// 큐 capacity(1)를 초과하도록 빠르게 밀어넣어 drop을 유발한다.
	dropped := false
	for i := range 10000 {
		w.Write([]byte(fmt.Sprintf("line-%d\n", i)))
		if w.droppedCount() > 0 {
			dropped = true
			break
		}
	}
	if !dropped {
		t.Skip("could not induce a drop deterministically on this run")
	}

	if err := w.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	out := target.String()
	if !strings.Contains(out, "dropped") {
		t.Fatalf("expected drop summary line in target output, got: %q", out)
	}
	if !strings.Contains(out, "async stdout writer") {
		t.Fatalf("expected async writer label in summary, got: %q", out)
	}
}

// I5: drop이 없으면 요약 라인을 남기지 않아야 한다.
func TestAsyncDropWriter_CloseNoSummaryWhenNoDrops(t *testing.T) {
	t.Parallel()

	var target syncBuffer
	w := newAsyncDropWriter(&target, 64)

	for i := range 10 {
		w.Write([]byte(fmt.Sprintf("line-%d\n", i)))
	}

	if err := w.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	if strings.Contains(target.String(), "dropped") {
		t.Fatalf("did not expect drop summary, got: %q", target.String())
	}
}
