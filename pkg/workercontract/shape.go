package workercontract

import (
	"maps"
	"slices"
)

// WorkerShape는 worker 하나의 attempt timeout·queue capacity·max age mode 기대값이다.
type WorkerShape struct {
	AttemptTimeout DurationMode
	Capacity       CapacityMode
	MaxAge         DurationMode
}

// ShapeProblems는 기대 shape와 다른 worker의 문제를 worker ID 순으로 돌려준다.
// Profile에 없는 worker는 zero profile로 비교되므로 mode mismatch로 드러난다.
func ShapeProblems(workers map[string]WorkerProfile, shapes map[string]WorkerShape) []string {
	problems := make([]string, 0)

	for _, workerID := range slices.Sorted(maps.Keys(shapes)) {
		worker := workers[workerID]
		shape := shapes[workerID]

		if worker.Executor.AttemptTimeout.Mode != shape.AttemptTimeout {
			problems = append(problems, workerID+" attempt_timeout mode mismatch")
		}

		if worker.Queue.Capacity.Mode != shape.Capacity {
			problems = append(problems, workerID+" capacity mode mismatch")
		}

		if worker.Queue.MaxAge.Mode != shape.MaxAge {
			problems = append(problems, workerID+" max_age mode mismatch")
		}
	}

	return problems
}
