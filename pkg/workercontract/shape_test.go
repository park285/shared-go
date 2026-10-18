package workercontract

import (
	"maps"
	"slices"
	"strings"
	"testing"
)

func TestShapeProblemsChecksWorkerSetsAndModes(t *testing.T) {
	const (
		inboxID  = "inbox"
		outboxID = "outbox"
	)

	fixed := DurationPolicy{Mode: DurationModeFixed}
	validWorker := WorkerProfile{
		Executor: ExecutorProfile{AttemptTimeout: fixed},
		Queue:    QueueProfile{Capacity: CapacityPolicy{Mode: CapacityModeUnbounded}, MaxAge: fixed},
	}
	validShape := WorkerShape{
		AttemptTimeout: DurationModeFixed, Capacity: CapacityModeUnbounded, MaxAge: DurationModeFixed,
	}

	for _, tc := range []struct {
		name     string
		workers  map[string]WorkerProfile
		shapes   map[string]WorkerShape
		rejected map[string]int
	}{
		{
			name:     "exact valid set",
			workers:  map[string]WorkerProfile{inboxID: validWorker},
			shapes:   map[string]WorkerShape{inboxID: validShape},
			rejected: map[string]int{},
		},
		{
			name:     "unvalidated worker",
			workers:  map[string]WorkerProfile{inboxID: validWorker, outboxID: validWorker},
			shapes:   map[string]WorkerShape{inboxID: validShape},
			rejected: map[string]int{outboxID: 1},
		},
		{
			name:     "missing worker",
			workers:  map[string]WorkerProfile{inboxID: validWorker},
			shapes:   map[string]WorkerShape{inboxID: validShape, outboxID: validShape},
			rejected: map[string]int{outboxID: 1},
		},
		{
			name:     "matching lengths with different workers",
			workers:  map[string]WorkerProfile{inboxID: validWorker},
			shapes:   map[string]WorkerShape{outboxID: validShape},
			rejected: map[string]int{inboxID: 1, outboxID: 1},
		},
		{
			name:    "independent mode violations",
			workers: map[string]WorkerProfile{inboxID: validWorker},
			shapes: map[string]WorkerShape{inboxID: {
				AttemptTimeout: DurationModePerJob, Capacity: CapacityModeBounded, MaxAge: DurationModeNone,
			}},
			rejected: map[string]int{inboxID: 3},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			problems := ShapeProblems(tc.workers, tc.shapes)
			if !slices.IsSorted(problems) {
				t.Fatal("shape problems are nondeterministic")
			}

			actual := make(map[string]int)

			for _, problem := range problems {
				id, _, _ := strings.Cut(problem, " ")
				actual[id]++
			}

			if !maps.Equal(actual, tc.rejected) {
				t.Fatalf("rejected workers = %v, want %v", actual, tc.rejected)
			}
		})
	}
}
