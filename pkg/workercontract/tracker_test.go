package workercontract_test

import (
	"testing"
	"time"

	"github.com/park285/shared-go/v2/pkg/workercontract"
)

func TestExecutorTrackerReportsActualWorkersAndOldestAttempt(t *testing.T) {
	tracker := workercontract.NewExecutorTracker()
	if !tracker.StartWorkers(2) {
		t.Fatal("StartWorkers() = false")
	}
	now := time.Now()
	first := tracker.BeginAttempt(now.Add(-2 * time.Second))
	second := tracker.BeginAttempt(now.Add(-time.Second))
	snapshot := tracker.Snapshot(now)
	if snapshot.RunningWorkers != 2 || snapshot.InFlight != 2 || snapshot.OldestInFlightAgeMS < 2000 {
		t.Fatalf("Snapshot() = %#v", snapshot)
	}
	if !tracker.EndAttempt(first) || !tracker.EndAttempt(second) || !tracker.StopWorkers(2) {
		t.Fatal("tracker teardown failed")
	}
}
