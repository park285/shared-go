package workerpool

import (
	"fmt"
	"testing"
)

const benchmarkQueueCycles = 100

var benchmarkManagedJobSink *managedJob

func BenchmarkManagedQueueSteadyState(b *testing.B) {
	for _, capacity := range []int{64, 1024, 65536} {
		b.Run(fmt.Sprintf("capacity-%d", capacity), func(b *testing.B) {
			b.Run("slice_fifo", func(b *testing.B) {
				benchmarkSliceFIFO(b, capacity)
			})
			b.Run("bounded_ring_fifo", func(b *testing.B) {
				benchmarkRingFIFO(b, capacity)
			})
		})
	}
}

func benchmarkSliceFIFO(b *testing.B, capacity int) {
	b.Helper()

	task := new(managedJob)
	queue := make([]*managedJob, capacity)

	for index := range queue {
		queue[index] = task
	}

	rotations := capacity * benchmarkQueueCycles

	b.ReportAllocs()
	b.ReportMetric(float64(rotations), "rotations/op")

	for b.Loop() {
		for range rotations {
			current := queue[0]

			queue[0] = nil
			queue = queue[1:]
			queue = append(queue, current) //nolint:makezero // 비교군은 기존 slice FIFO의 front-pop/append 비용을 그대로 측정한다.
		}
	}

	benchmarkManagedJobSink = queue[0]
}

func benchmarkRingFIFO(b *testing.B, capacity int) {
	b.Helper()

	task := new(managedJob)
	queue := newBoundedQueue[*managedJob](capacity)

	for range capacity {
		queue.Push(task)
	}

	rotations := capacity * benchmarkQueueCycles

	b.ReportAllocs()
	b.ReportMetric(float64(rotations), "rotations/op")

	for b.Loop() {
		for range rotations {
			current, _ := queue.Pop()
			queue.Push(current)
		}
	}

	benchmarkManagedJobSink, _ = queue.Front()
}
