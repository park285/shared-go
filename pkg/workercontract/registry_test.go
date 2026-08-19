package workercontract_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/park285/shared-go/pkg/workercontract"
)

func buildChatbotRegistry(t *testing.T, webhookQueue func() workercontract.QueueSnapshot) *workercontract.Registry {
	t.Helper()
	loaded, err := workercontract.LoadProfileFile(validProfilePath(), chatbotIdentity(t))
	if err != nil {
		t.Fatal(err)
	}
	registry := workercontract.NewRegistry(loaded, workercontract.NewProfileFileChecker(loaded, time.Unix(100, 0)))
	for _, workerID := range chatbotIdentity(t).WorkerIDs {
		backend := workercontract.QueueMemory
		scope := workercontract.QueueScopeProcess
		queue := func() workercontract.QueueSnapshot {
			return workercontract.CurrentQueueSnapshot(0, 0, time.Unix(100, 0))
		}
		if workerID == "webhook_inbox" {
			backend = workercontract.QueuePostgres
			scope = workercontract.QueueScopeShared
			queue = webhookQueue
		}
		profile, _ := registry.WorkerProfile(workerID)
		registration := workercontract.Registration{
			WorkerID:                workerID,
			Runtime:                 workercontract.RuntimeGo,
			QueueBackend:            backend,
			QueueScope:              scope,
			SettingsValidated:       true,
			PerJobDeadlineValidated: profile.Executor.AttemptTimeout.Mode != workercontract.DurationModePerJob || workerID == "command",
			ExecutorSnapshot: func() workercontract.ExecutorSnapshot {
				return workercontract.ExecutorSnapshot{RunningWorkers: 1}
			},
			QueueSnapshot: queue,
			Counters:      &workercontract.Counters{},
		}
		if err := registry.Register(registration); err != nil {
			t.Fatalf("Register(%s) error = %v", workerID, err)
		}
	}
	if err := registry.Seal(); err != nil {
		t.Fatalf("Seal() error = %v", err)
	}
	return registry
}

func TestRegistrySealsOnlyExactWorkerSet(t *testing.T) {
	loaded, err := workercontract.LoadProfileFile(validProfilePath(), chatbotIdentity(t))
	if err != nil {
		t.Fatal(err)
	}
	registry := workercontract.NewRegistry(loaded, workercontract.NewProfileFileChecker(loaded, time.Now()))
	if err := registry.Seal(); err == nil {
		t.Fatal("Seal() error = nil")
	}
	if err := registry.Register(workercontract.Registration{WorkerID: "unknown", SettingsValidated: true}); err == nil {
		t.Fatal("Register(unknown) error = nil")
	}
}

func TestDiagnosticsHandlerReturnsValid503EnvelopeOnSnapshotFailure(t *testing.T) {
	code := workercontract.QueueSnapshotFailed
	registry := buildChatbotRegistry(t, func() workercontract.QueueSnapshot {
		return workercontract.QueueSnapshot{Status: workercontract.QueueSnapshotUnavailable, ErrorCode: &code}
	})
	recorder := httptest.NewRecorder()
	registry.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/diagnostics/workers", nil))
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", recorder.Code)
	}
	envelope, err := registry.Diagnostics(time.Unix(101, 0))
	if err != nil {
		t.Fatal(err)
	}
	queue := envelope.Workers["webhook_inbox"].Queue
	if envelope.Complete || queue.Depth != nil || queue.OldestQueuedAgeMS != nil || queue.ErrorCode == nil {
		t.Fatalf("envelope = %+v", envelope)
	}
}
