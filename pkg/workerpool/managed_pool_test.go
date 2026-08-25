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

	"github.com/park285/shared-go/v2/pkg/workerpool"
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
	closeManagedPoolOnCleanup(t, pool)

	release := startBlockerJob(t, pool)

	receiveCtx, cancelReceive := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancelReceive()

	runResult := make(chan error, 1)
	finalized := make(chan workerpool.JobOutcome, 1)

	var finalizeCalls atomic.Int32

	if !trySubmit(pool, workerpool.JobSpec{
		Context: receiveCtx,
		Kind:    "ask",
		Timeout: 200 * time.Millisecond,
		Run: func(jobCtx context.Context) {
			runResult <- checkDequeuedJobBudget(jobCtx)
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

	assertCallCount(t, &finalizeCalls, "Finalize calls", 1)
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

	ctx, cancel := context.WithTimeout(t.Context(), time.Second)

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

	ctx, cancel := context.WithTimeout(t.Context(), time.Second)

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

		ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), time.Second)

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

	var (
		runCalls      atomic.Int32
		finalizeCalls atomic.Int32
	)

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

	ctx, cancel := context.WithTimeout(t.Context(), time.Second)

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

	var (
		queuedRuns       atomic.Int32
		queuedFinalizers atomic.Int32
	)

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

	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	if err := pool.CloseContext(ctx); err != nil {
		t.Fatalf("CloseContext() error = %v", err)
	}

	if cause := awaitValue(t, runCause, "running cancellation cause"); !errors.Is(cause, workerpool.ErrPoolShutdown) {
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

	if cause := awaitValue(t, causeCh, "timeout cause"); !errors.Is(cause, workerpool.ErrJobTimeout) {
		t.Fatalf("cause = %v, want ErrJobTimeout", cause)
	}

	if outcome := awaitValue(t, outcomeCh, "timeout outcome"); outcome != workerpool.JobOutcomeTimeout {
		t.Fatalf("outcome = %v, want timeout", outcome)
	}

	ctx, cancel := context.WithTimeout(t.Context(), time.Second)

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

		ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), time.Second)

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

		ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), time.Second)

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
