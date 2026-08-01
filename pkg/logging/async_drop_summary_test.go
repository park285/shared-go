package logging

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
)

type syncBuffer struct {
	mu      sync.Mutex
	buf     bytes.Buffer
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	if b.started != nil {
		b.once.Do(func() { close(b.started) })
		<-b.release
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// Close가 drop>0이면 target에 요약 1줄을 남겨야 한다.
func TestAsyncDropWriter_CloseWritesDropSummary(t *testing.T) {
	t.Parallel()

	target := &syncBuffer{started: make(chan struct{}), release: make(chan struct{})}
	releaseTarget := sync.OnceFunc(func() { close(target.release) })
	t.Cleanup(releaseTarget)
	w := newAsyncDropWriter(target, 1)

	w.Write([]byte("line-0\n"))
	<-target.started
	w.Write([]byte("line-1\n"))
	w.Write([]byte("line-2\n"))
	if w.droppedCount() == 0 {
		t.Fatal("droppedCount() = 0, want > 0 when queue overflows")
	}
	releaseTarget()

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

func TestAsyncDropWriter_CloseSummaryKeepsJSONFormat(t *testing.T) {
	t.Parallel()

	target := &syncBuffer{started: make(chan struct{}), release: make(chan struct{})}
	releaseTarget := sync.OnceFunc(func() { close(target.release) })
	t.Cleanup(releaseTarget)
	w := newAsyncDropWriter(target, 1)

	logger := slog.New(newFormatHandler(slog.LevelInfo, w))
	logger.Info("first")
	<-target.started
	for range 8 {
		logger.Info("overflow")
	}
	if w.droppedCount() == 0 {
		t.Fatal("droppedCount() = 0, want > 0 when queue overflows")
	}
	releaseTarget()

	if err := w.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	var summary map[string]any
	for line := range strings.SplitSeq(strings.TrimSpace(target.String()), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("non-JSON line in json stdout stream: %q (%v)", line, err)
		}
		if msg, ok := record[slog.MessageKey].(string); ok && strings.Contains(msg, "lost lines") {
			summary = record
		}
	}

	if summary == nil {
		t.Fatalf("drop summary record missing: %q", target.String())
	}
	if got := summary["dropped"]; got == nil || got == float64(0) {
		t.Fatalf("summary dropped = %v, want > 0", got)
	}
	if got, ok := summary[slog.SourceKey]; ok {
		t.Fatalf("summary carries a synthetic source attr: %v", got)
	}
}

func TestEnableFileLogging_AsyncSummaryKeepsJSONFormat(t *testing.T) {
	t.Parallel()

	stdout := &syncBuffer{started: make(chan struct{}), release: make(chan struct{})}
	releaseStdout := sync.OnceFunc(func() { close(stdout.release) })
	t.Cleanup(releaseStdout)

	config := Config{
		Level:      "info",
		Format:     FormatJSON,
		Dir:        t.TempDir(),
		MaxSizeMB:  5,
		MaxBackups: 5,
		MaxAgeDays: 30,
	}

	logger, closer, err := enableFileLoggingWithStdout(stdout, config, "async.log", Options{AsyncStdout: true})
	if err != nil {
		t.Fatalf("enableFileLoggingWithStdout() error = %v", err)
	}

	<-stdout.started
	for seq := range asyncStdoutQueueDepth * 2 {
		logger.Info("async_format_probe", "seq", seq)
	}

	releaseStdout()
	if err := closer.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	var summary map[string]any
	for line := range strings.SplitSeq(strings.TrimSpace(stdout.String()), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("non-JSON line in json stdout stream: %q (%v)", line, err)
		}
		if msg, ok := record[slog.MessageKey].(string); ok && strings.Contains(msg, "lost lines") {
			summary = record
		}
	}

	if summary == nil {
		t.Fatalf("drop summary missing — the stalled queue never overflowed: %q", stdout.String())
	}
	if got := summary["dropped"]; got == nil || got == float64(0) {
		t.Fatalf("summary dropped = %v, want > 0", got)
	}
}

func TestAsyncDropWriter_CloseIsIdempotent(t *testing.T) {
	t.Parallel()

	var target syncBuffer
	w := newAsyncDropWriter(&target, 8)
	w.maxLineBytes = 16

	if _, err := w.Write(bytes.Repeat([]byte("A"), 1024)); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	for range 3 {
		if err := w.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	}

	if got := strings.Count(target.String(), "lost lines"); got != 1 {
		t.Fatalf("loss summary written %d times, want exactly 1: %q", got, target.String())
	}
}

// target은 이 파일의 syncBuffer가 아니라 동기화 없는 buffer여야 한다. syncBuffer로 바꾸면
// 요약 경로의 동시 진입이 mutex에 가려 -race가 아무것도 잡지 못한다.
func TestAsyncDropWriter_ConcurrentCloseDoesNotRaceOnTarget(t *testing.T) {
	t.Parallel()

	var target bytes.Buffer
	w := newAsyncDropWriter(&target, 8)
	w.maxLineBytes = 16

	if _, err := w.Write(bytes.Repeat([]byte("A"), 1024)); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	const closers = 4
	start := make(chan struct{})
	var wg sync.WaitGroup

	wg.Add(closers)
	for range closers {
		go func() {
			defer wg.Done()
			<-start
			if err := w.Close(); err != nil {
				t.Errorf("Close() error = %v", err)
			}
		}()
	}
	close(start)
	wg.Wait()

	if got := strings.Count(target.String(), "lost lines"); got != 1 {
		t.Fatalf("loss summary written %d times, want exactly 1: %q", got, target.String())
	}
}

// drop이 없으면 요약 라인을 남기지 않아야 한다.
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
