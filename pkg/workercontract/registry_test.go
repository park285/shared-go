package workercontract_test

import (
	"context"
	jsonv2 "encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/park285/shared-go/v2/pkg/workercontract"
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
			return workercontract.CurrentQueueSnapshot(0, 0, time.Now())
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

func TestDiagnosticsEnvelopeCanonicalFixtureRoundTrip(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(validProfilePath()), "valid-envelope-chatbotgo.json"))
	require.NoError(t, err)

	var envelope workercontract.DiagnosticsEnvelope

	require.NoError(t, jsonv2.Unmarshal(raw, &envelope, jsonv2.RejectUnknownMembers(true)))
	require.False(t, envelope.Complete)
	require.Equal(t, workercontract.QueueSnapshotUnavailable, envelope.Workers["webhook_inbox"].Queue.SnapshotStatus)
	require.Nil(t, envelope.Workers["webhook_inbox"].Queue.Depth)
	require.Nil(t, envelope.Workers["draw"].Executor.RunningWorkers)
	require.Nil(t, envelope.Workers["draw"].Executor.InFlight)
	require.Nil(t, envelope.Workers["draw"].Executor.OldestInFlightAgeMS)
	require.NotNil(t, envelope.Workers["command"].Executor.RunningWorkers)

	encoded, err := jsonv2.Marshal(envelope)
	require.NoError(t, err)

	var decoded workercontract.DiagnosticsEnvelope

	require.NoError(t, jsonv2.Unmarshal(encoded, &decoded, jsonv2.RejectUnknownMembers(true)))
	require.Equal(t, envelope, decoded)
}

func TestDiagnosticsHandlerReturnsValid503EnvelopeOnSnapshotFailure(t *testing.T) {
	code := workercontract.QueueSnapshotFailed
	registry := buildChatbotRegistry(t, func() workercontract.QueueSnapshot {
		return workercontract.QueueSnapshot{Status: workercontract.QueueSnapshotUnavailable, ErrorCode: &code}
	})
	recorder := httptest.NewRecorder()
	registry.Handler().ServeHTTP(recorder, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/diagnostics/workers", http.NoBody))

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

func TestRegistryQueueSnapshotStalenessBoundaryAndRecovery(t *testing.T) {
	sampledAt := time.Unix(1000, 0)
	queries := 0
	sampler := workercontract.NewQueueSampler(func(context.Context) (workercontract.QueueValues, error) {
		queries++
		return workercontract.QueueValues{Depth: 5, OldestQueuedAge: 3 * time.Second}, nil
	})
	sampler.Sample(t.Context(), sampledAt)

	registry := buildChatbotRegistry(t, sampler.Latest)

	for _, tc := range []struct {
		name  string
		age   time.Duration
		stale bool
	}{
		{"before boundary", 32*time.Second - time.Nanosecond, false},
		{"exact boundary", 32 * time.Second, false},
		{"after boundary", 32*time.Second + time.Nanosecond, true},
		{"stopped sampler", 40 * time.Second, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			envelope, err := registry.Diagnostics(sampledAt.Add(tc.age))
			require.NoError(t, err)

			queue := envelope.Workers["webhook_inbox"].Queue
			require.Equal(t, !tc.stale, envelope.Complete)
			require.Equal(t, new(sampledAt.UnixMilli()), queue.LastSuccessAtEpochMS)

			if tc.stale {
				require.Equal(t, workercontract.QueueSnapshotUnavailable, queue.SnapshotStatus)
				require.Nil(t, queue.Depth)
				require.Nil(t, queue.OldestQueuedAgeMS)
				require.Equal(t, new(workercontract.QueueSnapshotStale), queue.ErrorCode)
			} else {
				require.Equal(t, workercontract.QueueSnapshotCurrent, queue.SnapshotStatus)
				require.Equal(t, new(int64(5)), queue.Depth)
				require.Equal(t, new(int64(3000)), queue.OldestQueuedAgeMS)
				require.Nil(t, queue.ErrorCode)
			}
		})
	}

	require.Equal(t, 1, queries)
	require.Equal(t, workercontract.QueueSnapshotCurrent, sampler.Latest().Status)

	refreshedAt := sampledAt.Add(41 * time.Second)
	sampler.Sample(t.Context(), refreshedAt)

	envelope, err := registry.Diagnostics(refreshedAt)
	require.NoError(t, err)

	queue := envelope.Workers["webhook_inbox"].Queue
	require.True(t, envelope.Complete)
	require.Equal(t, workercontract.QueueSnapshotCurrent, queue.SnapshotStatus)
	require.Equal(t, new(refreshedAt.UnixMilli()), queue.LastSuccessAtEpochMS)
	require.Equal(t, new(int64(5)), queue.Depth)
	require.Nil(t, queue.ErrorCode)
}

func TestDiagnosticsHandlerReturnsValid503EnvelopeOnStaleSnapshot(t *testing.T) {
	sampledAt := time.Now().Add(-40 * time.Second)
	registry := buildChatbotRegistry(t, func() workercontract.QueueSnapshot {
		return workercontract.CurrentQueueSnapshot(5, 3*time.Second, sampledAt)
	})
	recorder := httptest.NewRecorder()
	registry.Handler().ServeHTTP(recorder, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/diagnostics/workers", http.NoBody))

	require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
	require.Equal(t, "application/json", recorder.Header().Get("Content-Type"))

	var envelope workercontract.DiagnosticsEnvelope

	require.NoError(t, jsonv2.Unmarshal(recorder.Body.Bytes(), &envelope, jsonv2.RejectUnknownMembers(true)))
	require.Equal(t, workercontract.ContractVersion, envelope.ContractVersion)
	require.Equal(t, "chatbotgo", envelope.Service)
	require.Equal(t, "bot", envelope.Role)
	require.Greater(t, envelope.ObservedAtEpochMS, sampledAt.UnixMilli())
	require.Len(t, envelope.Workers, len(chatbotIdentity(t).WorkerIDs))
	require.False(t, envelope.Complete)

	queue := envelope.Workers["webhook_inbox"].Queue
	require.Equal(t, workercontract.QueueSnapshotUnavailable, queue.SnapshotStatus)
	require.Nil(t, queue.Depth)
	require.Nil(t, queue.OldestQueuedAgeMS)
	require.Equal(t, new(workercontract.QueueSnapshotStale), queue.ErrorCode)
	require.Equal(t, new(sampledAt.UnixMilli()), queue.LastSuccessAtEpochMS)

	for workerID, worker := range envelope.Workers {
		if workerID != "webhook_inbox" && worker.Queue.SnapshotStatus != workercontract.QueueSnapshotCurrent {
			t.Fatalf("healthy worker %s was affected: %+v", workerID, worker.Queue)
		}
	}
}

func TestRegistryQueueSnapshotMissingMetadataAndUnavailableReasons(t *testing.T) {
	sampledAt := time.Unix(1000, 0)
	for _, tc := range []struct {
		name     string
		snapshot workercontract.QueueSnapshot
		wantCode workercontract.QueueSnapshotErrorCode
	}{
		{"missing success timestamp", workercontract.QueueSnapshot{Status: workercontract.QueueSnapshotCurrent, Depth: new(int64(5)), OldestQueuedAgeMS: new(int64(0))}, workercontract.QueueSnapshotFailed},
		{"nil depth", workercontract.QueueSnapshot{Status: workercontract.QueueSnapshotCurrent, OldestQueuedAgeMS: new(int64(0)), LastSuccessAtEpochMS: new(sampledAt.UnixMilli())}, workercontract.QueueSnapshotFailed},
		{"nil age", workercontract.QueueSnapshot{Status: workercontract.QueueSnapshotCurrent, Depth: new(int64(5)), LastSuccessAtEpochMS: new(sampledAt.UnixMilli())}, workercontract.QueueSnapshotFailed},
		{"never sampled", workercontract.QueueSnapshot{Status: workercontract.QueueSnapshotUnavailable, ErrorCode: new(workercontract.QueueNotSampled)}, workercontract.QueueNotSampled},
		{"latest attempt failed", workercontract.QueueSnapshot{Status: workercontract.QueueSnapshotUnavailable, LastSuccessAtEpochMS: new(sampledAt.UnixMilli()), ErrorCode: new(workercontract.QueueSnapshotFailed)}, workercontract.QueueSnapshotFailed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			registry := buildChatbotRegistry(t, func() workercontract.QueueSnapshot { return tc.snapshot })

			envelope, err := registry.Diagnostics(sampledAt.Add(40 * time.Second))
			if err != nil {
				t.Fatal(err)
			}

			queue := envelope.Workers["webhook_inbox"].Queue
			if envelope.Complete || queue.SnapshotStatus != workercontract.QueueSnapshotUnavailable || queue.Depth != nil || queue.OldestQueuedAgeMS != nil ||
				queue.ErrorCode == nil || *queue.ErrorCode != tc.wantCode {
				t.Fatalf("unavailable queue = %+v", queue)
			}

			if tc.snapshot.LastSuccessAtEpochMS == nil {
				if queue.LastSuccessAtEpochMS != nil {
					t.Fatalf("invented last success: %d", *queue.LastSuccessAtEpochMS)
				}
			} else if queue.LastSuccessAtEpochMS == nil || *queue.LastSuccessAtEpochMS != *tc.snapshot.LastSuccessAtEpochMS {
				t.Fatalf("lost last success: %+v", queue)
			}
		})
	}
}

func TestInvalidExecutorKeepsOtherDiagnosticsAndRecovers(t *testing.T) {
	for _, tc := range []struct {
		name     string
		snapshot workercontract.ExecutorSnapshot
	}{
		{"running workers", workercontract.ExecutorSnapshot{RunningWorkers: -1}},
		{"in flight", workercontract.ExecutorSnapshot{InFlight: -1}},
		{"oldest in flight", workercontract.ExecutorSnapshot{OldestInFlightAgeMS: -1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			loaded, err := workercontract.LoadProfileFile(validProfilePath(), chatbotIdentity(t))
			require.NoError(t, err)

			now := time.Now()
			snapshot := tc.snapshot
			registry := buildExecutorObservationRegistry(t, loaded, &snapshot, now)

			recorder := httptest.NewRecorder()
			registry.Handler().ServeHTTP(recorder, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/diagnostics/workers", http.NoBody))
			require.Equal(t, http.StatusServiceUnavailable, recorder.Code)

			var envelope workercontract.DiagnosticsEnvelope

			require.NoError(t, jsonv2.Unmarshal(recorder.Body.Bytes(), &envelope, jsonv2.RejectUnknownMembers(true)))
			require.False(t, envelope.Complete)
			require.Equal(t, loaded.Hash, envelope.Profile.Hash)
			require.Len(t, envelope.Workers, len(loaded.Profile.Workers))

			failed := envelope.Workers["draw"]
			require.True(t, failed.Executor.Enabled)
			require.Equal(t, loaded.Profile.Workers["draw"].Executor.ConfiguredWorkers, failed.Executor.ConfiguredWorkers)
			require.Nil(t, failed.Executor.RunningWorkers)
			require.Nil(t, failed.Executor.InFlight)
			require.Nil(t, failed.Executor.OldestInFlightAgeMS)
			require.Equal(t, workercontract.QueueSnapshotCurrent, failed.Queue.SnapshotStatus)
			require.Equal(t, new(int64(2)), failed.Queue.Depth)
			require.Equal(t, uint64(1), failed.Totals.Admissions.Accepted)
			require.Equal(t, new(int64(1)), envelope.Workers["summary"].Executor.RunningWorkers)

			assertExecutorMetricsUnavailable(t, registry, now, len(loaded.Profile.Workers))

			snapshot = workercontract.ExecutorSnapshot{RunningWorkers: 2, InFlight: 1, OldestInFlightAgeMS: 300}

			recovered, err := registry.Diagnostics(now)
			require.NoError(t, err)
			require.True(t, recovered.Complete)
			require.Equal(t, new(int64(2)), recovered.Workers["draw"].Executor.RunningWorkers)
			require.Equal(t, new(int64(1)), recovered.Workers["draw"].Executor.InFlight)
			require.Equal(t, new(int64(300)), recovered.Workers["draw"].Executor.OldestInFlightAgeMS)
		})
	}
}

func TestRegistryRejectsEmptyProfile(t *testing.T) {
	registry := workercontract.NewRegistry(workercontract.LoadedProfile{}, nil)
	require.Error(t, registry.Seal())

	_, err := registry.Diagnostics(time.Now())
	require.Error(t, err)
}

func buildExecutorObservationRegistry(t *testing.T, loaded workercontract.LoadedProfile, snapshot *workercontract.ExecutorSnapshot, now time.Time) *workercontract.Registry {
	t.Helper()

	registry := workercontract.NewRegistry(loaded, workercontract.NewProfileFileChecker(loaded, now))
	for workerID := range loaded.Profile.Workers {
		executor := func() workercontract.ExecutorSnapshot {
			return workercontract.ExecutorSnapshot{RunningWorkers: 1}
		}
		counters := &workercontract.Counters{}

		if workerID == "draw" {
			executor = func() workercontract.ExecutorSnapshot { return *snapshot }

			require.True(t, counters.RecordAdmission(workercontract.AdmissionAccepted))
		}

		require.NoError(t, registry.Register(workercontract.Registration{
			WorkerID:                workerID,
			Runtime:                 workercontract.RuntimeGo,
			QueueBackend:            workercontract.QueueMemory,
			QueueScope:              workercontract.QueueScopeProcess,
			SettingsValidated:       true,
			PerJobDeadlineValidated: true,
			ExecutorSnapshot:        executor,
			QueueSnapshot: func() workercontract.QueueSnapshot {
				return workercontract.CurrentQueueSnapshot(2, time.Second, now)
			},
			Counters: counters,
		}))
	}

	require.NoError(t, registry.Seal())

	return registry
}

func assertExecutorMetricsUnavailable(t *testing.T, registry *workercontract.Registry, now time.Time, workers int) {
	t.Helper()

	families, err := registry.Metrics(now)
	require.NoError(t, err)

	for _, family := range families {
		switch family.Name {
		case "iris_stack_worker_running_workers", "iris_stack_worker_in_flight", "iris_stack_worker_oldest_in_flight_age_seconds":
			require.Len(t, family.Samples, workers-1)

			for _, sample := range family.Samples {
				require.NotEqual(t, "draw", sample.Labels["worker"])
			}
		case "iris_stack_worker_queue_depth":
			require.Len(t, family.Samples, workers)
		}
	}
}
