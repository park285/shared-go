package workercontract

import (
	"slices"
)

type WorkerShape struct {
	AttemptTimeout DurationMode
	Capacity       CapacityMode
	MaxAge         DurationMode
}

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
