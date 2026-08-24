package workercontract_test

import (
	"bytes"
	"strings"
	"testing"
	"time"

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
