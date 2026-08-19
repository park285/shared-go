package workercontract

import "sync/atomic"

// AdmissionResult는 canonical queue ownership 결론이다.
type AdmissionResult string

const (
	AdmissionAccepted       AdmissionResult = "accepted"
	AdmissionDuplicate      AdmissionResult = "duplicate"
	AdmissionRejected       AdmissionResult = "rejected"
	AdmissionFailed         AdmissionResult = "failed"
	AdmissionOutcomeUnknown AdmissionResult = "outcome_unknown"
)

// AttemptOutcome은 시작된 attempt의 terminal 결론이다.
type AttemptOutcome string

const (
	AttemptSuccess        AttemptOutcome = "success"
	AttemptFailed         AttemptOutcome = "failed"
	AttemptTimeout        AttemptOutcome = "timeout"
	AttemptCanceled       AttemptOutcome = "canceled"
	AttemptPanic          AttemptOutcome = "panic"
	AttemptOutcomeUnknown AttemptOutcome = "outcome_unknown"
)

// DiscardReason은 attempt 시작 전 owned work를 폐기한 이유다.
type DiscardReason string

const (
	DiscardStale    DiscardReason = "stale"
	DiscardShutdown DiscardReason = "shutdown"
)

// Counters는 worker별 bounded outcome 누계를 instance 안에 보관한다.
type Counters struct {
	admissions [5]atomic.Uint64
	attempts   [6]atomic.Uint64
	discarded  [2]atomic.Uint64
}

// RecordAdmission은 증명된 admission 결론만 기록한다.
func (c *Counters) RecordAdmission(result AdmissionResult) bool {
	if c == nil {
		return false
	}
	index, ok := admissionIndex(result)
	if !ok {
		return false
	}
	c.admissions[index].Add(1)
	return true
}

// RecordAttempt은 시작된 attempt의 terminal 결론만 기록한다.
func (c *Counters) RecordAttempt(outcome AttemptOutcome) bool {
	if c == nil {
		return false
	}
	index, ok := attemptIndex(outcome)
	if !ok {
		return false
	}
	c.attempts[index].Add(1)
	return true
}

// RecordDiscard는 attempt 시작 전 discard를 기록한다.
func (c *Counters) RecordDiscard(reason DiscardReason) bool {
	if c == nil {
		return false
	}
	index, ok := discardIndex(reason)
	if !ok {
		return false
	}
	c.discarded[index].Add(1)
	return true
}

func admissionIndex(result AdmissionResult) (int, bool) {
	switch result {
	case AdmissionAccepted:
		return 0, true
	case AdmissionDuplicate:
		return 1, true
	case AdmissionRejected:
		return 2, true
	case AdmissionFailed:
		return 3, true
	case AdmissionOutcomeUnknown:
		return 4, true
	default:
		return 0, false
	}
}

func attemptIndex(outcome AttemptOutcome) (int, bool) {
	switch outcome {
	case AttemptSuccess:
		return 0, true
	case AttemptFailed:
		return 1, true
	case AttemptTimeout:
		return 2, true
	case AttemptCanceled:
		return 3, true
	case AttemptPanic:
		return 4, true
	case AttemptOutcomeUnknown:
		return 5, true
	default:
		return 0, false
	}
}

func discardIndex(reason DiscardReason) (int, bool) {
	switch reason {
	case DiscardStale:
		return 0, true
	case DiscardShutdown:
		return 1, true
	default:
		return 0, false
	}
}

// AdmissionTotals는 diagnostics wire vocabulary를 고정한다.
type AdmissionTotals struct {
	Accepted       uint64 `json:"accepted"`
	Duplicate      uint64 `json:"duplicate"`
	Rejected       uint64 `json:"rejected"`
	Failed         uint64 `json:"failed"`
	OutcomeUnknown uint64 `json:"outcomeUnknown"`
}

// AttemptTotals는 diagnostics wire vocabulary를 고정한다.
type AttemptTotals struct {
	Success        uint64 `json:"success"`
	Failed         uint64 `json:"failed"`
	Timeout        uint64 `json:"timeout"`
	Canceled       uint64 `json:"canceled"`
	Panic          uint64 `json:"panic"`
	OutcomeUnknown uint64 `json:"outcomeUnknown"`
}

// DiscardTotals는 diagnostics wire vocabulary를 고정한다.
type DiscardTotals struct {
	Stale    uint64 `json:"stale"`
	Shutdown uint64 `json:"shutdown"`
}

// WorkerTotals는 한 worker의 bounded 누계다.
type WorkerTotals struct {
	Admissions AdmissionTotals `json:"admissions"`
	Attempts   AttemptTotals   `json:"attempts"`
	Discarded  DiscardTotals   `json:"discarded"`
}

// Snapshot은 현재 bounded outcome 누계를 반환한다.
func (c *Counters) Snapshot() WorkerTotals {
	if c == nil {
		return WorkerTotals{}
	}
	return WorkerTotals{
		Admissions: AdmissionTotals{
			Accepted:       c.admissions[0].Load(),
			Duplicate:      c.admissions[1].Load(),
			Rejected:       c.admissions[2].Load(),
			Failed:         c.admissions[3].Load(),
			OutcomeUnknown: c.admissions[4].Load(),
		},
		Attempts: AttemptTotals{
			Success:        c.attempts[0].Load(),
			Failed:         c.attempts[1].Load(),
			Timeout:        c.attempts[2].Load(),
			Canceled:       c.attempts[3].Load(),
			Panic:          c.attempts[4].Load(),
			OutcomeUnknown: c.attempts[5].Load(),
		},
		Discarded: DiscardTotals{
			Stale:    c.discarded[0].Load(),
			Shutdown: c.discarded[1].Load(),
		},
	}
}

// DiagnosticsEnvelope은 `/diagnostics/workers`의 v1 body다.
type DiagnosticsEnvelope struct {
	ContractVersion   int                          `json:"contractVersion"`
	Service           string                       `json:"service"`
	Role              string                       `json:"role"`
	ObservedAtEpochMS int64                        `json:"observedAtEpochMs"`
	Complete          bool                         `json:"complete"`
	Profile           ProfileDiagnostics           `json:"profile"`
	Workers           map[string]WorkerDiagnostics `json:"workers"`
}

// ProfileDiagnostics는 loaded identity와 file drift만 공개한다.
type ProfileDiagnostics struct {
	ID                   string                `json:"id"`
	Hash                 string                `json:"hash"`
	FileMatch            bool                  `json:"fileMatch"`
	FileCheckedAtEpochMS int64                 `json:"fileCheckedAtEpochMs"`
	FileErrorCode        *ProfileFileErrorCode `json:"fileErrorCode"`
}

// WorkerDiagnostics는 executor, canonical queue, bounded totals를 묶는다.
type WorkerDiagnostics struct {
	Runtime  RuntimeKind         `json:"runtime"`
	Executor ExecutorDiagnostics `json:"executor"`
	Queue    QueueDiagnostics    `json:"queue"`
	Totals   WorkerTotals        `json:"totals"`
}

// ExecutorDiagnostics는 process-local executor 상태다.
type ExecutorDiagnostics struct {
	Enabled             bool  `json:"enabled"`
	ConfiguredWorkers   int   `json:"configuredWorkers"`
	RunningWorkers      int64 `json:"runningWorkers"`
	InFlight            int64 `json:"inFlight"`
	OldestInFlightAgeMS int64 `json:"oldestInFlightAgeMs"`
}

// QueueDiagnostics는 latest-attempt snapshot 의미를 보존한다.
type QueueDiagnostics struct {
	Backend              QueueBackend            `json:"backend"`
	Scope                QueueScope              `json:"scope"`
	Bounded              bool                    `json:"bounded"`
	Capacity             *int64                  `json:"capacity"`
	Depth                *int64                  `json:"depth"`
	OldestQueuedAgeMS    *int64                  `json:"oldestQueuedAgeMs"`
	SnapshotStatus       QueueSnapshotStatus     `json:"snapshotStatus"`
	LastSuccessAtEpochMS *int64                  `json:"lastSuccessAtEpochMs"`
	ErrorCode            *QueueSnapshotErrorCode `json:"errorCode"`
}
