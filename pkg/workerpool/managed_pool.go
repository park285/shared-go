package workerpool

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"runtime/debug"
	"sync"
	"time"
)

// ErrJobTimeout은 dequeue 이후 job budget이 소진된 원인이다.
var ErrJobTimeout = errors.New("managed worker job timeout")

// ErrPoolShutdown은 pool shutdown으로 in-flight job이 취소된 원인이다.
var ErrPoolShutdown = errors.New("managed worker pool shutdown")

// JobOutcome은 managed job이 종료된 이유를 나타낸다.
type JobOutcome string

const (
	JobOutcomeSuccess  JobOutcome = "success"
	JobOutcomeRejected JobOutcome = "rejected"
	JobOutcomeStale    JobOutcome = "stale"
	JobOutcomePanic    JobOutcome = "panic"
	JobOutcomeShutdown JobOutcome = "shutdown"
	JobOutcomeTimeout  JobOutcome = "timeout"
	JobOutcomeCanceled JobOutcome = "canceled"
)

// JobSpec은 queue admission 이후 dequeue 시점에 실행 budget을 만드는 작업 계약이다.
type JobSpec struct {
	// Context는 값(trace, logger 등)만 전달한다. pool이 취소 신호를 끊고 자체 budget을 붙이므로
	// 이 context를 취소해도 Run이나 Finalize는 중단되지 않는다.
	Context     context.Context //nolint:containedctx // JobSpec은 queue에 적재된 뒤 dequeue 시점에 실행되는 작업 명세라, 값 전용 context를 필드로 운반해야 한다.
	Kind        string
	Timeout     time.Duration
	MaxQueueAge time.Duration
	Run         func(context.Context)
	// Finalize의 FinalizeTimeout은 관측 전용이다. 초과해도 callback은 중단되지 않고
	// reservation capacity는 callback이 실제로 반환할 때까지 회수되지 않는다.
	Finalize func(context.Context, JobOutcome)
}

// ManagedConfig는 ManagedPool의 고정 worker, queue, finalizer scheduler budget을 설정한다.
type ManagedConfig struct {
	Workers   int
	QueueSize int
	// FinalizeTimeout은 callback 실제 시작 시점부터 적용하는 관측용 예산이다. 초과는 TimedOut과
	// OverdueInFlight로 드러날 뿐이며 callback을 중단하거나 reservation을 회수하지 않는다.
	FinalizeTimeout time.Duration
	// FinalizeConcurrency는 동시에 실행할 수 있는 callback 수다.
	FinalizeConcurrency int
	// FinalizeQueueSize는 accepted job의 pending reservation, queued callback,
	// in-flight callback을 합한 총 reservation capacity다. 0 이하면 Workers+QueueSize를 사용한다.
	FinalizeQueueSize int
	Logger            *slog.Logger
}

// ManagedSubmitReason은 TrySubmitResult의 admission 또는 callback ownership 결론이다.
type ManagedSubmitReason string

const (
	ManagedSubmitAccepted                   ManagedSubmitReason = "accepted"
	ManagedSubmitRejected                   ManagedSubmitReason = "rejected"
	ManagedSubmitRejectedFinalizerScheduled ManagedSubmitReason = "rejected_finalizer_scheduled"
	ManagedSubmitRejectedFinalizerCapacity  ManagedSubmitReason = "rejected_finalizer_capacity"
	// ManagedSubmitRejectedFinalizerClosed는 capacity 부족이 아니라 shutdown으로 거부된 경우다.
	ManagedSubmitRejectedFinalizerClosed ManagedSubmitReason = "rejected_finalizer_closed"
)

// ManagedSubmitResult는 admission과 pool의 Finalize callback ownership을 함께 제공한다.
// FinalizerClaimed가 true이면 callback lifecycle은 pool이 소유하며 호출자는 직접 Finalize하지 않는다.
// False인 rejected 결과는 callback이 전혀 예약되지 않았으므로 호출자가 durable claim을 직접 복구해야 한다.
type ManagedSubmitResult struct {
	Accepted         bool
	FinalizerClaimed bool
	Reason           ManagedSubmitReason
}

// ManagedFinalizerSnapshot은 finalizer reservation, scheduler lifecycle, callback 누계를 제공한다.
// FinalizeTimeout은 callback 시작 시점부터 적용되며 이미 발생한 외부 효과나 goroutine을 중단시키지 못한다.
type ManagedFinalizerSnapshot struct {
	Concurrency  int
	QueueSize    int
	QueueDepth   int
	InFlight     int
	Reservations int
	// Accepting은 finalizer가 새 reservation을 받을 수 있는 lifecycle 상태다.
	Accepting bool
	// DispatchDrained는 Close 이후 pending reservation과 callback queue가 모두 dispatch됐음을 뜻한다.
	// 이미 시작된 callback의 실제 반환 완료는 뜻하지 않는다.
	DispatchDrained bool
	// Quiesced는 Close 이후 모든 callback이 실제로 반환하고 reservation이 해제됐음을 뜻한다.
	Quiesced bool
	// OverdueInFlight는 FinalizeTimeout이 지났는데도 아직 반환하지 않은 callback 수다.
	// 이 slot들의 reservation은 회수되지 않으므로 값이 지속되면 admission 고갈로 이어진다.
	OverdueInFlight     int
	Claimed             uint64
	Started             uint64
	Completed           uint64
	CompletedLate       uint64
	TimedOut            uint64
	Overloaded          uint64
	ReservationRejected uint64
	Panicked            uint64
}

// ManagedSnapshot은 pool의 현재 queue, 실행, outcome 상태를 복사해 제공한다.
type ManagedSnapshot struct {
	ConfiguredWorkers  int
	RunningWorkers     int
	QueueDepth         int
	InFlight           int
	OldestQueueAge     time.Duration
	OldestInFlightAge  time.Duration
	AdmissionsAccepted uint64
	AdmissionsRejected uint64
	Attempts           map[JobOutcome]uint64
	Discarded          map[JobOutcome]uint64
	Outcomes           map[JobOutcome]uint64
	Finalizer          ManagedFinalizerSnapshot
}

type managedJob struct {
	spec              JobSpec
	enqueuedAt        time.Time
	expiresAt         time.Time
	finalizerReserved bool
	finalizeOnce      sync.Once
	started           bool
}

type managedInFlight struct {
	cancel    context.CancelCauseFunc
	startedAt time.Time
}

// ManagedPool은 dequeue-time budget과 단일 finalization을 소유하는 worker pool이다.
type ManagedPool struct {
	mu                 sync.Mutex
	queue              []*managedJob
	queueSize          int
	configuredWorkers  int
	runningWorkers     int
	closed             bool
	workAvailable      *sync.Cond
	reaperNotify       chan struct{}
	stopCh             chan struct{}
	shutdownDone       chan struct{}
	shutdownOnce       sync.Once
	workerWG           sync.WaitGroup
	reaperWG           sync.WaitGroup
	inFlight           map[*managedJob]managedInFlight
	outcomes           map[JobOutcome]uint64
	attempts           map[JobOutcome]uint64
	discarded          map[JobOutcome]uint64
	admissionsAccepted uint64
	admissionsRejected uint64
	finalizer          *managedFinalizer
	logger             *slog.Logger
}

// NewManagedPool은 explicit config를 검증한 뒤 ManagedPool을 시작한다.
func NewManagedPool(config ManagedConfig) (*ManagedPool, error) {
	if config.Workers < 1 || config.Workers > 4096 {
		return nil, errors.New("managed worker pool: workers must be in 1..4096")
	}

	if config.QueueSize < 1 || config.QueueSize > 1_048_576 {
		return nil, errors.New("managed worker pool: queue size must be in 1..1048576")
	}

	if config.FinalizeTimeout <= 0 {
		return nil, errors.New("managed worker pool: finalize timeout must be positive")
	}

	if config.FinalizeConcurrency < 1 || config.FinalizeConcurrency > 4096 {
		return nil, errors.New("managed worker pool: finalize concurrency must be in 1..4096")
	}

	if config.FinalizeQueueSize < config.FinalizeConcurrency || config.FinalizeQueueSize > 1_048_576 {
		return nil, errors.New("managed worker pool: finalize queue size must be in finalize concurrency..1048576")
	}

	if config.Logger == nil {
		config.Logger = slog.Default()
	}

	return newManagedPool(config), nil
}

func newManagedPool(config ManagedConfig) *ManagedPool {
	pool := &ManagedPool{
		queue:             make([]*managedJob, 0, config.QueueSize),
		queueSize:         config.QueueSize,
		configuredWorkers: config.Workers,
		reaperNotify:      make(chan struct{}, 1),
		stopCh:            make(chan struct{}),
		shutdownDone:      make(chan struct{}),
		inFlight:          make(map[*managedJob]managedInFlight, config.Workers),
		outcomes:          make(map[JobOutcome]uint64),
		attempts:          make(map[JobOutcome]uint64),
		discarded:         make(map[JobOutcome]uint64),
		finalizer: newManagedFinalizer(
			config.FinalizeConcurrency,
			config.FinalizeQueueSize,
			config.FinalizeTimeout,
			config.Logger,
		),
		logger: config.Logger,
	}

	pool.workAvailable = sync.NewCond(&pool.mu)

	for range config.Workers {
		pool.workerWG.Go(pool.worker)
	}

	pool.reaperWG.Go(pool.reaper)

	return pool
}

// Snapshot은 호출 시점의 queue depth, in-flight, oldest queue age, outcome 누계를 반환한다.
func (p *ManagedPool) Snapshot() ManagedSnapshot {
	if p == nil {
		return ManagedSnapshot{Outcomes: map[JobOutcome]uint64{}}
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	snapshot := ManagedSnapshot{
		ConfiguredWorkers:  p.configuredWorkers,
		RunningWorkers:     p.runningWorkers,
		QueueDepth:         len(p.queue),
		InFlight:           len(p.inFlight),
		Outcomes:           make(map[JobOutcome]uint64, len(p.outcomes)),
		Attempts:           make(map[JobOutcome]uint64, len(p.attempts)),
		Discarded:          make(map[JobOutcome]uint64, len(p.discarded)),
		AdmissionsAccepted: p.admissionsAccepted,
		AdmissionsRejected: p.admissionsRejected,
	}
	if len(p.queue) > 0 {
		snapshot.OldestQueueAge = max(time.Since(p.queue[0].enqueuedAt), 0)
	}

	for _, inFlight := range p.inFlight {
		age := max(time.Since(inFlight.startedAt), 0)
		if age > snapshot.OldestInFlightAge {
			snapshot.OldestInFlightAge = age
		}
	}

	maps.Copy(snapshot.Outcomes, p.outcomes)
	maps.Copy(snapshot.Attempts, p.attempts)
	maps.Copy(snapshot.Discarded, p.discarded)

	snapshot.Finalizer = p.finalizer.Snapshot()

	return snapshot
}

// TrySubmitResult는 작업을 기다리지 않고 queue에 admission하고 Finalize callback의 ownership을 반환한다.
func (p *ManagedPool) TrySubmitResult(spec JobSpec) ManagedSubmitResult {
	return p.trySubmit(spec)
}

func (p *ManagedPool) trySubmit(spec JobSpec) ManagedSubmitResult {
	if p == nil {
		return ManagedSubmitResult{Reason: ManagedSubmitRejected}
	}

	job := &managedJob{spec: spec, enqueuedAt: time.Now()}
	if spec.MaxQueueAge > 0 {
		job.expiresAt = job.enqueuedAt.Add(spec.MaxQueueAge)
	}

	if p.shuttingDown() {
		return p.rejectClosedSubmission(job)
	}

	if spec.Finalize != nil {
		reserved, reserveReason := p.finalizer.Reserve(spec.Kind)

		job.finalizerReserved = reserved

		if !reserved {
			p.finalizeJob(job, JobOutcomeRejected)

			return ManagedSubmitResult{Reason: submitReasonForReserve(reserveReason)}
		}
	}

	if spec.Run == nil {
		return p.rejectSubmission(job)
	}

	p.mu.Lock()

	if p.closed || len(p.queue) >= p.queueSize {
		p.mu.Unlock()

		return p.rejectSubmission(job)
	}

	p.queue = append(p.queue, job)
	p.admissionsAccepted++
	p.workAvailable.Signal()
	p.mu.Unlock()

	if !job.expiresAt.IsZero() {
		signalManagedPool(p.reaperNotify)
	}

	return ManagedSubmitResult{
		Accepted:         true,
		FinalizerClaimed: job.finalizerReserved,
		Reason:           ManagedSubmitAccepted,
	}
}

func (p *ManagedPool) shuttingDown() bool {
	select {
	case <-p.stopCh:
		return true
	default:
		return false
	}
}

// shutdown은 closed를 먼저 세우고 in-flight job을 모두 기다린 뒤에야 finalizer를 닫는다. 이 경로가 없으면
// 그 사이 제출이 아직 열린 finalizer의 reservation을 소진한 뒤 재시도 가능한 capacity 신호를 돌려준다.
func (p *ManagedPool) rejectClosedSubmission(job *managedJob) ManagedSubmitResult {
	p.finalizeJob(job, JobOutcomeRejected)

	if job.spec.Finalize != nil {
		return ManagedSubmitResult{Reason: ManagedSubmitRejectedFinalizerClosed}
	}

	return ManagedSubmitResult{Reason: ManagedSubmitRejected}
}

func (p *ManagedPool) rejectSubmission(job *managedJob) ManagedSubmitResult {
	p.finalizeJob(job, JobOutcomeRejected)

	return rejectedSubmitResult(job.finalizerReserved)
}

func rejectedSubmitResult(finalizerClaimed bool) ManagedSubmitResult {
	if finalizerClaimed {
		return ManagedSubmitResult{
			FinalizerClaimed: true,
			Reason:           ManagedSubmitRejectedFinalizerScheduled,
		}
	}

	return ManagedSubmitResult{Reason: ManagedSubmitRejected}
}

func submitReasonForReserve(reason finalizerReserveReason) ManagedSubmitReason {
	switch reason {
	case finalizerReserveRejectedCapacity:
		return ManagedSubmitRejectedFinalizerCapacity
	case finalizerReserveRejectedClosed:
		return ManagedSubmitRejectedFinalizerClosed
	// Reserve가 성공한 경로는 이 함수를 호출하지 않으므로, accepted는 알 수 없는 값과 같은 일반 거부로 떨어뜨린다.
	case finalizerReserveAccepted:
		return ManagedSubmitRejected
	default:
		return ManagedSubmitRejected
	}
}

// CloseContext는 admission을 닫고 queued job을 drop하며 in-flight job을 취소한다.
// Nil을 반환하면 pool이 소유한 accepted/rejected Finalize callback이 모두 실제로 반환했고
// finalizer reservation도 모두 해제된 상태다. Callback이 context를 무시하면 호출자 context로
// bounded하게 반환하되 성공을 보고하지 않는다.
func (p *ManagedPool) CloseContext(ctx context.Context) error {
	if p == nil {
		return nil
	}

	ctx = contextOrBackground(ctx)

	//nolint:contextcheck // shutdown은 호출자 context와 독립적으로 완주해야 finalizer reservation이 새지 않는다. ctx는 대기 시간만 제한한다.
	p.shutdownOnce.Do(func() { go p.shutdown() })

	select {
	case <-p.shutdownDone:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (p *ManagedPool) shutdown() {
	p.mu.Lock()

	p.closed = true

	queued := p.queue

	p.queue = nil

	cancels := make([]context.CancelCauseFunc, 0, len(p.inFlight))

	for _, inFlight := range p.inFlight {
		cancels = append(cancels, inFlight.cancel)
	}

	close(p.stopCh)
	p.workAvailable.Broadcast()
	p.mu.Unlock()

	for _, cancel := range cancels {
		cancel(ErrPoolShutdown)
	}

	for _, job := range queued {
		p.finalizeJob(job, JobOutcomeShutdown)
	}

	p.workerWG.Wait()
	p.reaperWG.Wait()
	<-p.finalizer.Close()
	close(p.shutdownDone)
}

func (p *ManagedPool) worker() {
	p.mu.Lock()

	p.runningWorkers++
	p.mu.Unlock()

	defer func() {
		p.mu.Lock()

		p.runningWorkers--
		p.mu.Unlock()
	}()

	for {
		job, ok := p.take()
		if !ok {
			return
		}

		p.run(job)
	}
}

func (p *ManagedPool) take() (*managedJob, bool) {
	for {
		p.mu.Lock()

		for !p.closed && len(p.queue) == 0 {
			p.workAvailable.Wait()
		}

		if p.closed {
			p.mu.Unlock()

			return nil, false
		}

		job := p.queue[0]

		p.queue[0] = nil
		p.queue = p.queue[1:]
		p.mu.Unlock()

		if !job.expiresAt.IsZero() && !time.Now().Before(job.expiresAt) {
			p.finalizeJob(job, JobOutcomeStale)

			continue
		}

		return job, true
	}
}

func (p *ManagedPool) run(job *managedJob) {
	base := job.spec.Context
	if base == nil {
		base = context.Background()
	}

	shutdownCtx, shutdownCancel := context.WithCancelCause(context.WithoutCancel(base))
	jobCtx := shutdownCtx
	timeoutCancel := func() {}

	if job.spec.Timeout > 0 {
		jobCtx, timeoutCancel = context.WithTimeoutCause(shutdownCtx, job.spec.Timeout, ErrJobTimeout)
	}

	p.mu.Lock()

	if p.closed {
		p.mu.Unlock()
		shutdownCancel(ErrPoolShutdown)
		timeoutCancel()
		p.finalizeJob(job, JobOutcomeShutdown)

		return
	}

	job.started = true
	p.inFlight[job] = managedInFlight{cancel: shutdownCancel, startedAt: time.Now()}
	p.mu.Unlock()

	outcome := JobOutcomeSuccess

	func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				outcome = JobOutcomePanic

				p.logger.Error(
					"managed_worker_job_panicked",
					slog.String("kind", job.spec.Kind),
					slog.Any("panic", fmt.Sprintf("%v", recovered)),
					slog.String("stack", string(debug.Stack())),
				)
			}
		}()

		job.spec.Run(jobCtx)
	}()

	if outcome == JobOutcomeSuccess {
		cause := context.Cause(jobCtx)
		switch {
		case cause == nil:
		case errors.Is(cause, ErrJobTimeout):
			outcome = JobOutcomeTimeout
		case errors.Is(cause, ErrPoolShutdown):
			outcome = JobOutcomeShutdown
		default:
			outcome = JobOutcomeCanceled
		}
	}

	timeoutCancel()
	shutdownCancel(nil)
	p.mu.Lock()
	delete(p.inFlight, job)
	p.mu.Unlock()
	p.finalizeJob(job, outcome)
}

func (p *ManagedPool) reaper() {
	for {
		deadline, ok := p.nextExpiry()
		if !ok {
			select {
			case <-p.reaperNotify:
			case <-p.stopCh:
				return
			}

			continue
		}

		delay := max(time.Until(deadline), 0)
		timer := time.NewTimer(delay)

		select {
		case <-timer.C:
			p.expireStale(time.Now())
		case <-p.reaperNotify:
			stopManagedTimer(timer)
		case <-p.stopCh:
			stopManagedTimer(timer)

			return
		}
	}
}

func stopManagedTimer(timer *time.Timer) {
	if timer == nil || timer.Stop() {
		return
	}

	select {
	case <-timer.C:
	default:
	}
}

func (p *ManagedPool) nextExpiry() (time.Time, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	var earliest time.Time

	for _, job := range p.queue {
		if job.expiresAt.IsZero() || (!earliest.IsZero() && !job.expiresAt.Before(earliest)) {
			continue
		}

		earliest = job.expiresAt
	}

	return earliest, !earliest.IsZero()
}

func (p *ManagedPool) expireStale(now time.Time) {
	p.mu.Lock()

	stale := make([]*managedJob, 0)
	kept := p.queue[:0]

	for _, job := range p.queue {
		if !job.expiresAt.IsZero() && !now.Before(job.expiresAt) {
			stale = append(stale, job)
			continue
		}

		kept = append(kept, job)
	}

	for index := len(kept); index < len(p.queue); index++ {
		p.queue[index] = nil
	}

	p.queue = kept
	p.mu.Unlock()

	for _, job := range stale {
		p.finalizeJob(job, JobOutcomeStale)
	}
}

func (p *ManagedPool) finalizeJob(job *managedJob, outcome JobOutcome) {
	job.finalizeOnce.Do(func() {
		p.mu.Lock()

		p.outcomes[outcome]++

		if job.started {
			p.attempts[outcome]++
		} else {
			switch outcome {
			case JobOutcomeRejected:
				p.admissionsRejected++
			case JobOutcomeStale, JobOutcomeShutdown:
				p.discarded[outcome]++
			// 아래 outcome들은 run이 job.started를 세운 뒤에만 나오므로 이 분기에 도달하지 않는다.
			case JobOutcomeSuccess, JobOutcomePanic, JobOutcomeTimeout, JobOutcomeCanceled:
			}
		}

		p.mu.Unlock()
		p.finalizer.Schedule(job.spec, outcome, job.finalizerReserved)
	})
}

func signalManagedPool(ch chan struct{}) {
	select {
	case ch <- struct{}{}:
	default:
	}
}

func contextOrBackground(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}

	return ctx
}
