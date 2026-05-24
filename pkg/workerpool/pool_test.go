package workerpool

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestPool_Submit(t *testing.T) {
	p, err := New(Config{Size: 5, ExpiryDuration: time.Second})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer p.Shutdown()

	var counter int32
	for range 20 {
		err := p.Submit(func() {
			atomic.AddInt32(&counter, 1)
			time.Sleep(10 * time.Millisecond)
		})
		if err != nil {
			t.Fatalf("Submit() error = %v", err)
		}
	}

	p.Wait()
	if counter != 20 {
		t.Errorf("counter = %d, want 20", counter)
	}
}

func TestPool_Concurrency(t *testing.T) {
	p, err := New(Config{Size: 5, ExpiryDuration: time.Second})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer p.Shutdown()

	var maxConcurrent int32
	var current int32

	for range 20 {
		err := p.Submit(func() {
			c := atomic.AddInt32(&current, 1)
			for {
				old := atomic.LoadInt32(&maxConcurrent)
				if c <= old || atomic.CompareAndSwapInt32(&maxConcurrent, old, c) {
					break
				}
			}
			time.Sleep(50 * time.Millisecond)
			atomic.AddInt32(&current, -1)
		})
		if err != nil {
			t.Fatalf("Submit() error = %v", err)
		}
	}

	p.Wait()
	if maxConcurrent > 5 {
		t.Errorf("maxConcurrent = %d, want <= 5", maxConcurrent)
	}
}

func TestPool_ShutdownWait(t *testing.T) {
	p, err := New(Config{Size: 2, ExpiryDuration: time.Second})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var completed int32
	for range 5 {
		_ = p.Submit(func() {
			time.Sleep(50 * time.Millisecond)
			atomic.AddInt32(&completed, 1)
		})
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err = p.ShutdownWait(ctx)
	if err != nil {
		t.Errorf("ShutdownWait() error = %v", err)
	}
	if completed != 5 {
		t.Errorf("completed = %d, want 5", completed)
	}
}

func TestDefaultConfig(t *testing.T) {
	t.Parallel()

	cfg := DefaultConfig()
	if cfg.Size != 10 {
		t.Fatalf("Size = %d, want 10", cfg.Size)
	}
	if cfg.PreAlloc {
		t.Fatal("PreAlloc = true, want false")
	}
	if cfg.ExpiryDuration != 10*time.Second {
		t.Fatalf("ExpiryDuration = %v, want 10s", cfg.ExpiryDuration)
	}
}

func TestPool_CapRunningFree(t *testing.T) {
	t.Parallel()

	p, err := New(Config{Size: 4, ExpiryDuration: time.Second})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer p.Shutdown()

	if p.Cap() != 4 {
		t.Fatalf("Cap() = %d, want 4", p.Cap())
	}
	if p.Free() < 0 {
		t.Fatalf("Free() = %d, want >= 0", p.Free())
	}

	started := make(chan struct{})
	_ = p.Submit(func() {
		close(started)
		time.Sleep(100 * time.Millisecond)
	})
	<-started

	if p.Running() < 1 {
		t.Fatalf("Running() = %d, want >= 1", p.Running())
	}

	p.Wait()
}

func TestNew_WithPreAllocAndMaxBlocking(t *testing.T) {
	t.Parallel()

	p, err := New(Config{
		Size:            3,
		PreAlloc:        true,
		ExpiryDuration:  time.Second,
		MaxBlockingTask: 5,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer p.Shutdown()

	if p.Cap() != 3 {
		t.Fatalf("Cap() = %d, want 3", p.Cap())
	}
}

func TestPool_Submit_RejectsWhenNonblockingFull(t *testing.T) {
	t.Parallel()

	p, err := New(Config{Size: 1, ExpiryDuration: time.Second, Nonblocking: true})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer p.Shutdown()

	started := make(chan struct{})
	_ = p.Submit(func() {
		close(started)
		time.Sleep(200 * time.Millisecond)
	})
	<-started

	if err := p.Submit(func() {}); err == nil {
		t.Fatal("Submit() expected error when pool is full and nonblocking")
	}
	p.Wait()
}

func TestPool_ShutdownWait_Timeout(t *testing.T) {
	p, err := New(Config{Size: 1, ExpiryDuration: time.Second})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	_ = p.Submit(func() {
		time.Sleep(500 * time.Millisecond)
	})

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err = p.ShutdownWait(ctx)
	if err != context.DeadlineExceeded {
		t.Errorf("ShutdownWait() error = %v, want context.DeadlineExceeded", err)
	}
}
