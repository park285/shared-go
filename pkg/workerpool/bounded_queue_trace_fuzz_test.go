package workerpool

import "testing"

func FuzzBoundedQueueFIFO(f *testing.F) {
	f.Add(int8(-1), []byte{1, 0})
	f.Add(int8(0), []byte{0, 1})
	f.Add(int8(1), []byte{1, 3, 0, 3, 0, 1, 0, 0})
	f.Add(int8(3), []byte{1, 3, 1, 1, 0, 0, 3, 1, 0, 0, 1, 1, 0})
	f.Fuzz(func(t *testing.T, capacity int8, actions []byte) {
		trace := boundedQueueTrace{
			t:     t,
			queue: newBoundedQueue[int](int(capacity)),
			limit: max(0, int(capacity)),
		}

		for step, action := range actions {
			trace.apply(step, action)
		}

		trace.requireFinalOrder()
	})
}

type boundedQueueTrace struct {
	t     *testing.T
	queue boundedQueue[int]
	limit int
	fifo  []int
}

func (trace *boundedQueueTrace) apply(step int, action byte) {
	trace.t.Helper()

	if action&1 != 0 {
		trace.push(step, pushedValue(step, action))
	} else {
		trace.pop(step)
	}

	if got := trace.queue.Len(); got != len(trace.fifo) {
		trace.t.Fatalf("step %d: Len() = %d, want %d", step, got, len(trace.fifo))
	}
}

func pushedValue(step int, action byte) int {
	if action&2 != 0 {
		return 0
	}

	return step + 1
}

func (trace *boundedQueueTrace) push(step, value int) {
	trace.t.Helper()

	want := len(trace.fifo) < trace.limit
	if got := trace.queue.Push(value); got != want {
		trace.t.Fatalf("step %d: Push(%d) = %t, want %t", step, value, got, want)
	}

	if want {
		trace.fifo = append(trace.fifo, value)
	}
}

func (trace *boundedQueueTrace) pop(step int) {
	trace.t.Helper()

	wantOK := len(trace.fifo) != 0

	var want int

	if wantOK {
		want, trace.fifo = trace.fifo[0], trace.fifo[1:]
	}

	if got, ok := trace.queue.Pop(); got != want || ok != wantOK {
		trace.t.Fatalf("step %d: Pop() = (%d, %t), want (%d, %t)", step, got, ok, want, wantOK)
	}
}

func (trace *boundedQueueTrace) requireFinalOrder() {
	trace.t.Helper()

	for index, want := range trace.fifo {
		if got, ok := trace.queue.At(index); !ok || got != want {
			trace.t.Fatalf("final At(%d) = (%d, %t), want (%d, true)", index, got, ok, want)
		}
	}
}
