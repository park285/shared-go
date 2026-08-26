package workerpool

type boundedQueue[T any] struct {
	items []T
	head  int
	size  int
}

func newBoundedQueue[T any](capacity int) boundedQueue[T] {
	return boundedQueue[T]{items: make([]T, max(capacity, 0))}
}

func (q *boundedQueue[T]) Len() int {
	return q.size
}

func (q *boundedQueue[T]) Push(value T) bool {
	if q.size == len(q.items) {
		return false
	}

	index := (q.head + q.size) % len(q.items)

	q.items[index] = value
	q.size++

	return true
}

func (q *boundedQueue[T]) Pop() (T, bool) {
	var zero T

	if q.size == 0 {
		return zero, false
	}

	value := q.items[q.head]

	q.items[q.head] = zero
	q.head = (q.head + 1) % len(q.items)
	q.size--

	return value, true
}

func (q *boundedQueue[T]) Front() (T, bool) {
	var zero T

	if q.size == 0 {
		return zero, false
	}

	return q.items[q.head], true
}

func (q *boundedQueue[T]) At(index int) (T, bool) {
	var zero T

	if index < 0 || index >= q.size {
		return zero, false
	}

	return q.items[(q.head+index)%len(q.items)], true
}

func (q *boundedQueue[T]) Drain() []T {
	if q.size == 0 {
		return nil
	}

	values := make([]T, q.size)
	for index := range values {
		values[index], _ = q.Pop()
	}

	return values
}
