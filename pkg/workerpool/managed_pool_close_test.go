package workerpool_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/park285/shared-go/v2/pkg/workerpool"
)

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

	closeCtx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)

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

	if err := pool.CloseContext(t.Context()); err != nil {
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

	go func() { closed <- pool.CloseContext(t.Context()) }()

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
	pool := newManagedPoolForTest(t, workerpool.ManagedConfig{
		Workers:             1,
		QueueSize:           2,
		FinalizeConcurrency: 1,
		FinalizeQueueSize:   2,
	})

	firstStarted, releaseFirst := submitGatedFinalizeJob(t, pool, "first")

	awaitClosed(t, firstStarted, "first finalizer start")

	secondStarted, releaseSecond := submitGatedFinalizeJob(t, pool, "second")

	awaitManagedSnapshot(t, pool, "second finalizer queue", func(snapshot workerpool.ManagedSnapshot) bool {
		return snapshot.Finalizer.QueueDepth == 1 && snapshot.Finalizer.InFlight == 1
	})

	closed := make(chan error, 1)

	go func() { closed <- pool.CloseContext(t.Context()) }()

	awaitManagedSnapshot(t, pool, "closed finalizer lifecycle", func(snapshot workerpool.ManagedSnapshot) bool {
		return !snapshot.Finalizer.Accepting && !snapshot.Finalizer.DispatchDrained
	})
	requireCloseBlocked(t, closed, "first finalizer completion")

	releaseFirst()
	awaitClosed(t, secondStarted, "second finalizer start")
	awaitManagedSnapshot(t, pool, "finalizer dispatch drain", func(snapshot workerpool.ManagedSnapshot) bool {
		return !snapshot.Finalizer.Accepting && snapshot.Finalizer.DispatchDrained && snapshot.Finalizer.InFlight == 1
	})
	requireCloseBlocked(t, closed, "second finalizer completion")

	releaseSecond()

	if err := awaitValue(t, closed, "finalizer close completion"); err != nil {
		t.Fatalf("CloseContext() error = %v", err)
	}

	awaitManagedSnapshot(t, pool, "late callback completion", func(snapshot workerpool.ManagedSnapshot) bool {
		return snapshot.Finalizer.InFlight == 0 && snapshot.Finalizer.Reservations == 0
	})
}

func TestManagedPoolAcceptedJobsRetainFinalizerReservation(t *testing.T) {
	releaseFinalize := make(chan struct{})

	var (
		runCalls      atomic.Int32
		finalizeCalls atomic.Int32
	)

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
	expectFinalizerCapacityRejection(t, pool, "unreserved", workerpool.JobSpec{
		Kind: "unreserved",
		Run: func(context.Context) {
			runCalls.Add(1)
		},
		Finalize: func(context.Context, workerpool.JobOutcome) {
			finalizeCalls.Add(1)
		},
	})

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

	assertCallCount(t, &runCalls, "Run calls", 2)
	assertCallCount(t, &finalizeCalls, "Finalize calls", 2)

	closeManagedPool(t, pool)
}

func TestManagedPoolAcceptedQueuedFinalizerStartsAfterSlotReturns(t *testing.T) {
	const finalizeTimeout = 100 * time.Millisecond

	releaseFirst := make(chan struct{})
	secondStarted := make(chan finalizerCallState, 1)

	var (
		firstCalls  atomic.Int32
		secondCalls atomic.Int32
	)

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

			secondStarted <- captureFinalizerCallState(ctx, outcome)
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
	assertCallCount(t, &secondCalls, "second Finalize calls before slot release", 0)

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
	assertCallCount(t, &firstCalls, "first Finalize calls", 1)
	assertCallCount(t, &secondCalls, "second Finalize calls", 1)

	closeManagedPool(t, pool)
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

	ctx, cancel := context.WithTimeout(t.Context(), time.Second)

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

	ctx, cancel := context.WithTimeout(t.Context(), time.Second)

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

func closeManagedPool(t *testing.T, pool *workerpool.ManagedPool) {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), time.Second)

	defer cancel()

	if err := pool.CloseContext(ctx); err != nil {
		t.Fatalf("CloseContext() error = %v", err)
	}
}

func closeManagedPoolOnCleanup(t *testing.T, pool *workerpool.ManagedPool) {
	t.Helper()

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), time.Second)
		defer cancel()

		if err := pool.CloseContext(ctx); err != nil {
			t.Errorf("CloseContext() error = %v", err)
		}
	})
}

func newReleaseGate(t *testing.T) (chan struct{}, func()) {
	t.Helper()

	release := make(chan struct{})

	var releaseOnce sync.Once

	unblock := func() { releaseOnce.Do(func() { close(release) }) }

	t.Cleanup(unblock)

	return release, unblock
}

func startBlockerJob(t *testing.T, pool *workerpool.ManagedPool) chan struct{} {
	t.Helper()

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

	return release
}

func submitGatedFinalizeJob(t *testing.T, pool *workerpool.ManagedPool, label string) (chan struct{}, func()) {
	t.Helper()

	started := make(chan struct{})
	release, unblock := newReleaseGate(t)

	if !trySubmit(pool, workerpool.JobSpec{
		Run: func(context.Context) {},
		Finalize: func(context.Context, workerpool.JobOutcome) {
			close(started)
			<-release
		},
	}) {
		t.Fatalf("TrySubmit(%s) = false", label)
	}

	return started, unblock
}

func requireCloseBlocked(t *testing.T, closed <-chan error, stage string) {
	t.Helper()

	select {
	case err := <-closed:
		t.Fatalf("CloseContext() returned before %s: %v", stage, err)
	default:
	}
}

func expectFinalizerCapacityRejection(
	t *testing.T,
	pool *workerpool.ManagedPool,
	label string,
	spec workerpool.JobSpec,
) {
	t.Helper()

	result := pool.TrySubmitResult(spec)
	if result.Accepted || result.FinalizerClaimed ||
		result.Reason != workerpool.ManagedSubmitRejectedFinalizerCapacity {
		t.Fatalf("TrySubmitResult(%s) = %+v, want unclaimed capacity rejection", label, result)
	}
}

func assertCallCount(t *testing.T, counter *atomic.Int32, label string, want int32) {
	t.Helper()

	if got := counter.Load(); got != want {
		t.Fatalf("%s = %d, want %d", label, got, want)
	}
}

func checkDequeuedJobBudget(jobCtx context.Context) error {
	if err := jobCtx.Err(); err != nil {
		return fmt.Errorf("job context started canceled: %w", err)
	}

	deadline, ok := jobCtx.Deadline()
	if !ok || time.Until(deadline) < 100*time.Millisecond {
		return fmt.Errorf("job deadline was not created at dequeue: %v, %v", deadline, ok)
	}

	return nil
}

type finalizerCallState struct {
	outcome   workerpool.JobOutcome
	ctxErr    error
	remaining time.Duration
}

func captureFinalizerCallState(ctx context.Context, outcome workerpool.JobOutcome) finalizerCallState {
	state := finalizerCallState{outcome: outcome, ctxErr: ctx.Err(), remaining: -1}

	if deadline, ok := ctx.Deadline(); ok {
		state.remaining = time.Until(deadline)
	}

	return state
}
