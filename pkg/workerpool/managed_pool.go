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

const defaultFinalizeTimeout = 5 * time.Second

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
	Context     context.Context
	Kind        string
	Timeout     time.Duration
	MaxQueueAge time.Duration
	Run         func(context.Context)
	Finalize    func(context.Context, JobOutcome)
}

// ManagedConfig는 ManagedPool의 고정 worker, queue, finalizer scheduler budget을 설정한다.
type ManagedConfig struct {
	Workers   int
	QueueSize int
	// FinalizeTimeout은 callback 실제 시작 시점부터 적용한다.
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
)

// ManagedSubmitResult는 admission과 pool의 Finalize callback ownership을 함께 제공한다.
// FinalizerClaimed가 true이면 callback lifecycle은 pool이 소유하며 호출자는 직접 Finalize하지 않는다.
// false인 rejected 결과는 callback이 전혀 예약되지 않았으므로 호출자가 durable claim을 직접 복구해야 한다.
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
	Quiesced            bool
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
	QueueDepth     int
	InFlight       int
	OldestQueueAge time.Duration
	Outcomes       map[JobOutcome]uint64
	Finalizer      ManagedFinalizerSnapshot
}

type managedJob struct {
	spec              JobSpec
	enqueuedAt        time.Time
	expiresAt         time.Time
	finalizerReserved bool
	finalizeOnce      sync.Once
}

type managedFinalizerTask struct {
	base     context.Context
	kind     string
	outcome  JobOutcome
	callback func(context.Context, JobOutcome)
}

type managedFinalizerStart struct {
	ctx      context.Context
	cancel   context.CancelFunc
	deadline time.Time
}

type managedFinalizerResult struct {
	completedAt time.Time
	panicked    any
	stack       []byte
}

type managedFinalizer struct {
	mu       sync.Mutex
	queue    []*managedFinalizerTask
	snapshot ManagedFinalizerSnapshot
	closed   bool
	done     chan struct{}
	pending  int
	timeout  time.Duration
	logger   *slog.Logger
}

// ManagedPool은 dequeue-time budget과 단일 finalization을 소유하는 worker pool이다.
type ManagedPool struct {
	mu            sync.Mutex
	queue         []*managedJob
	queueSize     int
	closed        bool
	workAvailable *sync.Cond
	reaperNotify  chan struct{}
	stopCh        chan struct{}
	shutdownDone  chan struct{}
	shutdownOnce  sync.Once
	workerWG      sync.WaitGroup
	reaperWG      sync.WaitGroup
	inFlight      map[*managedJob]context.CancelCauseFunc
	outcomes      map[JobOutcome]uint64
	finalizer     *managedFinalizer
	logger        *slog.Logger
}

// NewManaged는 immutable worker와 queue 크기로 ManagedPool을 시작한다.
func NewManaged(config ManagedConfig) *ManagedPool {
	if config.Workers < 1 {
		config.Workers = 1
	}
	if config.QueueSize < 1 {
		config.QueueSize = 1
	}
	if config.FinalizeTimeout <= 0 {
		config.FinalizeTimeout = defaultFinalizeTimeout
	}
	if config.FinalizeConcurrency < 1 {
		config.FinalizeConcurrency = config.Workers
	}
	if config.FinalizeQueueSize < 1 {
		config.FinalizeQueueSize = config.Workers + config.QueueSize
	}
	if config.Logger == nil {
		config.Logger = slog.Default()
	}

	pool := &ManagedPool{
		queue:        make([]*managedJob, 0, config.QueueSize),
		queueSize:    config.QueueSize,
		reaperNotify: make(chan struct{}, 1),
		stopCh:       make(chan struct{}),
		shutdownDone: make(chan struct{}),
		inFlight:     make(map[*managedJob]context.CancelCauseFunc, config.Workers),
		outcomes:     make(map[JobOutcome]uint64),
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
		pool.workerWG.Add(1)
		go pool.worker()
	}
	pool.reaperWG.Add(1)
	go pool.reaper()
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
		QueueDepth: len(p.queue),
		InFlight:   len(p.inFlight),
		Outcomes:   make(map[JobOutcome]uint64, len(p.outcomes)),
	}
	if len(p.queue) > 0 {
		snapshot.OldestQueueAge = max(time.Since(p.queue[0].enqueuedAt), 0)
	}
	maps.Copy(snapshot.Outcomes, p.outcomes)
	snapshot.Finalizer = p.finalizer.Snapshot()
	return snapshot
}

// TrySubmit은 작업을 기다리지 않고 queue에 admission한다.
// false이면 pool은 Finalize를 호출하지 않으며 callback ownership은 호출자에게 남는다.
// rejected callback도 pool에 위임해야 하는 caller는 TrySubmitResult를 사용한다.
func (p *ManagedPool) TrySubmit(spec JobSpec) bool {
	return p.trySubmit(spec, false).Accepted
}

// TrySubmitResult는 TrySubmit과 같은 admission을 수행하고 Finalize callback의 ownership을 반환한다.
func (p *ManagedPool) TrySubmitResult(spec JobSpec) ManagedSubmitResult {
	return p.trySubmit(spec, true)
}

func (p *ManagedPool) trySubmit(spec JobSpec, claimRejectedFinalizer bool) ManagedSubmitResult {
	if p == nil {
		return ManagedSubmitResult{Reason: ManagedSubmitRejected}
	}
	job := &managedJob{spec: spec, enqueuedAt: time.Now()}
	if spec.MaxQueueAge > 0 {
		job.expiresAt = job.enqueuedAt.Add(spec.MaxQueueAge)
	}
	if spec.Finalize != nil {
		job.finalizerReserved = p.finalizer.Reserve(spec.Kind)
		if !job.finalizerReserved {
			p.finalizeJob(job, JobOutcomeRejected)
			return ManagedSubmitResult{Reason: ManagedSubmitRejectedFinalizerCapacity}
		}
	}
	if spec.Run == nil {
		return p.rejectSubmission(job, claimRejectedFinalizer)
	}

	p.mu.Lock()
	if p.closed || len(p.queue) >= p.queueSize {
		p.mu.Unlock()
		return p.rejectSubmission(job, claimRejectedFinalizer)
	}
	p.queue = append(p.queue, job)
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

func (p *ManagedPool) rejectSubmission(job *managedJob, claimFinalizer bool) ManagedSubmitResult {
	if claimFinalizer {
		p.finalizeJob(job, JobOutcomeRejected)
		return rejectedSubmitResult(job.finalizerReserved)
	}

	job.finalizeOnce.Do(func() {
		p.mu.Lock()
		p.outcomes[JobOutcomeRejected]++
		p.mu.Unlock()
		p.finalizer.Release(job.finalizerReserved)
		job.finalizerReserved = false
	})
	return ManagedSubmitResult{Reason: ManagedSubmitRejected}
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

// CloseContext는 admission을 닫고 queued job을 drop하며 in-flight job을 취소한다.
// nil을 반환하면 pool이 소유한 accepted/rejected Finalize callback이 모두 실제로 반환했고
// finalizer reservation도 모두 해제된 상태다. callback이 context를 무시하면 호출자 context로
// bounded하게 반환하되 성공을 보고하지 않는다.
func (p *ManagedPool) CloseContext(ctx context.Context) error {
	if p == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
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
	for _, cancel := range p.inFlight {
		cancels = append(cancels, cancel)
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
	defer p.workerWG.Done()
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
	p.inFlight[job] = shutdownCancel
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
	defer p.reaperWG.Done()
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
		p.mu.Unlock()
		p.finalizer.Schedule(job.spec, outcome, job.finalizerReserved)
	})
}

func newManagedFinalizer(concurrency, queueSize int, timeout time.Duration, logger *slog.Logger) *managedFinalizer {
	finalizer := &managedFinalizer{
		queue: make([]*managedFinalizerTask, 0, queueSize),
		done:  make(chan struct{}),
		snapshot: ManagedFinalizerSnapshot{
			Concurrency: concurrency,
			QueueSize:   queueSize,
			Accepting:   true,
		},
		timeout: timeout,
		logger:  logger,
	}
	return finalizer
}

func (f *managedFinalizer) Reserve(kind string) bool {
	f.mu.Lock()
	if !f.closed && f.snapshot.Reservations < f.snapshot.QueueSize {
		f.snapshot.Reservations++
		f.pending++
		f.mu.Unlock()
		return true
	}
	reason := "closed"
	if !f.closed {
		f.snapshot.Overloaded++
		reason = "capacity"
	}
	f.snapshot.ReservationRejected++
	f.mu.Unlock()
	f.logger.Warn(
		"managed_worker_finalize_reservation_rejected",
		slog.String("kind", kind),
		slog.String("reason", reason),
	)
	return false
}

func (f *managedFinalizer) Release(reserved bool) {
	if !reserved {
		return
	}

	f.mu.Lock()
	f.pending--
	f.releaseReservationLocked()
	tasks := f.dispatchLocked()
	f.mu.Unlock()
	f.start(tasks)
}

func (f *managedFinalizer) Schedule(spec JobSpec, outcome JobOutcome, reserved bool) {
	if spec.Finalize == nil || !reserved {
		return
	}
	base := spec.Context
	if base == nil {
		base = context.Background()
	}
	task := &managedFinalizerTask{
		base:     base,
		kind:     spec.Kind,
		outcome:  outcome,
		callback: spec.Finalize,
	}

	f.mu.Lock()
	f.pending--
	f.snapshot.Claimed++
	f.queue = append(f.queue, task)
	tasks := f.dispatchLocked()
	f.mu.Unlock()
	f.start(tasks)
}

func (f *managedFinalizer) Close() <-chan struct{} {
	f.mu.Lock()
	f.closed = true
	f.snapshot.Accepting = false
	tasks := f.dispatchLocked()
	done := f.done
	f.mu.Unlock()
	f.start(tasks)

	return done
}

func (f *managedFinalizer) Snapshot() ManagedFinalizerSnapshot {
	f.mu.Lock()
	defer f.mu.Unlock()
	snapshot := f.snapshot
	snapshot.QueueDepth = len(f.queue)
	return snapshot
}

func (f *managedFinalizer) dispatchLocked() []*managedFinalizerTask {
	available := f.snapshot.Concurrency - f.snapshot.InFlight
	count := min(len(f.queue), available)
	tasks := make([]*managedFinalizerTask, 0, count)
	for range count {
		task := f.queue[0]
		f.queue[0] = nil
		f.queue = f.queue[1:]
		f.snapshot.InFlight++
		tasks = append(tasks, task)
	}
	if f.closed && f.pending == 0 && len(f.queue) == 0 {
		f.snapshot.DispatchDrained = true
	}
	f.markQuiescedLocked()
	return tasks
}

func (f *managedFinalizer) markQuiescedLocked() {
	if !f.closed || f.pending != 0 || len(f.queue) != 0 || f.snapshot.InFlight != 0 || f.snapshot.Reservations != 0 || f.snapshot.Quiesced {
		return
	}
	f.snapshot.Quiesced = true
	close(f.done)
}

func (f *managedFinalizer) start(tasks []*managedFinalizerTask) {
	for _, task := range tasks {
		go f.execute(task)
	}
}

func (f *managedFinalizer) execute(task *managedFinalizerTask) {
	started := make(chan managedFinalizerStart, 1)
	completed := make(chan managedFinalizerResult, 1)
	go func() {
		f.mu.Lock()
		f.snapshot.Started++
		f.mu.Unlock()
		ctx, cancel := context.WithTimeout(context.WithoutCancel(task.base), f.timeout)
		deadline, _ := ctx.Deadline()
		started <- managedFinalizerStart{ctx: ctx, cancel: cancel, deadline: deadline}

		result := managedFinalizerResult{}
		defer func() {
			if recovered := recover(); recovered != nil {
				result.panicked = recovered
				result.stack = debug.Stack()
			}
			result.completedAt = time.Now()
			completed <- result
		}()
		task.callback(ctx, task.outcome)
	}()

	start := <-started
	defer start.cancel()
	select {
	case result := <-completed:
		f.recordCompletion(task, result, start.deadline, false)
	case <-start.ctx.Done():
		select {
		case result := <-completed:
			f.recordCompletion(task, result, start.deadline, false)
		default:
			f.recordTimeout(task)
			f.recordCompletion(task, <-completed, start.deadline, true)
		}
	}
}

func (f *managedFinalizer) recordTimeout(task *managedFinalizerTask) {
	f.mu.Lock()
	f.snapshot.TimedOut++
	f.mu.Unlock()
	f.logger.Warn(
		"managed_worker_finalize_timed_out",
		slog.String("kind", task.kind),
		slog.String("outcome", string(task.outcome)),
	)
}

func (f *managedFinalizer) recordCompletion(
	task *managedFinalizerTask,
	result managedFinalizerResult,
	deadline time.Time,
	timedOut bool,
) {
	completedLate := !result.completedAt.Before(deadline)
	if completedLate && !timedOut {
		f.recordTimeout(task)
	}

	f.mu.Lock()
	f.snapshot.InFlight--
	f.releaseReservationLocked()
	if result.panicked != nil {
		f.snapshot.Panicked++
	} else if completedLate {
		f.snapshot.CompletedLate++
	} else {
		f.snapshot.Completed++
	}
	tasks := f.dispatchLocked()
	f.mu.Unlock()
	f.start(tasks)

	if result.panicked != nil {
		f.logger.Error(
			"managed_worker_finalize_panicked",
			slog.String("kind", task.kind),
			slog.String("outcome", string(task.outcome)),
			slog.Any("panic", fmt.Sprintf("%v", result.panicked)),
			slog.String("stack", string(result.stack)),
		)
		return
	}
	if completedLate {
		f.logger.Warn(
			"managed_worker_finalize_completed_late",
			slog.String("kind", task.kind),
			slog.String("outcome", string(task.outcome)),
		)
	}
}

func (f *managedFinalizer) releaseReservationLocked() {
	f.snapshot.Reservations--
}

func signalManagedPool(ch chan struct{}) {
	select {
	case ch <- struct{}{}:
	default:
	}
}
