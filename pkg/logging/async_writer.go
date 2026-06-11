package logging

import (
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"
)

const (
	asyncStdoutQueueDepth       = 256
	asyncDropWriterCloseTimeout = 2 * time.Second
)

// stdout이 느린 fd에 묶여도 로깅 전체가 블로킹되지 않도록 큐가 가득 차면 해당 라인을 버린다.
// 파일(lumberjack) lane은 동기 기록을 유지하므로 유실은 stdout 사본에 한정된다.
type asyncDropWriter struct {
	target  io.Writer
	queue   chan []byte
	done    chan struct{}
	stopped chan struct{}
	stop    sync.Once
	dropped atomic.Uint64
}

func newAsyncDropWriter(target io.Writer, depth int) *asyncDropWriter {
	if depth <= 0 {
		depth = asyncStdoutQueueDepth
	}

	w := &asyncDropWriter{
		target:  target,
		queue:   make(chan []byte, depth),
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

func (w *asyncDropWriter) forward(line []byte) {
	if _, err := w.target.Write(line); err != nil {
		w.dropped.Add(1)
	}
}

func (w *asyncDropWriter) Write(p []byte) (int, error) {
	line := make([]byte, len(p))
	copy(line, p)

	select {
	case w.queue <- line:
	default:
		w.dropped.Add(1)
	}

	return len(p), nil
}

func (w *asyncDropWriter) Close() error {
	w.stop.Do(func() { close(w.done) })

	// stopped 분기에서만 run goroutine이 종료를 마쳐 target에 동시 기록이 없다.
	// 타임아웃 분기에서는 run이 아직 forward 중일 수 있어 요약 기록을 생략한다.
	select {
	case <-w.stopped:
		if dropped := w.dropped.Load(); dropped > 0 {
			fmt.Fprintf(w.target, "[logging] async stdout writer dropped %d lines\n", dropped)
		}
	case <-time.After(asyncDropWriterCloseTimeout):
	}

	return nil
}

func (w *asyncDropWriter) droppedCount() uint64 {
	return w.dropped.Load()
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
