package workercontract

import (
	"slices"
	"testing"
)

func TestShapeProblemsReportsMismatchesInWorkerOrder(t *testing.T) {
	fixed := DurationPolicy{Mode: DurationModeFixed}
	workers := map[string]WorkerProfile{
		"zeta":  {Executor: ExecutorProfile{AttemptTimeout: fixed}, Queue: QueueProfile{Capacity: CapacityPolicy{Mode: CapacityModeUnbounded}, MaxAge: fixed}},
		"alpha": {Executor: ExecutorProfile{AttemptTimeout: DurationPolicy{Mode: DurationModePerJob}}, Queue: QueueProfile{Capacity: CapacityPolicy{Mode: CapacityModeBounded}, MaxAge: fixed}},
	}
	shapes := map[string]WorkerShape{
		"zeta":    {AttemptTimeout: DurationModeFixed, Capacity: CapacityModeUnbounded, MaxAge: DurationModeFixed},
		"alpha":   {AttemptTimeout: DurationModeFixed, Capacity: CapacityModeUnbounded, MaxAge: DurationModeFixed},
		"missing": {AttemptTimeout: DurationModeFixed, Capacity: CapacityModeBounded, MaxAge: DurationModeFixed},
	}

	got := ShapeProblems(workers, shapes)
	want := []string{
		"alpha attempt_timeout mode mismatch",
		"alpha capacity mode mismatch",
		"missing attempt_timeout mode mismatch",
		"missing capacity mode mismatch",
		"missing max_age mode mismatch",
	}

	if !slices.Equal(got, want) {
		t.Fatalf("ShapeProblems() = %v, want %v", got, want)
	}
}
