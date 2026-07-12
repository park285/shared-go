package workerpool_test

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/park285/shared-go/pkg/workerpool"
)

func TestManagedPoolCreatesJobBudgetAtDequeue(t *testing.T) {
	pool := workerpool.NewManaged(workerpool.ManagedConfig{Workers: 1, QueueSize: 2})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := pool.CloseContext(ctx); err != nil {
			t.Errorf("CloseContext() error = %v", err)
		}
	})

	started := make(chan struct{})
	release := make(chan struct{})
	if !pool.TrySubmit(workerpool.JobSpec{
		Kind:    "blocker",
		Timeout: time.Second,
		Run: func(context.Context) {
			close(started)
			<-release
		},
	}) {
		t.Fatal("TrySubmit(blocker) = false")
	}
	awaitClosed(t, started, "blocker start")

	receiveCtx, cancelReceive := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancelReceive()
	runResult := make(chan error, 1)
	finalized := make(chan workerpool.JobOutcome, 1)
	var finalizeCalls atomic.Int32
	if !pool.TrySubmit(workerpool.JobSpec{
		Context: receiveCtx,
		Kind:    "ask",
		Timeout: 200 * time.Millisecond,
		Run: func(jobCtx context.Context) {
			if err := jobCtx.Err(); err != nil {
				runResult <- fmt.Errorf("job context started canceled: %w", err)
				return
			}
			deadline, ok := jobCtx.Deadline()
			if !ok || time.Until(deadline) < 100*time.Millisecond {
				runResult <- fmt.Errorf("job deadline was not created at dequeue: %v, %v", deadline, ok)
				return
			}
			runResult <- nil
		},
		Finalize: func(_ context.Context, outcome workerpool.JobOutcome) {
			finalizeCalls.Add(1)
			finalized <- outcome
		},
	}) {
		t.Fatal("TrySubmit(ask) = false")
	}

	awaitClosed(t, receiveCtx.Done(), "receive context expiry")
	close(release)
	if err := awaitValue(t, runResult, "ask run"); err != nil {
		t.Fatal(err)
	}
	if outcome := awaitValue(t, finalized, "ask finalizer"); outcome != workerpool.JobOutcomeSuccess {
		t.Fatalf("outcome = %v, want success", outcome)
	}
	if got := finalizeCalls.Load(); got != 1 {
		t.Fatalf("Finalize calls = %d, want 1", got)
	}
}

func TestManagedPoolFinalizesQueueRejectionExactlyOnce(t *testing.T) {
	pool := workerpool.NewManaged(workerpool.ManagedConfig{Workers: 1, QueueSize: 1})
	started := make(chan struct{})
	release := make(chan struct{})
	if !pool.TrySubmit(workerpool.JobSpec{
		Kind: "blocker",
		Run: func(context.Context) {
			close(started)
			<-release
		},
	}) {
		t.Fatal("TrySubmit(blocker) = false")
	}
	awaitClosed(t, started, "blocker start")
	if !pool.TrySubmit(workerpool.JobSpec{Kind: "queued", Run: func(context.Context) {}}) {
		t.Fatal("TrySubmit(queued) = false")
	}

	finalized := make(chan workerpool.JobOutcome, 1)
	var finalizeCalls atomic.Int32
	accepted := pool.TrySubmit(workerpool.JobSpec{
		Kind: "rejected",
		Run:  func(context.Context) {},
		Finalize: func(_ context.Context, outcome workerpool.JobOutcome) {
			finalizeCalls.Add(1)
			finalized <- outcome
		},
	})
	close(release)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := pool.CloseContext(ctx); err != nil {
		t.Fatalf("CloseContext() error = %v", err)
	}

	if accepted {
		t.Fatal("TrySubmit(rejected) = true, want false")
	}
	if outcome := awaitValue(t, finalized, "rejection finalizer"); outcome != workerpool.JobOutcomeRejected {
		t.Fatalf("outcome = %v, want rejected", outcome)
	}
	if got := finalizeCalls.Load(); got != 1 {
		t.Fatalf("Finalize calls = %d, want 1", got)
	}
}

func TestManagedPoolReaperFinalizesStaleJobWhileWorkersAreBusy(t *testing.T) {
	pool := workerpool.NewManaged(workerpool.ManagedConfig{Workers: 1, QueueSize: 2})
	started := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseBlocker := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(func() {
		releaseBlocker()
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := pool.CloseContext(ctx); err != nil {
			t.Errorf("CloseContext() error = %v", err)
		}
	})
	if !pool.TrySubmit(workerpool.JobSpec{
		Kind: "blocker",
		Run: func(context.Context) {
			close(started)
			<-release
		},
	}) {
		t.Fatal("TrySubmit(blocker) = false")
	}
	awaitClosed(t, started, "blocker start")

	finalized := make(chan workerpool.JobOutcome, 1)
	var runCalls atomic.Int32
	var finalizeCalls atomic.Int32
	if !pool.TrySubmit(workerpool.JobSpec{
		Kind:        "stale",
		MaxQueueAge: 30 * time.Millisecond,
		Run:         func(context.Context) { runCalls.Add(1) },
		Finalize: func(_ context.Context, outcome workerpool.JobOutcome) {
			finalizeCalls.Add(1)
			finalized <- outcome
		},
	}) {
		t.Fatal("TrySubmit(stale) = false")
	}

	if outcome := awaitValue(t, finalized, "stale finalizer"); outcome != workerpool.JobOutcomeStale {
		t.Fatalf("outcome = %v, want stale", outcome)
	}
	if got := runCalls.Load(); got != 0 {
		t.Fatalf("Run calls = %d, want 0", got)
	}
	if got := finalizeCalls.Load(); got != 1 {
		t.Fatalf("Finalize calls = %d, want 1", got)
	}
	releaseBlocker()
}

func TestManagedPoolSnapshotReportsQueueInFlightAgeAndOutcomes(t *testing.T) {
	pool := workerpool.NewManaged(workerpool.ManagedConfig{Workers: 1, QueueSize: 1})
	started := make(chan struct{})
	release := make(chan struct{})
	if !pool.TrySubmit(workerpool.JobSpec{
		Kind: "running",
		Run: func(context.Context) {
			close(started)
			<-release
		},
	}) {
		t.Fatal("TrySubmit(running) = false")
	}
	awaitClosed(t, started, "running job start")
	if !pool.TrySubmit(workerpool.JobSpec{Kind: "queued", Run: func(context.Context) {}}) {
		t.Fatal("TrySubmit(queued) = false")
	}
	if pool.TrySubmit(workerpool.JobSpec{Kind: "rejected", Run: func(context.Context) {}}) {
		t.Fatal("TrySubmit(rejected) = true")
	}
	time.Sleep(time.Millisecond)

	snapshot := pool.Snapshot()
	if snapshot.QueueDepth != 1 || snapshot.InFlight != 1 {
		t.Fatalf("snapshot queue/in-flight = %d/%d, want 1/1", snapshot.QueueDepth, snapshot.InFlight)
	}
	if snapshot.OldestQueueAge <= 0 {
		t.Fatalf("OldestQueueAge = %v, want > 0", snapshot.OldestQueueAge)
	}
	if snapshot.Outcomes[workerpool.JobOutcomeRejected] != 1 {
		t.Fatalf("rejected outcomes = %d, want 1", snapshot.Outcomes[workerpool.JobOutcomeRejected])
	}

	close(release)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := pool.CloseContext(ctx); err != nil {
		t.Fatalf("CloseContext() error = %v", err)
	}
}

func TestManagedPoolShutdownDropsQueuedAndCancelsInFlight(t *testing.T) {
	pool := workerpool.NewManaged(workerpool.ManagedConfig{Workers: 1, QueueSize: 2})
	started := make(chan struct{})
	runCause := make(chan error, 1)
	runOutcome := make(chan workerpool.JobOutcome, 1)
	if !pool.TrySubmit(workerpool.JobSpec{
		Kind: "running",
		Run: func(ctx context.Context) {
			close(started)
			<-ctx.Done()
			runCause <- context.Cause(ctx)
		},
		Finalize: func(_ context.Context, outcome workerpool.JobOutcome) {
			runOutcome <- outcome
		},
	}) {
		t.Fatal("TrySubmit(running) = false")
	}
	awaitClosed(t, started, "running job start")

	queuedOutcome := make(chan workerpool.JobOutcome, 1)
	var queuedRuns atomic.Int32
	var queuedFinalizers atomic.Int32
	if !pool.TrySubmit(workerpool.JobSpec{
		Kind: "queued",
		Run:  func(context.Context) { queuedRuns.Add(1) },
		Finalize: func(_ context.Context, outcome workerpool.JobOutcome) {
			queuedFinalizers.Add(1)
			queuedOutcome <- outcome
		},
	}) {
		t.Fatal("TrySubmit(queued) = false")
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := pool.CloseContext(ctx); err != nil {
		t.Fatalf("CloseContext() error = %v", err)
	}
	if cause := awaitValue(t, runCause, "running cancellation cause"); cause != workerpool.ErrPoolShutdown {
		t.Fatalf("running cause = %v, want ErrPoolShutdown", cause)
	}
	if outcome := awaitValue(t, runOutcome, "running finalizer"); outcome != workerpool.JobOutcomeShutdown {
		t.Fatalf("running outcome = %v, want shutdown", outcome)
	}
	if outcome := awaitValue(t, queuedOutcome, "queued finalizer"); outcome != workerpool.JobOutcomeShutdown {
		t.Fatalf("queued outcome = %v, want shutdown", outcome)
	}
	if got := queuedRuns.Load(); got != 0 {
		t.Fatalf("queued Run calls = %d, want 0", got)
	}
	if got := queuedFinalizers.Load(); got != 1 {
		t.Fatalf("queued Finalize calls = %d, want 1", got)
	}
}

func TestManagedPoolClassifiesTimeoutCause(t *testing.T) {
	pool := workerpool.NewManaged(workerpool.ManagedConfig{Workers: 1, QueueSize: 1})
	causeCh := make(chan error, 1)
	outcomeCh := make(chan workerpool.JobOutcome, 1)
	if !pool.TrySubmit(workerpool.JobSpec{
		Kind:    "timeout",
		Timeout: 20 * time.Millisecond,
		Run: func(ctx context.Context) {
			<-ctx.Done()
			causeCh <- context.Cause(ctx)
		},
		Finalize: func(_ context.Context, outcome workerpool.JobOutcome) {
			outcomeCh <- outcome
		},
	}) {
		t.Fatal("TrySubmit(timeout) = false")
	}

	if cause := awaitValue(t, causeCh, "timeout cause"); cause != workerpool.ErrJobTimeout {
		t.Fatalf("cause = %v, want ErrJobTimeout", cause)
	}
	if outcome := awaitValue(t, outcomeCh, "timeout outcome"); outcome != workerpool.JobOutcomeTimeout {
		t.Fatalf("outcome = %v, want timeout", outcome)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := pool.CloseContext(ctx); err != nil {
		t.Fatalf("CloseContext() error = %v", err)
	}
}

func TestManagedPoolStartsConfiguredWorkersForPreloadedQueue(t *testing.T) {
	previousProcs := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(previousProcs)
	pool := workerpool.NewManaged(workerpool.ManagedConfig{Workers: 4, QueueSize: 4})
	started := make(chan struct{}, 4)
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseWorkers := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(func() {
		releaseWorkers()
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := pool.CloseContext(ctx); err != nil {
			t.Errorf("CloseContext() error = %v", err)
		}
	})
	for index := range 4 {
		if !pool.TrySubmit(workerpool.JobSpec{
			Kind: fmt.Sprintf("parallel-%d", index),
			Run: func(context.Context) {
				started <- struct{}{}
				<-release
			},
		}) {
			t.Fatalf("TrySubmit(%d) = false", index)
		}
	}

	for index := range 4 {
		select {
		case <-started:
		case <-time.After(200 * time.Millisecond):
			t.Fatalf("started workers = %d, want 4", index)
		}
	}
	releaseWorkers()
}

func TestManagedPoolWakesConfiguredWorkersAlreadyWaiting(t *testing.T) {
	previousProcs := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(previousProcs)
	pool := workerpool.NewManaged(workerpool.ManagedConfig{Workers: 4, QueueSize: 4})
	started := make(chan struct{}, 4)
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseWorkers := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(func() {
		releaseWorkers()
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := pool.CloseContext(ctx); err != nil {
			t.Errorf("CloseContext() error = %v", err)
		}
	})

	// 모든 worker가 빈 queue에서 대기하도록 한 뒤 한 scheduler turn에 작업을 넣는다.
	time.Sleep(20 * time.Millisecond)
	for index := range 4 {
		if !pool.TrySubmit(workerpool.JobSpec{
			Kind: fmt.Sprintf("waiting-%d", index),
			Run: func(context.Context) {
				started <- struct{}{}
				<-release
			},
		}) {
			t.Fatalf("TrySubmit(%d) = false", index)
		}
	}

	for index := range 4 {
		select {
		case <-started:
		case <-time.After(200 * time.Millisecond):
			t.Fatalf("started workers = %d, want 4", index)
		}
	}
	releaseWorkers()
}

func TestManagedPoolCloseContextReturnsWhenQueuedFinalizerDoesNotReturn(t *testing.T) {
	started := make(chan struct{})
	releaseRun := make(chan struct{})
	releaseFinalize := make(chan struct{})
	pool := workerpool.NewManaged(workerpool.ManagedConfig{Workers: 1, QueueSize: 2, FinalizeTimeout: time.Millisecond})
	if !pool.TrySubmit(workerpool.JobSpec{Run: func(context.Context) {
		close(started)
		<-releaseRun
	}}) {
		t.Fatal("first TrySubmit() = false")
	}
	if !pool.TrySubmit(workerpool.JobSpec{
		Run: func(context.Context) {},
		Finalize: func(context.Context, workerpool.JobOutcome) {
			<-releaseFinalize
		},
	}) {
		t.Fatal("second TrySubmit() = false")
	}
	awaitClosed(t, started, "running job start")
	closeCtx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	begin := time.Now()
	err := pool.CloseContext(closeCtx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("CloseContext() error = %v, want context deadline", err)
	}
	if elapsed := time.Since(begin); elapsed > 100*time.Millisecond {
		t.Fatalf("CloseContext() elapsed = %v, want bounded return", elapsed)
	}
	close(releaseFinalize)
	close(releaseRun)
	if err := pool.CloseContext(context.Background()); err != nil {
		t.Fatalf("CloseContext(cleanup) error = %v", err)
	}
}

func awaitClosed(t *testing.T, ch <-chan struct{}, name string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for %s", name)
	}
}

func awaitValue[T any](t *testing.T, ch <-chan T, name string) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for %s", name)
		var zero T
		return zero
	}
}
