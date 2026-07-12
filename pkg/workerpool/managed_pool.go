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

// ManagedConfig는 ManagedPool의 고정 worker, queue, cleanup budget을 설정한다.
type ManagedConfig struct {
	Workers         int
	QueueSize       int
	FinalizeTimeout time.Duration
	Logger          *slog.Logger
}

// ManagedSnapshot은 pool의 현재 queue, 실행, outcome 상태를 복사해 제공한다.
type ManagedSnapshot struct {
	QueueDepth     int
	InFlight       int
	OldestQueueAge time.Duration
	Outcomes       map[JobOutcome]uint64
}

type managedJob struct {
	spec         JobSpec
	enqueuedAt   time.Time
	expiresAt    time.Time
	finalizeOnce sync.Once
}

// ManagedPool은 dequeue-time budget과 단일 finalization을 소유하는 worker pool이다.
type ManagedPool struct {
	mu              sync.Mutex
	queue           []*managedJob
	queueSize       int
	closed          bool
	workAvailable   *sync.Cond
	reaperNotify    chan struct{}
	stopCh          chan struct{}
	shutdownDone    chan struct{}
	shutdownOnce    sync.Once
	workerWG        sync.WaitGroup
	reaperWG        sync.WaitGroup
	inFlight        map[*managedJob]context.CancelCauseFunc
	outcomes        map[JobOutcome]uint64
	finalizeTimeout time.Duration
	logger          *slog.Logger
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
	if config.Logger == nil {
		config.Logger = slog.Default()
	}

	pool := &ManagedPool{
		queue:           make([]*managedJob, 0, config.QueueSize),
		queueSize:       config.QueueSize,
		reaperNotify:    make(chan struct{}, 1),
		stopCh:          make(chan struct{}),
		shutdownDone:    make(chan struct{}),
		inFlight:        make(map[*managedJob]context.CancelCauseFunc, config.Workers),
		outcomes:        make(map[JobOutcome]uint64),
		finalizeTimeout: config.FinalizeTimeout,
		logger:          config.Logger,
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
	return snapshot
}

// TrySubmit은 작업을 기다리지 않고 queue에 admission한다.
func (p *ManagedPool) TrySubmit(spec JobSpec) bool {
	if p == nil {
		return false
	}
	job := &managedJob{spec: spec, enqueuedAt: time.Now()}
	if spec.MaxQueueAge > 0 {
		job.expiresAt = job.enqueuedAt.Add(spec.MaxQueueAge)
	}
	if spec.Run == nil {
		p.finalizeJob(job, JobOutcomeRejected)
		return false
	}

	p.mu.Lock()
	if p.closed || len(p.queue) >= p.queueSize {
		p.mu.Unlock()
		p.finalizeJob(job, JobOutcomeRejected)
		return false
	}
	p.queue = append(p.queue, job)
	p.workAvailable.Signal()
	p.mu.Unlock()
	if !job.expiresAt.IsZero() {
		signalManagedPool(p.reaperNotify)
	}
	return true
}

// CloseContext는 admission을 닫고 queued job을 drop하며 in-flight job을 취소한다.
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
		if job.spec.Finalize == nil {
			return
		}
		base := job.spec.Context
		if base == nil {
			base = context.Background()
		}
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(base), p.finalizeTimeout)
		defer cancel()
		defer func() {
			if recovered := recover(); recovered != nil {
				p.logger.Error(
					"managed_worker_finalize_panicked",
					slog.String("kind", job.spec.Kind),
					slog.String("outcome", string(outcome)),
					slog.Any("panic", fmt.Sprintf("%v", recovered)),
					slog.String("stack", string(debug.Stack())),
				)
			}
		}()
		job.spec.Finalize(cleanupCtx, outcome)
	})
}

func signalManagedPool(ch chan struct{}) {
	select {
	case ch <- struct{}{}:
	default:
	}
}
