package workerpool_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/park285/shared-go/pkg/workerpool"
)

func TestManagedPoolStuckFinalizerPermanentlyBlocksAdmission(t *testing.T) {
	release := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(unblock)

	pool := workerpool.NewManaged(workerpool.ManagedConfig{
		Workers:             1,
		QueueSize:           1,
		FinalizeConcurrency: 1,
		FinalizeQueueSize:   1,
		FinalizeTimeout:     20 * time.Millisecond,
	})

	if !pool.TrySubmit(workerpool.JobSpec{
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
	if !pool.TrySubmit(workerpool.JobSpec{Run: func(context.Context) { close(runCompleted) }}) {
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
	pool := workerpool.NewManaged(workerpool.ManagedConfig{Workers: 1, QueueSize: 1})

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
