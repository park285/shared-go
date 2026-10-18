package workercontract_test

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/park285/shared-go/v2/pkg/workercontract"
)

func TestMetricsUseExactVocabularyAndOmitFailedCurrentQueueValues(t *testing.T) {
	code := workercontract.QueueSnapshotFailed
	registry := buildChatbotRegistry(t, func() workercontract.QueueSnapshot {
		lastSuccess := time.Unix(90, 0).UnixMilli()

		return workercontract.QueueSnapshot{
			Status:               workercontract.QueueSnapshotUnavailable,
			LastSuccessAtEpochMS: &lastSuccess,
			ErrorCode:            &code,
		}
	})

	families, err := registry.Metrics(time.Unix(101, 0))
	if err != nil {
		t.Fatal(err)
	}

	if len(families) != 20 {
		t.Fatalf("families = %d, want 20", len(families))
	}

	var first, second bytes.Buffer

	if err := workercontract.WritePrometheus(&first, families); err != nil {
		t.Fatal(err)
	}

	if err := workercontract.WritePrometheus(&second, families); err != nil {
		t.Fatal(err)
	}

	if first.String() != second.String() {
		t.Fatal("exposition is not deterministic")
	}

	text := first.String()
	if strings.Contains(text, `iris_stack_worker_queue_depth{queue_backend="postgres"`) {
		t.Fatal("failed PostgreSQL snapshot exported current depth")
	}

	for _, want := range []string{
		`queue_backend="postgres"`,
		`queue_scope="shared"`,
		`runtime="go"`,
		`result="outcome_unknown"`,
		`outcome="panic"`,
		`reason="shutdown"`,
		"iris_stack_worker_queue_snapshot_success",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("exposition missing %q", want)
		}
	}
}

func TestMetricsOmitStaleQueueValuesAndRecover(t *testing.T) {
	sampledAt := time.Unix(1000, 0)
	snapshot := workercontract.CurrentQueueSnapshot(5, 3*time.Second, sampledAt)
	registry := buildChatbotRegistry(t, func() workercontract.QueueSnapshot { return snapshot })

	for _, tc := range []struct {
		name       string
		observedAt time.Time
		refresh    bool
		current    bool
	}{
		{"at boundary", sampledAt.Add(32 * time.Second), false, true},
		{"stale", sampledAt.Add(40 * time.Second), false, false},
		{"recovered", sampledAt.Add(41 * time.Second), true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.refresh {
				snapshot = workercontract.CurrentQueueSnapshot(5, 3*time.Second, tc.observedAt)
			}

			families, err := registry.Metrics(tc.observedAt)
			require.NoError(t, err)

			values := make(map[string]float64)

			for _, family := range families {
				for _, sample := range family.Samples {
					if sample.Labels["worker"] == "webhook_inbox" {
						values[family.Name] = sample.Value
					}
				}
			}

			for name, want := range map[string]float64{
				"iris_stack_worker_queue_depth":               5,
				"iris_stack_worker_oldest_queued_age_seconds": 3,
			} {
				value, exists := values[name]
				require.Equal(t, tc.current, exists, name)

				if exists {
					require.InDelta(t, want, value, 0, name)
				}
			}

			wantSuccess := float64(0)

			if tc.current {
				wantSuccess = 1
			}

			require.Contains(t, values, "iris_stack_worker_queue_snapshot_success")
			require.InDelta(t, wantSuccess, values["iris_stack_worker_queue_snapshot_success"], 0)

			wantLastSuccess := float64(*snapshot.LastSuccessAtEpochMS) / 1000

			require.Contains(t, values, "iris_stack_worker_queue_snapshot_last_success_timestamp_seconds")
			require.InDelta(t, wantLastSuccess, values["iris_stack_worker_queue_snapshot_last_success_timestamp_seconds"], 0)
			require.Contains(t, values, "iris_stack_worker_in_flight")
		})
	}
}
