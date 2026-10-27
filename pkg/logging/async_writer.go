package logging

import (
	"bytes"
	"context"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strconv"
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
	target    io.Writer
	queue     chan queuedLine
	done      chan struct{}
	stopped   chan struct{}
	mu        sync.Mutex
	closed    bool
	dropped   atomic.Uint64
	truncated atomic.Uint64
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
		target:  target,
		queue:   make(chan queuedLine, depth),
		done:    make(chan struct{}),
		stopped: make(chan struct{}),
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
			w.writeLossSummary()

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
	if n, err := w.target.Write(line.data); err != nil || n != len(line.data) {
		w.recordDrop()

		return
	}

	if line.truncated {
		w.truncated.Add(1)
		stdoutTruncated.Add(1)
	}
}

func (w *asyncDropWriter) Write(p []byte) (int, error) {
	// admission과 enqueue를 직렬화해 복사 전에 자리를 예약한다. Consumer는 이
	// lock을 잡지 않으며 target I/O도 lock 밖이다. Full/closed 경로는 할당하지 않는다.
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.closed || len(w.queue) == cap(w.queue) {
		w.recordDrop()

		return len(p), nil
	}

	w.queue <- w.boundedCopy(p)

	return len(p), nil
}

func (w *asyncDropWriter) recordDrop() {
	w.dropped.Add(1)
	stdoutDropped.Add(1)
}

// 정상 record는 JSON을 다시 해석하지 않는다. Oversized 사본만 이미 정제된 JSON에서
// correlation을 재구성하고 나머지 필드를 생략한다. 원문 prefix를 싣지 않아 정제 전
// 데이터를 복원하지 않으며 파일 lane에 전달할 p도 변경하지 않는다.
func (w *asyncDropWriter) boundedCopy(p []byte) queuedLine {
	if len(p) <= asyncStdoutMaxLineBytes {
		return queuedLine{data: bytes.Clone(p)}
	}

	data := []byte(`{"stdout_truncated":true,"stdout_original_bytes":`)

	data = strconv.AppendInt(data, int64(len(p)), 10)

	// 미지의 필드는 생략한다. 제외할 대형 진단 값을 맵에 복사하지 않기 위해서다.
	var fields struct {
		Time      jsontext.Value `json:"time"`
		Level     jsontext.Value `json:"level"`
		Message   jsontext.Value `json:"msg"`
		Event     jsontext.Value `json:"event"`
		TraceID   jsontext.Value `json:"trace_id"`
		SpanID    jsontext.Value `json:"span_id"`
		RequestID jsontext.Value `json:"request_id"`
	}

	if err := jsonv2.Unmarshal(p, &fields, jsontext.AllowDuplicateNames(true)); err != nil {
		data = append(data, ",\"stdout_invalid_json\":true}\n"...)

		return queuedLine{data: data, truncated: true}
	}

	// 각 string은 최대 1024 bytes, JSON escaping은 최대 6배다.
	// 7개 고정 key와 metadata를 포함해 항상 64 KiB보다 작다.
	for _, field := range [...]struct {
		key string
		raw jsontext.Value
	}{
		{"time", fields.Time},
		{"level", fields.Level},
		{"msg", fields.Message},
		{"event", fields.Event},
		{"trace_id", fields.TraceID},
		{"span_id", fields.SpanID},
		{"request_id", fields.RequestID},
	} {
		var value string

		if len(field.raw) == 0 || jsonv2.Unmarshal(field.raw, &value) != nil {
			continue
		}

		if len(value) > 1024 {
			end := 1024
			for !utf8.RuneStart(value[end]) {
				end--
			}

			value = value[:end]
		}

		data = append(data, ',', '"')
		data = append(data, field.key...)
		data = append(data, '"', ':')

		var quoteErr error

		data, quoteErr = jsontext.AppendQuote(data, value)
		if quoteErr != nil {
			// AppendQuote는 잘못된 UTF-8을 보고해도 유효한 JSON을 유지한다.
			data = append(data, `,"stdout_invalid_utf8":true`...)
		}
	}

	data = append(data, '}', '\n')

	return queuedLine{data: data, truncated: true}
}

func (w *asyncDropWriter) Close() error {
	w.mu.Lock()

	if !w.closed {
		w.closed = true
		close(w.done)
	}

	w.mu.Unlock()

	// 요약까지 sole writer인 run이 담당한다. 타임아웃은 대기만 끝내며,
	// 막힌 target.Write를 취소하거나 run의 종료를 보장하지 않는다.
	select {
	case <-w.stopped:
	case <-time.After(asyncDropWriterCloseTimeout):
	}

	return nil
}

// 요약도 JSON handler를 거쳐 stdout 파싱 계약을 보존한다. Run만 호출하므로
// 일반 라인과 직렬화되고, 요약 쓰기가 막혀도 Close의 대기 시간은 늘지 않는다.
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
	_ = newFormatHandler(record.Level, w.target, nil).Handle(context.Background(), record)
}

func (w *asyncDropWriter) droppedCount() uint64 {
	return w.dropped.Load()
}

func (w *asyncDropWriter) truncatedCount() uint64 {
	return w.truncated.Load()
}

type multiCloser []io.Closer

// Close는 앞 lane이 실패해도 모든 lane을 닫고, 실패한 lane의 오류를 모두 errors.Join으로 남긴다.
func (c multiCloser) Close() error {
	var closeErr error

	for _, closer := range c {
		if closer == nil {
			continue
		}

		closeErr = errors.Join(closeErr, closer.Close())
	}

	if closeErr != nil {
		return fmt.Errorf("logging: close log lanes: %w", closeErr)
	}

	return nil
}
