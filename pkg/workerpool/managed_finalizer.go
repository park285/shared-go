package workerpool

import (
	"context"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync"
	"time"
)

type finalizerReserveReason int

const (
	finalizerReserveAccepted finalizerReserveReason = iota
	finalizerReserveRejectedCapacity
	finalizerReserveRejectedClosed
)

type managedFinalizerTask struct {
	base     context.Context //nolint:containedctx // dispatch까지 queue에 머무는 callback task라, 값 전용 base context를 필드로 운반해야 한다.
	kind     string
	outcome  JobOutcome
	callback func(context.Context, JobOutcome)
}

type managedFinalizerStart struct {
	ctx      context.Context //nolint:containedctx // callback goroutine이 만든 deadline context를 감시 goroutine으로 1회 넘기는 채널 메시지다. FinalizeTimeout을 callback 실제 시작 시점부터 재려면 이 handoff가 필요하다.
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
	queue    boundedQueue[*managedFinalizerTask]
	snapshot ManagedFinalizerSnapshot
	closed   bool
	done     chan struct{}
	pending  int
	timeout  time.Duration
	logger   *slog.Logger
}

func newManagedFinalizer(concurrency, queueSize int, timeout time.Duration, logger *slog.Logger) *managedFinalizer {
	finalizer := &managedFinalizer{
		queue: newBoundedQueue[*managedFinalizerTask](queueSize),
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

func (f *managedFinalizer) Reserve(kind string) (bool, finalizerReserveReason) {
	f.mu.Lock()

	if !f.closed && f.snapshot.Reservations < f.snapshot.QueueSize {
		f.snapshot.Reservations++

		f.pending++
		f.mu.Unlock()

		return true, finalizerReserveAccepted
	}

	reason := "closed"
	rejectReason := finalizerReserveRejectedClosed

	if !f.closed {
		f.snapshot.Overloaded++

		reason = "capacity"
		rejectReason = finalizerReserveRejectedCapacity
	}

	f.snapshot.ReservationRejected++
	f.mu.Unlock()
	f.logger.Warn(
		"managed_worker_finalize_reservation_rejected",
		slog.String("kind", kind),
		slog.String("reason", reason),
	)

	return false, rejectReason
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

	if !f.queue.Push(task) {
		panic("workerpool: finalizer reservation exceeded queue capacity")
	}

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

	snapshot.QueueDepth = f.queue.Len()

	return snapshot
}

func (f *managedFinalizer) dispatchLocked() []*managedFinalizerTask {
	available := f.snapshot.Concurrency - f.snapshot.InFlight
	count := min(f.queue.Len(), available)
	tasks := make([]*managedFinalizerTask, 0, count)

	for range count {
		task, _ := f.queue.Pop()
		f.snapshot.InFlight++

		tasks = append(tasks, task)
	}

	if f.closed && f.pending == 0 && f.queue.Len() == 0 {
		f.snapshot.DispatchDrained = true
	}

	f.markQuiescedLocked()

	return tasks
}

func (f *managedFinalizer) markQuiescedLocked() {
	if !f.closed || f.pending != 0 || f.queue.Len() != 0 || f.snapshot.InFlight != 0 || f.snapshot.Reservations != 0 || f.snapshot.Quiesced {
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
			f.recordTimeout(task, true)
			f.recordCompletion(task, <-completed, start.deadline, true)
		}
	}
}

func (f *managedFinalizer) recordTimeout(task *managedFinalizerTask, stillRunning bool) {
	f.mu.Lock()

	f.snapshot.TimedOut++

	if stillRunning {
		f.snapshot.OverdueInFlight++
	}

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
		f.recordTimeout(task, false)
	}

	f.mu.Lock()

	if timedOut {
		f.snapshot.OverdueInFlight--
	}

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
