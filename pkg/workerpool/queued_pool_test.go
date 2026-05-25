package workerpool

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestQueuedPool_TrySubmit_Success(t *testing.T) {
	p := NewQueued(QueuedConfig{Workers: 1, QueueSize: 1})
	defer p.StopAndWait()

	done := make(chan struct{})
	if !p.TrySubmit(func() {
		close(done)
	}) {
		t.Fatal("TrySubmit() = false, want true")
	}

	waitForClosed(t, done, "submitted task")
}

func TestQueuedPool_TrySubmit_Full(t *testing.T) {
	p := NewQueued(QueuedConfig{Workers: 1, QueueSize: 1})
	defer p.StopAndWait()

	release := make(chan struct{})
	started := make(chan struct{})
	if !p.TrySubmit(func() {
		close(started)
		<-release
	}) {
		t.Fatal("TrySubmit() for running task = false, want true")
	}
	waitForClosed(t, started, "running task start")

	if !p.TrySubmit(func() {}) {
		t.Fatal("TrySubmit() for queued task = false, want true")
	}
	if p.TrySubmit(func() {}) {
		t.Fatal("TrySubmit() with full queue = true, want false")
	}

	close(release)
}

func TestQueuedPool_TrySubmit_AfterStop(t *testing.T) {
	p := NewQueued(QueuedConfig{Workers: 1, QueueSize: 1})
	p.StopAndWait()

	if p.TrySubmit(func() {}) {
		t.Fatal("TrySubmit() after StopAndWait() = true, want false")
	}
}

func TestQueuedPool_SubmitWait_Blocking(t *testing.T) {
	p := NewQueued(QueuedConfig{Workers: 1, QueueSize: 1})
	defer p.StopAndWait()

	release := make(chan struct{})
	started := make(chan struct{})
	if !p.TrySubmit(func() {
		close(started)
		<-release
	}) {
		t.Fatal("TrySubmit() for running task = false, want true")
	}
	waitForClosed(t, started, "running task start")

	if !p.TrySubmit(func() {}) {
		t.Fatal("TrySubmit() for queued task = false, want true")
	}

	taskDone := make(chan struct{})
	submitted := make(chan bool, 1)
	go func() {
		submitted <- p.SubmitWait(func() {
			close(taskDone)
		})
	}()

	expectNoBool(t, submitted, "SubmitWait")
	close(release)
	waitBool(t, submitted, true, "SubmitWait")
	waitForClosed(t, taskDone, "SubmitWait task")
}

func TestQueuedPool_SubmitWait_StopUnblocks(t *testing.T) {
	p := NewQueued(QueuedConfig{Workers: 1, QueueSize: 1})

	release := make(chan struct{})
	started := make(chan struct{})
	if !p.TrySubmit(func() {
		close(started)
		<-release
	}) {
		t.Fatal("TrySubmit() for running task = false, want true")
	}
	waitForClosed(t, started, "running task start")

	if !p.TrySubmit(func() {}) {
		t.Fatal("TrySubmit() for queued task = false, want true")
	}

	submitted := make(chan bool, 1)
	go func() {
		submitted <- p.SubmitWait(func() {})
	}()

	expectNoBool(t, submitted, "SubmitWait")

	stopped := make(chan struct{})
	go func() {
		p.StopAndWait()
		close(stopped)
	}()

	waitBool(t, submitted, false, "SubmitWait")
	close(release)
	waitForClosed(t, stopped, "StopAndWait")
}

func TestQueuedPool_StopAndWait_Drains(t *testing.T) {
	p := NewQueued(QueuedConfig{Workers: 2, QueueSize: 10})

	var completed int32
	for range 10 {
		if !p.SubmitWait(func() {
			atomic.AddInt32(&completed, 1)
		}) {
			t.Fatal("SubmitWait() = false, want true")
		}
	}

	p.StopAndWait()
	if completed != 10 {
		t.Fatalf("completed = %d, want 10", completed)
	}
}

func TestQueuedPool_StopAndWait_Idempotent(t *testing.T) {
	p := NewQueued(QueuedConfig{Workers: 1, QueueSize: 1})

	p.StopAndWait()
	p.StopAndWait()
}

func TestQueuedPool_StopAndWaitContext_Success(t *testing.T) {
	p := NewQueued(QueuedConfig{Workers: 2, QueueSize: 5})

	var completed int32
	for range 5 {
		if !p.SubmitWait(func() {
			atomic.AddInt32(&completed, 1)
		}) {
			t.Fatal("SubmitWait() = false, want true")
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if err := p.StopAndWaitContext(ctx); err != nil {
		t.Fatalf("StopAndWaitContext() error = %v, want nil", err)
	}
	if completed != 5 {
		t.Fatalf("completed = %d, want 5", completed)
	}
}

func TestQueuedPool_StopAndWaitContext_Timeout(t *testing.T) {
	p := NewQueued(QueuedConfig{Workers: 1, QueueSize: 1})

	release := make(chan struct{})
	started := make(chan struct{})
	if !p.TrySubmit(func() {
		close(started)
		<-release
	}) {
		t.Fatal("TrySubmit() = false, want true")
	}
	waitForClosed(t, started, "running task start")

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()

	if err := p.StopAndWaitContext(ctx); err != context.DeadlineExceeded {
		t.Fatalf("StopAndWaitContext() error = %v, want %v", err, context.DeadlineExceeded)
	}

	close(release)
	p.StopAndWait()
}

func TestQueuedPool_NilReceiver(t *testing.T) {
	var p *QueuedPool

	if p.TrySubmit(func() {}) {
		t.Fatal("TrySubmit() on nil receiver = true, want false")
	}
	if p.SubmitWait(func() {}) {
		t.Fatal("SubmitWait() on nil receiver = true, want false")
	}
	p.StopAndWait()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := p.StopAndWaitContext(ctx); err != nil {
		t.Fatalf("StopAndWaitContext() on nil receiver error = %v, want nil", err)
	}
	if p.Workers() != 0 {
		t.Fatalf("Workers() on nil receiver = %d, want 0", p.Workers())
	}
	if p.QueueSize() != 0 {
		t.Fatalf("QueueSize() on nil receiver = %d, want 0", p.QueueSize())
	}
	if p.Pending() != 0 {
		t.Fatalf("Pending() on nil receiver = %d, want 0", p.Pending())
	}
}

func TestQueuedPool_NilTask(t *testing.T) {
	p := NewQueued(QueuedConfig{Workers: 1, QueueSize: 2})
	defer p.StopAndWait()

	if p.TrySubmit(nil) {
		t.Fatal("TrySubmit(nil) = true, want false")
	}
	if p.SubmitWait(nil) {
		t.Fatal("SubmitWait(nil) = true, want false")
	}

	done := make(chan struct{})
	if !p.SubmitWait(func() {
		close(done)
	}) {
		t.Fatal("SubmitWait() = false, want true")
	}
	waitForClosed(t, done, "task after nil rejection")
}

func TestQueuedPool_Workers(t *testing.T) {
	p := NewQueued(QueuedConfig{Workers: 3, QueueSize: 1})
	defer p.StopAndWait()

	if p.Workers() != 3 {
		t.Fatalf("Workers() = %d, want 3", p.Workers())
	}

	defaulted := NewQueued(QueuedConfig{Workers: 0, QueueSize: 1})
	defer defaulted.StopAndWait()
	if defaulted.Workers() != 1 {
		t.Fatalf("Workers() with zero config = %d, want 1", defaulted.Workers())
	}
}

func TestQueuedPool_QueueSize(t *testing.T) {
	p := NewQueued(QueuedConfig{Workers: 1, QueueSize: 4})
	defer p.StopAndWait()

	if p.QueueSize() != 4 {
		t.Fatalf("QueueSize() = %d, want 4", p.QueueSize())
	}

	defaulted := NewQueued(QueuedConfig{Workers: 1, QueueSize: 0})
	defer defaulted.StopAndWait()
	if defaulted.QueueSize() != 1 {
		t.Fatalf("QueueSize() with zero config = %d, want 1", defaulted.QueueSize())
	}
}

func TestQueuedPool_Pending(t *testing.T) {
	p := NewQueued(QueuedConfig{Workers: 1, QueueSize: 2})
	defer p.StopAndWait()

	release := make(chan struct{})
	started := make(chan struct{})
	if !p.TrySubmit(func() {
		close(started)
		<-release
	}) {
		t.Fatal("TrySubmit() for running task = false, want true")
	}
	waitForClosed(t, started, "running task start")

	if got := p.Pending(); got != 0 {
		t.Fatalf("Pending() with empty queue = %d, want 0", got)
	}
	if !p.TrySubmit(func() {}) {
		t.Fatal("TrySubmit() first queued task = false, want true")
	}
	if !p.TrySubmit(func() {}) {
		t.Fatal("TrySubmit() second queued task = false, want true")
	}
	if got := p.Pending(); got != 2 {
		t.Fatalf("Pending() = %d, want 2", got)
	}

	close(release)
}

func TestQueuedPool_ConcurrentSubmit(t *testing.T) {
	p := NewQueued(QueuedConfig{Workers: 8, QueueSize: 16})
	defer p.StopAndWait()

	const tasks = 200
	var submitters sync.WaitGroup
	var completed int32
	var rejected int32

	submitters.Add(tasks)
	for range tasks {
		go func() {
			defer submitters.Done()
			if !p.SubmitWait(func() {
				atomic.AddInt32(&completed, 1)
			}) {
				atomic.AddInt32(&rejected, 1)
			}
		}()
	}

	submitters.Wait()
	p.StopAndWait()

	if rejected != 0 {
		t.Fatalf("rejected submissions = %d, want 0", rejected)
	}
	if completed != tasks {
		t.Fatalf("completed = %d, want %d", completed, tasks)
	}
}

func waitForClosed(t *testing.T, ch <-chan struct{}, name string) {
	t.Helper()

	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for %s", name)
	}
}

func waitBool(t *testing.T, ch <-chan bool, want bool, name string) {
	t.Helper()

	select {
	case got := <-ch:
		if got != want {
			t.Fatalf("%s = %v, want %v", name, got, want)
		}
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for %s", name)
	}
}

func expectNoBool(t *testing.T, ch <-chan bool, name string) {
	t.Helper()

	select {
	case got := <-ch:
		t.Fatalf("%s returned early with %v", name, got)
	case <-time.After(50 * time.Millisecond):
	}
}
