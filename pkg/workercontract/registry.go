package workercontract

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"sync"
	"time"
)

// RuntimeKind는 worker implementation runtime label이다.
type RuntimeKind string

const (
	RuntimeGo   RuntimeKind = "go"
	RuntimeRust RuntimeKind = "rust"
)

// QueueBackend는 canonical queue backend label이다.
type QueueBackend string

const (
	QueueMemory   QueueBackend = "memory"
	QueueSQLite   QueueBackend = "sqlite"
	QueuePostgres QueueBackend = "postgres"
	QueueValkey   QueueBackend = "valkey"
)

// QueueScope는 central aggregation semantics를 고정한다.
type QueueScope string

const (
	QueueScopeProcess QueueScope = "process"
	QueueScopeShared  QueueScope = "shared"
)

// ExecutorSnapshot은 process-local executor의 non-blocking snapshot이다.
type ExecutorSnapshot struct {
	RunningWorkers      int64
	InFlight            int64
	OldestInFlightAgeMS int64
}

// Registration은 profile worker 하나와 실제 implementation adapter를 연결한다.
type Registration struct {
	WorkerID                string
	Runtime                 RuntimeKind
	QueueBackend            QueueBackend
	QueueScope              QueueScope
	SettingsValidated       bool
	PerJobDeadlineValidated bool
	ExecutorSnapshot        func() ExecutorSnapshot
	QueueSnapshot           func() QueueSnapshot
	Counters                *Counters
	TotalsSnapshot          func() WorkerTotals
}

// Registry는 process-local worker registration과 diagnostics를 소유한다.
type Registry struct {
	mu            sync.RWMutex
	loaded        LoadedProfile
	fileChecker   *ProfileFileChecker
	registrations map[string]Registration
	sealed        bool
	runtime       RuntimeKind
}

// NewRegistry는 검증된 profile을 기반으로 빈 instance-owned registry를 만든다.
func NewRegistry(loaded LoadedProfile, checker *ProfileFileChecker) *Registry {
	return &Registry{
		loaded:        loaded,
		fileChecker:   checker,
		registrations: make(map[string]Registration, len(loaded.Profile.Workers)),
	}
}

// WorkerProfile은 service validator가 strict settings를 해석할 worker entry를 반환한다.
func (r *Registry) WorkerProfile(workerID string) (WorkerProfile, bool) {
	if r == nil {
		return WorkerProfile{}, false
	}
	worker, ok := r.loaded.Profile.Workers[workerID]
	return worker, ok
}

// Counters는 등록된 worker의 instrumentation owner를 반환한다.
func (r *Registry) Counters(workerID string) (*Counters, bool) {
	if r == nil {
		return nil, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	registration, ok := r.registrations[workerID]
	return registration.Counters, ok
}

// Register는 worker adapter 하나를 중복 없이 등록한다.
func (r *Registry) Register(registration Registration) error {
	if r == nil {
		return errors.New("worker registry: nil registry")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.sealed {
		return errors.New("worker registry: already sealed")
	}
	profileWorker, ok := r.loaded.Profile.Workers[registration.WorkerID]
	if !ok {
		return fmt.Errorf("worker registry: unknown worker %q", registration.WorkerID)
	}
	if _, exists := r.registrations[registration.WorkerID]; exists {
		return fmt.Errorf("worker registry: duplicate worker %q", registration.WorkerID)
	}
	if !registration.SettingsValidated {
		return fmt.Errorf("worker registry: %s settings were not strictly validated", registration.WorkerID)
	}
	if err := validateRegistrationMetadata(registration); err != nil {
		return fmt.Errorf("worker registry: %s: %w", registration.WorkerID, err)
	}
	if profileWorker.Executor.Enabled && registration.ExecutorSnapshot == nil {
		return fmt.Errorf("worker registry: %s: executor snapshot source required", registration.WorkerID)
	}
	if registration.QueueSnapshot == nil && (registration.QueueBackend != QueueMemory || profileWorker.Executor.Enabled) {
		return fmt.Errorf("worker registry: %s: queue snapshot source required", registration.WorkerID)
	}
	if profileWorker.Executor.AttemptTimeout.Mode == DurationModePerJob && !registration.PerJobDeadlineValidated {
		return fmt.Errorf("worker registry: %s: per-job deadline storage is not validated", registration.WorkerID)
	}
	if registration.Counters == nil && registration.TotalsSnapshot == nil {
		registration.Counters = &Counters{}
	}
	r.registrations[registration.WorkerID] = registration
	return nil
}

func validateRegistrationMetadata(registration Registration) error {
	if registration.Runtime != RuntimeGo && registration.Runtime != RuntimeRust {
		return errors.New("invalid runtime")
	}
	switch registration.QueueBackend {
	case QueueMemory, QueueSQLite, QueuePostgres, QueueValkey:
	default:
		return errors.New("invalid queue backend")
	}
	if registration.QueueScope != QueueScopeProcess && registration.QueueScope != QueueScopeShared {
		return errors.New("invalid queue scope")
	}
	if registration.QueueBackend == QueueMemory && registration.QueueScope != QueueScopeProcess {
		return errors.New("memory queue must use process scope")
	}
	return nil
}

// Seal은 exact worker set과 single-runtime identity를 검증한다.
func (r *Registry) Seal() error {
	if r == nil {
		return errors.New("worker registry: nil registry")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.sealed {
		return nil
	}
	expected := make([]string, 0, len(r.loaded.Profile.Workers))
	actual := make([]string, 0, len(r.registrations))
	for workerID := range r.loaded.Profile.Workers {
		expected = append(expected, workerID)
	}
	for workerID, registration := range r.registrations {
		actual = append(actual, workerID)
		if r.runtime == "" {
			r.runtime = registration.Runtime
		} else if r.runtime != registration.Runtime {
			return errors.New("worker registry: one process cannot register multiple runtimes")
		}
	}
	slices.Sort(expected)
	slices.Sort(actual)
	if !slices.Equal(expected, actual) {
		return fmt.Errorf("worker registry: got workers %v, want %v", actual, expected)
	}
	r.sealed = true
	return nil
}

// Diagnostics는 queue I/O 없이 current registry envelope를 만든다.
func (r *Registry) Diagnostics(observedAt time.Time) (DiagnosticsEnvelope, error) {
	if r == nil {
		return DiagnosticsEnvelope{}, errors.New("worker registry: nil registry")
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if !r.sealed {
		return DiagnosticsEnvelope{}, errors.New("worker registry: not sealed")
	}
	fileStatus := r.fileChecker.Status()
	envelope := DiagnosticsEnvelope{
		ContractVersion:   ContractVersion,
		Service:           r.loaded.Profile.Service,
		Role:              r.loaded.Profile.Role,
		ObservedAtEpochMS: observedAt.UnixMilli(),
		Complete:          true,
		Profile: ProfileDiagnostics{
			ID:                   r.loaded.Profile.ProfileID,
			Hash:                 r.loaded.Hash,
			FileMatch:            fileStatus.Match,
			FileCheckedAtEpochMS: fileStatus.CheckedAtEpochMS,
			FileErrorCode:        fileStatus.ErrorCode,
		},
		Workers: make(map[string]WorkerDiagnostics, len(r.registrations)),
	}
	for workerID, registration := range r.registrations {
		profileWorker := r.loaded.Profile.Workers[workerID]
		executor, err := executorDiagnostics(profileWorker, registration)
		if err != nil {
			return DiagnosticsEnvelope{}, fmt.Errorf("worker registry: %s: %w", workerID, err)
		}
		queue := queueDiagnostics(profileWorker, registration, observedAt)
		if queue.SnapshotStatus != QueueSnapshotCurrent {
			envelope.Complete = false
		}
		envelope.Workers[workerID] = WorkerDiagnostics{
			Runtime:  registration.Runtime,
			Executor: executor,
			Queue:    queue,
			Totals:   registrationTotals(registration),
		}
	}
	return envelope, nil
}

func registrationTotals(registration Registration) WorkerTotals {
	if registration.TotalsSnapshot != nil {
		return registration.TotalsSnapshot()
	}
	return registration.Counters.Snapshot()
}

func executorDiagnostics(profile WorkerProfile, registration Registration) (ExecutorDiagnostics, error) {
	diagnostics := ExecutorDiagnostics{
		Enabled:           profile.Executor.Enabled,
		ConfiguredWorkers: profile.Executor.ConfiguredWorkers,
	}
	if !profile.Executor.Enabled {
		return diagnostics, nil
	}
	snapshot := registration.ExecutorSnapshot()
	if snapshot.RunningWorkers < 0 || snapshot.InFlight < 0 || snapshot.OldestInFlightAgeMS < 0 {
		return ExecutorDiagnostics{}, errors.New("invalid executor snapshot")
	}
	diagnostics.RunningWorkers = snapshot.RunningWorkers
	diagnostics.InFlight = snapshot.InFlight
	diagnostics.OldestInFlightAgeMS = snapshot.OldestInFlightAgeMS
	return diagnostics, nil
}

func queueDiagnostics(profile WorkerProfile, registration Registration, observedAt time.Time) QueueDiagnostics {
	diagnostics := QueueDiagnostics{
		Backend: registration.QueueBackend,
		Scope:   registration.QueueScope,
		Bounded: profile.Queue.Capacity.Mode == CapacityModeBounded,
	}
	if profile.Queue.Capacity.Items != nil {
		capacity := *profile.Queue.Capacity.Items
		diagnostics.Capacity = &capacity
	}
	var snapshot QueueSnapshot
	if registration.QueueSnapshot == nil {
		snapshot = CurrentQueueSnapshot(0, 0, observedAt)
	} else {
		snapshot = registration.QueueSnapshot()
	}
	if !validQueueSnapshot(snapshot) {
		code := QueueSnapshotFailed
		snapshot.Status = QueueSnapshotUnavailable
		snapshot.Depth = nil
		snapshot.OldestQueuedAgeMS = nil
		snapshot.ErrorCode = &code
	}
	diagnostics.Depth = snapshot.Depth
	diagnostics.OldestQueuedAgeMS = snapshot.OldestQueuedAgeMS
	diagnostics.SnapshotStatus = snapshot.Status
	diagnostics.LastSuccessAtEpochMS = snapshot.LastSuccessAtEpochMS
	diagnostics.ErrorCode = snapshot.ErrorCode
	return diagnostics
}

func validQueueSnapshot(snapshot QueueSnapshot) bool {
	switch snapshot.Status {
	case QueueSnapshotCurrent:
		return snapshot.Depth != nil && snapshot.OldestQueuedAgeMS != nil && snapshot.ErrorCode == nil &&
			*snapshot.Depth >= 0 && *snapshot.OldestQueuedAgeMS >= 0 &&
			(*snapshot.Depth != 0 || *snapshot.OldestQueuedAgeMS == 0)
	case QueueSnapshotUnavailable:
		return snapshot.Depth == nil && snapshot.OldestQueuedAgeMS == nil && snapshot.ErrorCode != nil
	default:
		return false
	}
}

// Handler는 sealed registry의 access-control-neutral HTTP handler를 반환한다.
func (r *Registry) Handler() http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		envelope, err := r.Diagnostics(time.Now())
		writer.Header().Set("Content-Type", "application/json")
		if err != nil {
			writer.WriteHeader(http.StatusServiceUnavailable)
			if encodeErr := json.NewEncoder(writer).Encode(map[string]string{"errorCode": "worker_registry_unavailable"}); encodeErr != nil {
				return
			}
			return
		}
		if !envelope.Complete {
			writer.WriteHeader(http.StatusServiceUnavailable)
		}
		if encodeErr := json.NewEncoder(writer).Encode(envelope); encodeErr != nil {
			return
		}
	})
}
