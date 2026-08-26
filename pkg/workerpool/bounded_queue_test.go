package workerpool

import (
	"slices"
	"testing"
)

func TestBoundedQueuePreservesFIFOAcrossWrap(t *testing.T) {
	t.Parallel()

	queue := newBoundedQueue[int](3)

	pushQueueValues(t, &queue, 1, 2, 3)

	if queue.Push(4) {
		t.Fatal("Push() on a full queue = true")
	}

	assertQueuePops(t, &queue, 1, 2)
	pushQueueValues(t, &queue, 4, 5)

	want := []int{3, 4, 5}

	if got, ok := queue.Front(); !ok || got != want[0] {
		t.Fatalf("Front() = (%d, %t), want (%d, true)", got, ok, want[0])
	}

	for index, expected := range want {
		if got, ok := queue.At(index); !ok || got != expected {
			t.Fatalf("At(%d) = (%d, %t), want (%d, true)", index, got, ok, expected)
		}
	}

	if got := queue.Drain(); !slices.Equal(got, want) {
		t.Fatalf("Drain() = %v, want %v", got, want)
	}

	if queue.Len() != 0 {
		t.Fatalf("Len() after Drain = %d, want 0", queue.Len())
	}

	for index, value := range queue.items {
		if value != 0 {
			t.Fatalf("items[%d] after Drain = %d, want zero", index, value)
		}
	}
}

func TestBoundedQueueZeroCapacityRejectsPush(t *testing.T) {
	t.Parallel()

	queue := newBoundedQueue[string](0)
	if queue.Push("value") {
		t.Fatal("Push() on zero-capacity queue = true")
	}

	if value, ok := queue.Pop(); ok || value != "" {
		t.Fatalf("Pop() = (%q, %t), want zero/false", value, ok)
	}
}

func pushQueueValues(t *testing.T, queue *boundedQueue[int], values ...int) {
	t.Helper()

	for _, value := range values {
		if !queue.Push(value) {
			t.Fatalf("Push(%d) = false", value)
		}
	}
}

func assertQueuePops(t *testing.T, queue *boundedQueue[int], values ...int) {
	t.Helper()

	for _, want := range values {
		got, ok := queue.Pop()
		if !ok || got != want {
			t.Fatalf("Pop() = (%d, %t), want (%d, true)", got, ok, want)
		}
	}
}
