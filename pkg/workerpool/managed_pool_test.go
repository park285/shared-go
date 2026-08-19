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

func trySubmit(pool *workerpool.ManagedPool, spec workerpool.JobSpec) bool {
	return pool.TrySubmitResult(spec).Accepted
}

func newManagedPoolForTest(t *testing.T, config workerpool.ManagedConfig) *workerpool.ManagedPool {
	t.Helper()
	if config.FinalizeTimeout == 0 {
		config.FinalizeTimeout = 5 * time.Second
	}
	if config.FinalizeConcurrency == 0 {
		config.FinalizeConcurrency = config.Workers
	}
	if config.FinalizeQueueSize == 0 {
		config.FinalizeQueueSize = config.Workers + config.QueueSize
	}
	pool, err := workerpool.NewManagedPool(config)
	if err != nil {
		t.Fatalf("NewManagedPool() error = %v", err)
	}
	return pool
}

func TestManagedPoolCreatesJobBudgetAtDequeue(t *testing.T) {
	pool := newManagedPoolForTest(t, workerpool.ManagedConfig{Workers: 1, QueueSize: 2})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := pool.CloseContext(ctx); err != nil {
			t.Errorf("CloseContext() error = %v", err)
		}
	})

	started := make(chan struct{})
	release := make(chan struct{})
	if !trySubmit(pool, workerpool.JobSpec{
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
	if !trySubmit(pool, workerpool.JobSpec{
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
	pool := newManagedPoolForTest(t, workerpool.ManagedConfig{Workers: 1, QueueSize: 1})
	started := make(chan struct{})
	release := make(chan struct{})
	if !trySubmit(pool, workerpool.JobSpec{
		Kind: "blocker",
		Run: func(context.Context) {
			close(started)
			<-release
		},
	}) {
		t.Fatal("TrySubmit(blocker) = false")
	}
	awaitClosed(t, started, "blocker start")
	if !trySubmit(pool, workerpool.JobSpec{Kind: "queued", Run: func(context.Context) {}}) {
		t.Fatal("TrySubmit(queued) = false")
	}

	finalized := make(chan workerpool.JobOutcome, 1)
	var finalizeCalls atomic.Int32
	result := pool.TrySubmitResult(workerpool.JobSpec{
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

	if result.Accepted || !result.FinalizerClaimed || result.Reason != workerpool.ManagedSubmitRejectedFinalizerScheduled {
		t.Fatalf("TrySubmitResult(rejected) = %+v, want claimed rejection finalizer", result)
	}
	if outcome := awaitValue(t, finalized, "rejection finalizer"); outcome != workerpool.JobOutcomeRejected {
		t.Fatalf("outcome = %v, want rejected", outcome)
	}
	if got := finalizeCalls.Load(); got != 1 {
		t.Fatalf("Finalize calls = %d, want 1", got)
	}
}

func TestManagedPoolRunNilRejectionClaimsFinalizer(t *testing.T) {
	finalized := make(chan workerpool.JobOutcome, 1)
	pool := newManagedPoolForTest(t, workerpool.ManagedConfig{Workers: 1, QueueSize: 1, FinalizeQueueSize: 1})
	result := pool.TrySubmitResult(workerpool.JobSpec{
		Finalize: func(_ context.Context, outcome workerpool.JobOutcome) {
			finalized <- outcome
		},
	})
	if result.Accepted || !result.FinalizerClaimed || result.Reason != workerpool.ManagedSubmitRejectedFinalizerScheduled {
		t.Fatalf("TrySubmitResult(nil Run) = %+v, want claimed rejection finalizer", result)
	}
	if outcome := awaitValue(t, finalized, "nil Run finalizer"); outcome != workerpool.JobOutcomeRejected {
		t.Fatalf("outcome = %v, want rejected", outcome)
	}
	awaitManagedSnapshot(t, pool, "nil Run reservation release", func(snapshot workerpool.ManagedSnapshot) bool {
		return snapshot.Finalizer.Reservations == 0 && snapshot.Finalizer.Completed == 1
	})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := pool.CloseContext(ctx); err != nil {
		t.Fatalf("CloseContext() error = %v", err)
	}
}

func TestManagedPoolReaperFinalizesStaleJobWhileWorkersAreBusy(t *testing.T) {
	pool := newManagedPoolForTest(t, workerpool.ManagedConfig{Workers: 1, QueueSize: 2})
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
	if !trySubmit(pool, workerpool.JobSpec{
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
	if !trySubmit(pool, workerpool.JobSpec{
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
	pool := newManagedPoolForTest(t, workerpool.ManagedConfig{Workers: 1, QueueSize: 1})
	started := make(chan struct{})
	release := make(chan struct{})
	if !trySubmit(pool, workerpool.JobSpec{
		Kind: "running",
		Run: func(context.Context) {
			close(started)
			<-release
		},
	}) {
		t.Fatal("TrySubmit(running) = false")
	}
	awaitClosed(t, started, "running job start")
	if !trySubmit(pool, workerpool.JobSpec{Kind: "queued", Run: func(context.Context) {}}) {
		t.Fatal("TrySubmit(queued) = false")
	}
	if trySubmit(pool, workerpool.JobSpec{Kind: "rejected", Run: func(context.Context) {}}) {
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
	if snapshot.ConfiguredWorkers != 1 || snapshot.RunningWorkers != 1 || snapshot.OldestInFlightAge <= 0 {
		t.Fatalf("worker snapshot = %+v", snapshot)
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

func TestNewManagedPoolRejectsInvalidConfiguration(t *testing.T) {
	valid := workerpool.ManagedConfig{
		Workers:             1,
		QueueSize:           1,
		FinalizeTimeout:     time.Second,
		FinalizeConcurrency: 1,
		FinalizeQueueSize:   1,
	}
	tests := []struct {
		name   string
		mutate func(*workerpool.ManagedConfig)
	}{
		{"workers", func(config *workerpool.ManagedConfig) { config.Workers = 0 }},
		{"queue", func(config *workerpool.ManagedConfig) { config.QueueSize = 0 }},
		{"finalize timeout", func(config *workerpool.ManagedConfig) { config.FinalizeTimeout = 0 }},
		{"finalize concurrency", func(config *workerpool.ManagedConfig) { config.FinalizeConcurrency = 0 }},
		{"finalize capacity", func(config *workerpool.ManagedConfig) { config.FinalizeQueueSize = 0 }},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			config := valid
			testCase.mutate(&config)
			pool, err := workerpool.NewManagedPool(config)
			if err == nil || pool != nil {
				t.Fatalf("NewManagedPool() = %v, %v", pool, err)
			}
		})
	}
}

func TestManagedPoolShutdownDropsQueuedAndCancelsInFlight(t *testing.T) {
	pool := newManagedPoolForTest(t, workerpool.ManagedConfig{Workers: 1, QueueSize: 2})
	started := make(chan struct{})
	runCause := make(chan error, 1)
	runOutcome := make(chan workerpool.JobOutcome, 1)
	if !trySubmit(pool, workerpool.JobSpec{
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
	if !trySubmit(pool, workerpool.JobSpec{
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
	pool := newManagedPoolForTest(t, workerpool.ManagedConfig{Workers: 1, QueueSize: 1})
	causeCh := make(chan error, 1)
	outcomeCh := make(chan workerpool.JobOutcome, 1)
	if !trySubmit(pool, workerpool.JobSpec{
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
	pool := newManagedPoolForTest(t, workerpool.ManagedConfig{Workers: 4, QueueSize: 4})
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
		if !trySubmit(pool, workerpool.JobSpec{
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
	pool := newManagedPoolForTest(t, workerpool.ManagedConfig{Workers: 4, QueueSize: 4})
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
		if !trySubmit(pool, workerpool.JobSpec{
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
	pool := newManagedPoolForTest(t, workerpool.ManagedConfig{Workers: 1, QueueSize: 2, FinalizeTimeout: time.Millisecond})
	if !trySubmit(pool, workerpool.JobSpec{Run: func(context.Context) {
		close(started)
		<-releaseRun
	}}) {
		t.Fatal("first TrySubmit() = false")
	}
	if !trySubmit(pool, workerpool.JobSpec{
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

func TestManagedPoolCloseContextWaitsForQueuedFinalizerCompletion(t *testing.T) {
	started := make(chan struct{})
	releaseFinalize := make(chan struct{})
	pool := newManagedPoolForTest(t, workerpool.ManagedConfig{Workers: 1, QueueSize: 2})
	if !trySubmit(pool, workerpool.JobSpec{Run: func(ctx context.Context) {
		close(started)
		<-ctx.Done()
	}}) {
		t.Fatal("TrySubmit(running) = false")
	}
	if !trySubmit(pool, workerpool.JobSpec{
		Run: func(context.Context) {},
		Finalize: func(context.Context, workerpool.JobOutcome) {
			<-releaseFinalize
		},
	}) {
		t.Fatal("TrySubmit(queued) = false")
	}
	awaitClosed(t, started, "running job start")

	closed := make(chan error, 1)
	go func() { closed <- pool.CloseContext(context.Background()) }()
	awaitManagedSnapshot(t, pool, "queued finalizer start", func(snapshot workerpool.ManagedSnapshot) bool {
		return snapshot.Finalizer.InFlight == 1 && snapshot.Finalizer.Started == 1
	})
	select {
	case err := <-closed:
		t.Fatalf("CloseContext() returned before finalizer completion: %v", err)
	default:
	}
	close(releaseFinalize)
	if err := awaitValue(t, closed, "background CloseContext completion"); err != nil {
		t.Fatalf("CloseContext() error = %v", err)
	}
}

func TestManagedPoolFinalizerDispatchDrainsAfterCloseWhileCallbackStillRuns(t *testing.T) {
	firstStarted := make(chan struct{})
	secondStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	releaseSecond := make(chan struct{})
	var releaseFirstOnce sync.Once
	var releaseSecondOnce sync.Once
	releaseFirstCallback := func() { releaseFirstOnce.Do(func() { close(releaseFirst) }) }
	releaseSecondCallback := func() { releaseSecondOnce.Do(func() { close(releaseSecond) }) }
	t.Cleanup(releaseFirstCallback)
	t.Cleanup(releaseSecondCallback)
	pool := newManagedPoolForTest(t, workerpool.ManagedConfig{
		Workers:             1,
		QueueSize:           2,
		FinalizeConcurrency: 1,
		FinalizeQueueSize:   2,
	})
	if !trySubmit(pool, workerpool.JobSpec{
		Run: func(context.Context) {},
		Finalize: func(context.Context, workerpool.JobOutcome) {
			close(firstStarted)
			<-releaseFirst
		},
	}) {
		t.Fatal("TrySubmit(first) = false")
	}
	awaitClosed(t, firstStarted, "first finalizer start")
	if !trySubmit(pool, workerpool.JobSpec{
		Run: func(context.Context) {},
		Finalize: func(context.Context, workerpool.JobOutcome) {
			close(secondStarted)
			<-releaseSecond
		},
	}) {
		t.Fatal("TrySubmit(second) = false")
	}
	awaitManagedSnapshot(t, pool, "second finalizer queue", func(snapshot workerpool.ManagedSnapshot) bool {
		return snapshot.Finalizer.QueueDepth == 1 && snapshot.Finalizer.InFlight == 1
	})
	closed := make(chan error, 1)
	go func() { closed <- pool.CloseContext(context.Background()) }()
	awaitManagedSnapshot(t, pool, "closed finalizer lifecycle", func(snapshot workerpool.ManagedSnapshot) bool {
		return !snapshot.Finalizer.Accepting && !snapshot.Finalizer.DispatchDrained
	})
	select {
	case err := <-closed:
		t.Fatalf("CloseContext() returned before first finalizer completion: %v", err)
	default:
	}
	releaseFirstCallback()
	awaitClosed(t, secondStarted, "second finalizer start")
	awaitManagedSnapshot(t, pool, "finalizer dispatch drain", func(snapshot workerpool.ManagedSnapshot) bool {
		return !snapshot.Finalizer.Accepting && snapshot.Finalizer.DispatchDrained && snapshot.Finalizer.InFlight == 1
	})
	select {
	case err := <-closed:
		t.Fatalf("CloseContext() returned before second finalizer completion: %v", err)
	default:
	}
	releaseSecondCallback()
	if err := awaitValue(t, closed, "finalizer close completion"); err != nil {
		t.Fatalf("CloseContext() error = %v", err)
	}
	awaitManagedSnapshot(t, pool, "late callback completion", func(snapshot workerpool.ManagedSnapshot) bool {
		return snapshot.Finalizer.InFlight == 0 && snapshot.Finalizer.Reservations == 0
	})
}

func TestManagedPoolAcceptedJobsRetainFinalizerReservation(t *testing.T) {
	releaseFinalize := make(chan struct{})
	var runCalls atomic.Int32
	var finalizeCalls atomic.Int32
	pool := newManagedPoolForTest(t, workerpool.ManagedConfig{
		Workers:             1,
		QueueSize:           8,
		FinalizeConcurrency: 1,
		FinalizeQueueSize:   2,
		FinalizeTimeout:     time.Second,
	})
	for index := range 2 {
		if !trySubmit(pool, workerpool.JobSpec{
			Kind: fmt.Sprintf("reserved-%d", index),
			Run: func(context.Context) {
				runCalls.Add(1)
			},
			Finalize: func(context.Context, workerpool.JobOutcome) {
				finalizeCalls.Add(1)
				<-releaseFinalize
			},
		}) {
			t.Fatalf("TrySubmit(%d) = false", index)
		}
	}
	awaitManagedSnapshot(t, pool, "initial finalizer reservation", func(snapshot workerpool.ManagedSnapshot) bool {
		return snapshot.Finalizer.InFlight == 1 && snapshot.Finalizer.QueueDepth == 1
	})
	result := pool.TrySubmitResult(workerpool.JobSpec{
		Kind: "unreserved",
		Run: func(context.Context) {
			runCalls.Add(1)
		},
		Finalize: func(context.Context, workerpool.JobOutcome) {
			finalizeCalls.Add(1)
		},
	})
	if result.Accepted || result.FinalizerClaimed || result.Reason != workerpool.ManagedSubmitRejectedFinalizerCapacity {
		t.Fatalf("TrySubmitResult(unreserved) = %+v, want unclaimed capacity rejection", result)
	}

	awaitManagedSnapshot(t, pool, "finalizer reservation overload", func(snapshot workerpool.ManagedSnapshot) bool {
		return snapshot.Finalizer.InFlight == 1 &&
			snapshot.Finalizer.QueueDepth == 1 &&
			snapshot.Finalizer.Overloaded == 1 &&
			snapshot.Finalizer.ReservationRejected == 1
	})
	close(releaseFinalize)
	awaitManagedSnapshot(t, pool, "reserved finalizer completion", func(snapshot workerpool.ManagedSnapshot) bool {
		return snapshot.Finalizer.Completed == 2 && snapshot.Finalizer.InFlight == 0
	})
	if got := runCalls.Load(); got != 2 {
		t.Fatalf("Run calls = %d, want 2", got)
	}
	if got := finalizeCalls.Load(); got != 2 {
		t.Fatalf("Finalize calls = %d, want 2", got)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := pool.CloseContext(ctx); err != nil {
		t.Fatalf("CloseContext() error = %v", err)
	}
}

func TestManagedPoolAcceptedQueuedFinalizerStartsAfterSlotReturns(t *testing.T) {
	const finalizeTimeout = 100 * time.Millisecond
	releaseFirst := make(chan struct{})
	secondStarted := make(chan struct {
		outcome   workerpool.JobOutcome
		ctxErr    error
		remaining time.Duration
	}, 1)
	var firstCalls atomic.Int32
	var secondCalls atomic.Int32
	pool := newManagedPoolForTest(t, workerpool.ManagedConfig{
		Workers:             1,
		QueueSize:           2,
		FinalizeConcurrency: 1,
		FinalizeQueueSize:   2,
		FinalizeTimeout:     finalizeTimeout,
	})
	if !trySubmit(pool, workerpool.JobSpec{
		Run: func(context.Context) {},
		Finalize: func(ctx context.Context, _ workerpool.JobOutcome) {
			firstCalls.Add(1)
			<-ctx.Done()
			<-releaseFirst
		},
	}) {
		t.Fatal("TrySubmit(first finalizer) = false")
	}
	if !trySubmit(pool, workerpool.JobSpec{
		Run: func(context.Context) {},
		Finalize: func(ctx context.Context, outcome workerpool.JobOutcome) {
			secondCalls.Add(1)
			deadline, ok := ctx.Deadline()
			if !ok {
				secondStarted <- struct {
					outcome   workerpool.JobOutcome
					ctxErr    error
					remaining time.Duration
				}{outcome: outcome, ctxErr: ctx.Err(), remaining: -1}
				return
			}
			secondStarted <- struct {
				outcome   workerpool.JobOutcome
				ctxErr    error
				remaining time.Duration
			}{outcome: outcome, ctxErr: ctx.Err(), remaining: time.Until(deadline)}
		},
	}) {
		t.Fatal("TrySubmit(second finalizer) = false")
	}
	awaitManagedSnapshot(t, pool, "timed-out first and queued second finalizer", func(snapshot workerpool.ManagedSnapshot) bool {
		return snapshot.Finalizer.TimedOut == 1 &&
			snapshot.Finalizer.InFlight == 1 &&
			snapshot.Finalizer.QueueDepth == 1 &&
			snapshot.Finalizer.Reservations == 2
	})
	if got := secondCalls.Load(); got != 0 {
		t.Fatalf("second Finalize calls before slot release = %d, want 0", got)
	}
	close(releaseFirst)
	second := awaitValue(t, secondStarted, "accepted queued finalizer start")
	if second.outcome != workerpool.JobOutcomeSuccess || second.ctxErr != nil {
		t.Fatalf("second Finalize state = %+v, want success with live context", second)
	}
	if second.remaining < finalizeTimeout/2 {
		t.Fatalf("second Finalize deadline remaining = %v, want fresh start-time budget", second.remaining)
	}
	awaitManagedSnapshot(t, pool, "accepted finalizer completion", func(snapshot workerpool.ManagedSnapshot) bool {
		return snapshot.Finalizer.CompletedLate == 1 &&
			snapshot.Finalizer.Completed == 1 &&
			snapshot.Finalizer.Reservations == 0
	})
	if got := firstCalls.Load(); got != 1 {
		t.Fatalf("first Finalize calls = %d, want 1", got)
	}
	if got := secondCalls.Load(); got != 1 {
		t.Fatalf("second Finalize calls = %d, want 1", got)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := pool.CloseContext(ctx); err != nil {
		t.Fatalf("CloseContext() error = %v", err)
	}
}

func TestManagedPoolFinalizerReportsLateCompletionOnce(t *testing.T) {
	releaseFinalize := make(chan struct{})
	pool := newManagedPoolForTest(t, workerpool.ManagedConfig{
		Workers:           1,
		QueueSize:         1,
		FinalizeQueueSize: 1,
		FinalizeTimeout:   20 * time.Millisecond,
	})
	if !trySubmit(pool, workerpool.JobSpec{
		Run: func(context.Context) {},
		Finalize: func(ctx context.Context, _ workerpool.JobOutcome) {
			<-ctx.Done()
			<-releaseFinalize
		},
	}) {
		t.Fatal("TrySubmit(late finalizer) = false")
	}
	awaitManagedSnapshot(t, pool, "finalizer timeout", func(snapshot workerpool.ManagedSnapshot) bool {
		return snapshot.Finalizer.TimedOut == 1 &&
			snapshot.Finalizer.InFlight == 1 &&
			snapshot.Finalizer.Reservations == 1
	})
	capacityResult := pool.TrySubmitResult(workerpool.JobSpec{
		Run:      func(context.Context) {},
		Finalize: func(context.Context, workerpool.JobOutcome) {},
	})
	if capacityResult.Accepted || capacityResult.FinalizerClaimed || capacityResult.Reason != workerpool.ManagedSubmitRejectedFinalizerCapacity {
		t.Fatalf("TrySubmitResult(while late) = %+v, want unclaimed capacity rejection", capacityResult)
	}
	close(releaseFinalize)
	awaitManagedSnapshot(t, pool, "late finalizer completion", func(snapshot workerpool.ManagedSnapshot) bool {
		return snapshot.Finalizer.CompletedLate == 1 &&
			snapshot.Finalizer.Completed == 0 &&
			snapshot.Finalizer.InFlight == 0 &&
			snapshot.Finalizer.Reservations == 0
	})
	if result := pool.TrySubmitResult(workerpool.JobSpec{
		Run:      func(context.Context) {},
		Finalize: func(context.Context, workerpool.JobOutcome) {},
	}); !result.Accepted || !result.FinalizerClaimed || result.Reason != workerpool.ManagedSubmitAccepted {
		t.Fatalf("TrySubmitResult(after late) = %+v, want accepted finalizer claim", result)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := pool.CloseContext(ctx); err != nil {
		t.Fatalf("CloseContext() error = %v", err)
	}
}

func TestManagedPoolFinalizerContainsPanic(t *testing.T) {
	pool := newManagedPoolForTest(t, workerpool.ManagedConfig{Workers: 1, QueueSize: 1})
	if !trySubmit(pool, workerpool.JobSpec{
		Run: func(context.Context) {},
		Finalize: func(context.Context, workerpool.JobOutcome) {
			panic("finalizer panic")
		},
	}) {
		t.Fatal("TrySubmit(panicking finalizer) = false")
	}
	awaitManagedSnapshot(t, pool, "finalizer panic", func(snapshot workerpool.ManagedSnapshot) bool {
		return snapshot.Finalizer.Panicked == 1 && snapshot.Finalizer.InFlight == 0
	})
	if !trySubmit(pool, workerpool.JobSpec{Run: func(context.Context) {}}) {
		t.Fatal("TrySubmit(after finalizer panic) = false")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := pool.CloseContext(ctx); err != nil {
		t.Fatalf("CloseContext() error = %v", err)
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

func awaitManagedSnapshot(t *testing.T, pool *workerpool.ManagedPool, name string, ready func(workerpool.ManagedSnapshot) bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for {
		if snapshot := pool.Snapshot(); ready(snapshot) {
			return
		}
		if !time.Now().Before(deadline) {
			t.Fatalf("timed out waiting for %s: %+v", name, pool.Snapshot())
		}
		time.Sleep(time.Millisecond)
	}
}
