package logging

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"
)

const (
	asyncStdoutQueueDepth       = 256
	asyncStdoutMaxLineBytes     = 64 << 10
	asyncDropWriterCloseTimeout = 2 * time.Second
)

// stdout이 느린 fd에 묶여도 로깅 전체가 블로킹되지 않도록 큐가 가득 차면 해당 라인을 버린다.
// 파일(lumberjack) lane은 동기 기록을 유지하므로 유실은 stdout 사본에 한정된다.
type asyncDropWriter struct {
	target       io.Writer
	queue        chan queuedLine
	done         chan struct{}
	stopped      chan struct{}
	stop         sync.Once
	summarize    sync.Once
	maxLineBytes int
	dropped      atomic.Uint64
	truncated    atomic.Uint64
}

type queuedLine struct {
	data      []byte
	truncated bool
}

func newAsyncDropWriter(target io.Writer, depth int) *asyncDropWriter {
	if depth <= 0 {
		depth = asyncStdoutQueueDepth
	}

	w := &asyncDropWriter{
		target:       target,
		queue:        make(chan queuedLine, depth),
		done:         make(chan struct{}),
		stopped:      make(chan struct{}),
		maxLineBytes: asyncStdoutMaxLineBytes,
	}

	go w.run()

	return w
}

func (w *asyncDropWriter) run() {
	defer close(w.stopped)

	for {
		select {
		case line := <-w.queue:
			w.forward(line)
		case <-w.done:
			w.drain()

			return
		}
	}
}

func (w *asyncDropWriter) drain() {
	for {
		select {
		case line := <-w.queue:
			w.forward(line)
		default:
			return
		}
	}
}

func (w *asyncDropWriter) forward(line queuedLine) {
	if _, err := w.target.Write(line.data); err != nil {
		w.dropped.Add(1)

		return
	}

	if line.truncated {
		w.truncated.Add(1)
	}
}

func (w *asyncDropWriter) Write(p []byte) (int, error) {
	line := w.boundedCopy(p)

	select {
	case w.queue <- line:
	default:
		w.dropped.Add(1)
	}

	return len(p), nil
}

// 절단은 record 구분자를 남겨야 한다. 개행까지 잘라내면 잘린 조각이 다음 record와 한 줄로
// 이어붙어, JSON lane에서는 절단된 record와 그 다음 record 2건이 함께 파싱 불가가 된다.
func (w *asyncDropWriter) boundedCopy(p []byte) queuedLine {
	if w.maxLineBytes <= 0 || len(p) <= w.maxLineBytes {
		data := make([]byte, len(p))
		copy(data, p)

		return queuedLine{data: data}
	}

	body := trimPartialRune(p[:w.maxLineBytes-1])
	data := make([]byte, len(body)+1)
	copy(data, body)

	data[len(body)] = '\n'

	return queuedLine{data: data, truncated: true}
}

// 절단 지점이 multi-byte rune 한가운데면 남은 바이트가 invalid UTF-8이 되어, 여러 수집기가
// 이를 JSON parse 오류보다 험하게 다룬다. 마지막 rune 시작 바이트까지만 되짚어(UTF-8은 최대
// 4바이트) 잘린 sequence를 떼어낸다.
func trimPartialRune(line []byte) []byte {
	for i := len(line) - 1; i >= 0 && i > len(line)-utf8.UTFMax; i-- {
		if !utf8.RuneStart(line[i]) {
			continue
		}

		if r, size := utf8.DecodeRune(line[i:]); r == utf8.RuneError && size == 1 {
			return line[:i]
		}

		break
	}

	return line
}

func (w *asyncDropWriter) Close() error {
	w.stop.Do(func() { close(w.done) })

	// stopped 분기에서만 run goroutine이 종료를 마쳐 target에 동시 기록이 없다.
	// 타임아웃 분기에서는 run이 아직 forward 중일 수 있어 요약 기록을 생략한다.
	// stopped는 닫힌 채 유지되므로 Once 없이는 Close마다 요약이 반복되고, 동시 Close에서는
	// 두 goroutine이 같은 target에 함께 쓴다.
	select {
	case <-w.stopped:
		w.summarize.Do(w.writeLossSummary)
	case <-time.After(asyncDropWriterCloseTimeout):
	}

	return nil
}

// 요약도 JSON handler를 거쳐야 stdout 스트림의 파싱 계약을 보존한다. Run goroutine이 이미
// 종료했으므로 queue를 거치지 않고 target에 직접 쓰는 일회용 handler를 만든다.
func (w *asyncDropWriter) writeLossSummary() {
	dropped, truncated := w.dropped.Load(), w.truncated.Load()
	if dropped == 0 && truncated == 0 {
		return
	}

	record := slog.NewRecord(time.Now(), slog.LevelWarn, "async stdout writer lost lines", 0)
	record.AddAttrs(
		slog.Uint64("dropped", dropped),
		slog.Uint64("truncated", truncated),
	)

	// Handle은 handler level을 재검사하지 않으므로 이 인자는 요약 방출 여부를 정하지 않는다.
	//nolint:errcheck // best-effort 종료 진단, 기록 실패는 무시한다.
	_ = newFormatHandler(record.Level, w.target).Handle(context.Background(), record)
}

func (w *asyncDropWriter) droppedCount() uint64 {
	return w.dropped.Load()
}

func (w *asyncDropWriter) truncatedCount() uint64 {
	return w.truncated.Load()
}

type multiCloser []io.Closer

func (c multiCloser) Close() error {
	var firstErr error

	for _, closer := range c {
		if closer == nil {
			continue
		}

		if err := closer.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}

	return firstErr
}
