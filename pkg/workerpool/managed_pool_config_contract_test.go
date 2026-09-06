package workerpool_test

import (
	"strings"
	"testing"
	"time"

	"github.com/park285/shared-go/v2/pkg/workerpool"
)

func TestManagedPoolRequiresExplicitFinalizerCapacity(t *testing.T) {
	cases := []struct {
		name     string
		capacity int
		accepted bool
	}{
		{name: "negative", capacity: -1},
		{name: "zero has no default", capacity: 0},
		{name: "below concurrency", capacity: 1},
		{name: "minimum", capacity: 2, accepted: true},
		{name: "not derived from workers and queue", capacity: 3, accepted: true},
		{name: "below maximum", capacity: 1_048_575, accepted: true},
		{name: "maximum", capacity: 1_048_576, accepted: true},
		{name: "above maximum", capacity: 1_048_577},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pool, err := workerpool.NewManagedPool(workerpool.ManagedConfig{
				Workers:             1,
				QueueSize:           7,
				FinalizeTimeout:     time.Second,
				FinalizeConcurrency: 2,
				FinalizeQueueSize:   tc.capacity,
			})
			if pool != nil {
				closeManagedPoolOnCleanup(t, pool)
			}

			if !tc.accepted {
				if err == nil || pool != nil {
					t.Fatalf("NewManagedPool(capacity=%d) = (%v, %v), want nil pool and error", tc.capacity, pool, err)
				}

				if !strings.Contains(err.Error(), "finalize queue size") {
					t.Fatalf("NewManagedPool(capacity=%d) error = %v, want finalizer capacity error", tc.capacity, err)
				}

				return
			}

			if err != nil || pool == nil {
				t.Fatalf("NewManagedPool(capacity=%d) = (%v, %v), want explicit capacity accepted", tc.capacity, pool, err)
			}

			snapshot := pool.Snapshot().Finalizer
			if snapshot.QueueSize != tc.capacity || snapshot.Concurrency != 2 || snapshot.Reservations != 0 {
				t.Fatalf("finalizer snapshot = %+v, want capacity %d, concurrency 2, and no reservations", snapshot, tc.capacity)
			}
		})
	}
}
