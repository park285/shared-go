package logging

import (
	"bytes"
	jsonv2 "encoding/json/v2"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/park285/shared-go/v2/pkg/internal/testsupport"
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

	out, err := b.buf.Write(p)
	if err != nil {
		return out, fmt.Errorf("write: %w", err)
	}

	return out, nil
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

	testsupport.WriteResponse(t, w, "line-0\n")
	<-target.started
	testsupport.WriteResponse(t, w, "line-1\n")
	testsupport.WriteResponse(t, w, "line-2\n")

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

		if err := jsonv2.Unmarshal([]byte(line), &record); err != nil {
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
		Level:      testInfo,
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

	if closer == nil {
		t.Fatal("file logging did not return a closer")
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

		if err := jsonv2.Unmarshal([]byte(line), &record); err != nil {
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

// target은 이 파일의 syncBuffer가 아니라 동기화 없는 buffer여야 한다. SyncBuffer로 바꾸면
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

	for range closers {
		wg.Go(func() {
			<-start

			if err := w.Close(); err != nil {
				t.Errorf("Close() error = %v", err)
			}
		})
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
		testsupport.WriteBytes(t, w, fmt.Appendf(nil, "line-%d\n", i))
	}

	if err := w.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	if strings.Contains(target.String(), "dropped") {
		t.Fatalf("did not expect drop summary, got: %q", target.String())
	}
}

type summaryBlockingWriter struct {
	buf       bytes.Buffer
	started   chan struct{}
	release   chan struct{}
	summaries atomic.Int32
}

func (w *summaryBlockingWriter) Write(p []byte) (int, error) {
	if bytes.Contains(p, []byte("async stdout writer lost lines")) {
		if w.summaries.Add(1) == 1 {
			close(w.started)
		}

		<-w.release
	}

	n, err := w.buf.Write(p)
	if err != nil {
		return n, fmt.Errorf("write summary test buffer: %w", err)
	}

	return n, nil
}

func TestAsyncDropWriter_BlockedSummaryKeepsConcurrentCloseBounded(t *testing.T) {
	t.Parallel()

	target := &summaryBlockingWriter{started: make(chan struct{}), release: make(chan struct{})}
	releaseTarget := sync.OnceFunc(func() { close(target.release) })
	t.Cleanup(releaseTarget)

	w := newAsyncDropWriter(target, 8)

	w.maxLineBytes = 16
	testsupport.WriteBytes(t, w, bytes.Repeat([]byte("A"), 1024))

	var closers sync.WaitGroup

	for range 4 {
		closers.Go(func() {
			if err := w.Close(); err != nil {
				t.Errorf("Close() error = %v", err)
			}
		})
	}

	closed := make(chan struct{})

	go func() {
		closers.Wait()
		close(closed)
	}()

	waitForSummarySignal(t, target.started, asyncDropWriterCloseTimeout+time.Second, "loss summary did not reach target")
	waitForSummarySignal(t, closed, asyncDropWriterCloseTimeout+time.Second, "Close blocked on the loss summary")

	// A bounded wait is not cancellation or a completed join. The sole writer
	// still owns the blocked summary until the target permits it to finish.
	select {
	case <-w.stopped:
		t.Fatal("writer reported stopped while its summary was blocked")
	default:
	}

	if got := target.summaries.Load(); got != 1 {
		t.Fatalf("summary writes = %d, want 1", got)
	}

	releaseTarget()

	waitForSummarySignal(t, w.stopped, time.Second, "writer did not finish after target release")

	if err := w.Close(); err != nil {
		t.Fatalf("Close() after join error = %v", err)
	}

	if got := target.summaries.Load(); got != 1 {
		t.Fatalf("summary writes after repeated Close = %d, want 1", got)
	}

	lines := strings.Split(strings.TrimSpace(target.buf.String()), "\n")
	if len(lines) != 2 || !strings.Contains(lines[1], "async stdout writer lost lines") {
		t.Fatalf("summary must follow the drained line, got %q", target.buf.String())
	}
}

func waitForSummarySignal(t *testing.T, signal <-chan struct{}, timeout time.Duration, message string) {
	t.Helper()

	select {
	case <-signal:
	case <-time.After(timeout):
		t.Fatal(message)
	}
}
