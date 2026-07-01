package workerpool

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// TestQueuedPool_PanicRecover — Behavior 2 (RED 예상)
// task가 panic을 일으켜도 worker goroutine이 살아남고, 이후 task가 실행돼야 한다.
// recover된 내용은 slog.Error로 기록돼야 한다.
func TestQueuedPool_PanicRecover(t *testing.T) {
	var logBuf bytes.Buffer
	logHandler := slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelError})
	logger := slog.New(logHandler)

	p := newQueuedWithLogger(QueuedConfig{Workers: 1, QueueSize: 4}, logger)
	defer p.StopAndWait()

	// 1) panic task 제출
	if !p.SubmitWait(func() {
		panic("intentional test panic")
	}) {
		t.Fatal("SubmitWait(panic task) = false, want true")
	}

	// 2) worker가 recover 후 계속 실행 중인지 확인하기 위해 후속 task 제출
	var ran int32
	done := make(chan struct{})
	if !p.SubmitWait(func() {
		atomic.StoreInt32(&ran, 1)
		close(done)
	}) {
		t.Fatal("SubmitWait(subsequent task) = false, want true")
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("subsequent task did not execute after panic — worker goroutine likely dead")
	}

	if atomic.LoadInt32(&ran) != 1 {
		t.Error("subsequent task was not executed")
	}

	// 3) panic 정보가 error 로그에 기록됐는지 확인
	p.StopAndWait()
	logOut := logBuf.String()
	if !strings.Contains(logOut, "intentional test panic") {
		t.Errorf("panic value must appear in error log; got: %s", logOut)
	}
}

// TestQueuedPool_PanicRecover_NoLogger — logger 없는 기본 생성자로도 panic이 silently crash하지 않음을 검증.
func TestQueuedPool_PanicRecover_NoLogger(t *testing.T) {
	p := NewQueued(QueuedConfig{Workers: 1, QueueSize: 2})
	defer p.StopAndWait()

	if !p.SubmitWait(func() { panic("no-logger panic") }) {
		t.Fatal("SubmitWait = false")
	}

	var ran int32
	done := make(chan struct{})
	if !p.SubmitWait(func() {
		atomic.StoreInt32(&ran, 1)
		close(done)
	}) {
		t.Fatal("SubmitWait(subsequent) = false")
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("process crashed or worker dead after panic")
	}
}

func newQueuedWithLogger(config QueuedConfig, logger *slog.Logger) *QueuedPool {
	return NewQueuedWithLogger(config, logger)
}

// context 패키지가 import에 포함돼 있음을 컴파일러에 알린다.
var _ context.Context = nil
