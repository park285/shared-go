package obsmetrics

import (
	"math"
	"sync"
	"testing"
)

var testLatencyBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120}

func TestHistogramSnapshotCumulative(t *testing.T) {
	t.Parallel()

	hist := NewHistogram([]float64{0.1, 1, 5})

	for _, v := range []float64{0.05, 0.2, 2, 7} {
		hist.Observe(v)
	}

	snap := hist.Snapshot()
	assertUint64Slice(t, snap.Cumulative, []uint64{1, 2, 3})

	if snap.Total != 4 {
		t.Fatalf("Total = %d, want 4", snap.Total)
	}

	if math.Abs(snap.Sum-9.25) > 1e-12 {
		t.Fatalf("Sum = %f, want 9.25", snap.Sum)
	}
}

func TestHistogramConcurrentObserveSnapshotInvariants(t *testing.T) {
	t.Parallel()

	hist := NewHistogram(testLatencyBuckets)

	const (
		workers    = 16
		iterations = 1000
	)

	done := make(chan struct{})
	snapshotErr := make(chan string, 1)

	go func() {
		for {
			select {
			case <-done:
				return
			default:
			}

			snap := hist.Snapshot()
			if err := validateHistogramSnapshotInvariants(snap); err != "" {
				select {
				case snapshotErr <- err:
				default:
				}

				return
			}
		}
	}()

	var wg sync.WaitGroup

	wg.Add(workers)

	for worker := range workers {
		go func(worker int) {
			defer wg.Done()

			for i := range iterations {
				hist.Observe(testLatencyBuckets[(worker+i)%len(testLatencyBuckets)])
			}
		}(worker)
	}

	wg.Wait()
	close(done)

	select {
	case err := <-snapshotErr:
		t.Fatal(err)
	default:
	}

	snap := hist.Snapshot()
	if err := validateHistogramSnapshotInvariants(snap); err != "" {
		t.Fatal(err)
	}

	if snap.Total != workers*iterations {
		t.Fatalf("Total = %d, want %d", snap.Total, workers*iterations)
	}

	if got := snap.Cumulative[len(snap.Cumulative)-1]; got != snap.Total {
		t.Fatalf("last cumulative bucket = %d, want total %d", got, snap.Total)
	}
}

func validateHistogramSnapshotInvariants(snap HistogramSnapshot) string {
	for i, v := range snap.Cumulative {
		if v > snap.Total {
			return "cumulative bucket is greater than total"
		}

		if i > 0 && v < snap.Cumulative[i-1] {
			return "cumulative buckets are not monotonic"
		}
	}

	return ""
}

func assertUint64Slice(t *testing.T, got, want []uint64) {
	t.Helper()

	if len(got) != len(want) {
		t.Fatalf("len(got) = %d, want %d; got=%v", len(got), len(want), got)
	}

	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("got[%d] = %d, want %d; got=%v", i, got[i], want[i], got)
		}
	}
}

func BenchmarkHistogramObserveConcurrent(b *testing.B) {
	hist := NewHistogram(testLatencyBuckets)

	b.ReportAllocs()
	b.SetParallelism(16)
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			hist.Observe(0.025)
		}
	})
}
