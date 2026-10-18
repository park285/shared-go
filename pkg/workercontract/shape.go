package workercontract

import (
	"slices"
)

// WorkerShape는 worker 하나의 attempt timeout·queue capacity·max age mode 기대값이다.
type WorkerShape struct {
	AttemptTimeout DurationMode
	Capacity       CapacityMode
	MaxAge         DurationMode
}

// ShapeProblems는 worker/shape 키와 mode 불일치를 worker ID·문제 순으로 돌려준다.
func ShapeProblems(workers map[string]WorkerProfile, shapes map[string]WorkerShape) []string {
	problems := make([]string, 0)

	for workerID, shape := range shapes {
		worker, ok := workers[workerID]
		if !ok {
			problems = append(problems, workerID+" worker is missing")
			continue
		}

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

	for workerID := range workers {
		if _, ok := shapes[workerID]; !ok {
			problems = append(problems, workerID+" shape is missing")
		}
	}

	slices.Sort(problems)

	return problems
}
