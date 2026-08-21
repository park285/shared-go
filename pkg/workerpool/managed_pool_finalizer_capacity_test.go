package workerpool_test

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/park285/shared-go/pkg/workerpool"
)

func TestManagedPoolStuckFinalizerPermanentlyBlocksAdmission(t *testing.T) {
	release := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(unblock)

	pool := newManagedPoolForTest(t, workerpool.ManagedConfig{
		Workers:             1,
		QueueSize:           1,
		FinalizeConcurrency: 1,
		FinalizeQueueSize:   1,
		FinalizeTimeout:     20 * time.Millisecond,
	})

	if !trySubmit(pool, workerpool.JobSpec{
		Run:      func(context.Context) {},
		Finalize: func(context.Context, workerpool.JobOutcome) { <-release },
	}) {
		t.Fatal("TrySubmit(stuck finalizer) = false")
	}

	awaitManagedSnapshot(t, pool, "stuck finalizer overdue", func(snapshot workerpool.ManagedSnapshot) bool {
		return snapshot.Finalizer.TimedOut == 1 &&
			snapshot.Finalizer.OverdueInFlight == 1 &&
			snapshot.Finalizer.InFlight == 1 &&
			snapshot.Finalizer.Reservations == 1
	})

	for attempt := range 3 {
		result := pool.TrySubmitResult(workerpool.JobSpec{
			Run:      func(context.Context) {},
			Finalize: func(context.Context, workerpool.JobOutcome) {},
		})
		if result.Accepted || result.FinalizerClaimed ||
			result.Reason != workerpool.ManagedSubmitRejectedFinalizerCapacity {
			t.Fatalf("TrySubmitResult(attempt %d) = %+v, want unclaimed capacity rejection", attempt, result)
		}
		time.Sleep(25 * time.Millisecond)
	}

	blocked := pool.Snapshot()
	if blocked.Finalizer.OverdueInFlight != 1 || blocked.Finalizer.Reservations != 1 {
		t.Fatalf("finalizer snapshot = %+v, want the overdue slot still holding its reservation", blocked.Finalizer)
	}
	runCompleted := make(chan struct{})
	if !trySubmit(pool, workerpool.JobSpec{Run: func(context.Context) { close(runCompleted) }}) {
		t.Fatal("TrySubmit(no finalizer) = false, want admission unaffected without a finalizer")
	}
	awaitClosed(t, runCompleted, "run-only job")

	unblock()

	awaitManagedSnapshot(t, pool, "finalizer capacity returned", func(snapshot workerpool.ManagedSnapshot) bool {
		return snapshot.Finalizer.OverdueInFlight == 0 &&
			snapshot.Finalizer.InFlight == 0 &&
			snapshot.Finalizer.Reservations == 0
	})
	if result := pool.TrySubmitResult(workerpool.JobSpec{
		Run:      func(context.Context) {},
		Finalize: func(context.Context, workerpool.JobOutcome) {},
	}); !result.Accepted || !result.FinalizerClaimed {
		t.Fatalf("TrySubmitResult(after release) = %+v, want accepted finalizer claim", result)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := pool.CloseContext(ctx); err != nil {
		t.Fatalf("CloseContext() error = %v", err)
	}
}

func TestManagedPoolReportsClosedFinalizerRejectionSeparately(t *testing.T) {
	pool := newManagedPoolForTest(t, workerpool.ManagedConfig{Workers: 1, QueueSize: 1})

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := pool.CloseContext(ctx); err != nil {
		t.Fatalf("CloseContext() error = %v", err)
	}

	finalized := make(chan struct{}, 1)
	result := pool.TrySubmitResult(workerpool.JobSpec{
		Run: func(context.Context) {},
		Finalize: func(context.Context, workerpool.JobOutcome) {
			finalized <- struct{}{}
		},
	})
	if result.Accepted || result.FinalizerClaimed ||
		result.Reason != workerpool.ManagedSubmitRejectedFinalizerClosed {
		t.Fatalf("TrySubmitResult(after close) = %+v, want unclaimed closed rejection", result)
	}
	select {
	case <-finalized:
		t.Fatal("Finalize ran for a rejection the pool did not claim")
	case <-time.After(50 * time.Millisecond):
	}

	if plain := pool.TrySubmitResult(workerpool.JobSpec{Run: func(context.Context) {}}); plain.Accepted ||
		plain.Reason != workerpool.ManagedSubmitRejected {
		t.Fatalf("TrySubmitResult(no finalizer, after close) = %+v, want plain rejection", plain)
	}
}

func TestManagedPoolRejectsShutdownWindowSubmissionsAsClosed(t *testing.T) {
	release := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(unblock)

	pool := newManagedPoolForTest(t, workerpool.ManagedConfig{
		Workers:             1,
		QueueSize:           2,
		FinalizeConcurrency: 1,
		FinalizeQueueSize:   2,
		FinalizeTimeout:     5 * time.Second,
	})

	running := make(chan struct{})
	if !trySubmit(pool, workerpool.JobSpec{
		Run: func(context.Context) {
			close(running)
			<-release
		},
	}) {
		t.Fatal("TrySubmit(worker blocker) = false")
	}
	awaitClosed(t, running, "worker blocker start")

	finalizing := make(chan struct{})
	if !trySubmit(pool, workerpool.JobSpec{
		Run: func(context.Context) {},
		Finalize: func(context.Context, workerpool.JobOutcome) {
			close(finalizing)
			<-release
		},
	}) {
		t.Fatal("TrySubmit(queued job) = false")
	}

	closeErr := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		closeErr <- pool.CloseContext(ctx)
	}()
	awaitClosed(t, finalizing, "shutdown finalize of queued job")

	finalized := make(chan struct{}, 16)
	for attempt := range 5 {
		result := pool.TrySubmitResult(workerpool.JobSpec{
			Run: func(context.Context) {},
			Finalize: func(context.Context, workerpool.JobOutcome) {
				finalized <- struct{}{}
			},
		})
		if result.Accepted || result.FinalizerClaimed ||
			result.Reason != workerpool.ManagedSubmitRejectedFinalizerClosed {
			t.Fatalf("TrySubmitResult(shutdown window, attempt %d) = %+v, want unclaimed closed rejection", attempt, result)
		}
	}

	if plain := pool.TrySubmitResult(workerpool.JobSpec{Run: func(context.Context) {}}); plain.Accepted ||
		plain.FinalizerClaimed || plain.Reason != workerpool.ManagedSubmitRejected {
		t.Fatalf("TrySubmitResult(no finalizer, shutdown window) = %+v, want plain rejection", plain)
	}

	during := pool.Snapshot()
	if during.Finalizer.Claimed != 1 || during.Finalizer.Reservations != 1 ||
		during.Finalizer.Overloaded != 0 || during.Finalizer.ReservationRejected != 0 {
		t.Fatalf("finalizer snapshot = %+v, want shutdown-window submissions to reserve nothing", during.Finalizer)
	}

	unblock()
	if err := awaitValue(t, closeErr, "CloseContext"); err != nil {
		t.Fatalf("CloseContext() error = %v", err)
	}
	select {
	case <-finalized:
		t.Fatal("Finalize ran for a rejection the pool did not claim")
	default:
	}

	after := pool.Snapshot()
	if !after.Finalizer.Quiesced || after.Finalizer.Reservations != 0 {
		t.Fatalf("finalizer snapshot after close = %+v, want quiesced with no reservation held", after.Finalizer)
	}
}

func TestManagedPoolConcurrentShutdownKeepsFinalizerOwnershipBalanced(t *testing.T) {
	pool := newManagedPoolForTest(t, workerpool.ManagedConfig{
		Workers:             2,
		QueueSize:           4,
		FinalizeConcurrency: 2,
		FinalizeQueueSize:   4,
		FinalizeTimeout:     time.Second,
		Logger:              slog.New(slog.DiscardHandler),
	})

	var claimed, finalized atomic.Int64
	stop := make(chan struct{})
	var submitters sync.WaitGroup
	for range 8 {
		submitters.Add(1)
		go func() {
			defer submitters.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				result := pool.TrySubmitResult(workerpool.JobSpec{
					Run: func(context.Context) {},
					Finalize: func(context.Context, workerpool.JobOutcome) {
						finalized.Add(1)
					},
				})
				if result.FinalizerClaimed {
					claimed.Add(1)
				}
			}
		}()
	}

	time.Sleep(10 * time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := pool.CloseContext(ctx); err != nil {
		t.Fatalf("CloseContext() error = %v", err)
	}
	close(stop)
	submitters.Wait()

	awaitManagedSnapshot(t, pool, "finalizer ownership balanced", func(snapshot workerpool.ManagedSnapshot) bool {
		return snapshot.Finalizer.Reservations == 0 && claimed.Load() == finalized.Load()
	})
	if result := pool.TrySubmitResult(workerpool.JobSpec{
		Run:      func(context.Context) {},
		Finalize: func(context.Context, workerpool.JobOutcome) {},
	}); result.Reason != workerpool.ManagedSubmitRejectedFinalizerClosed {
		t.Fatalf("TrySubmitResult(after concurrent close) = %+v, want closed rejection", result)
	}
}
