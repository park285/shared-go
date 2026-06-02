package workerpool

import (
	"context"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync"
)

type QueuedConfig struct {
	Workers   int
	QueueSize int
}

type QueuedPool struct {
	queue     chan func()
	stopCh    chan struct{}
	mu        sync.RWMutex
	closed    bool
	workerWG  sync.WaitGroup
	stopOnce  sync.Once
	workers   int
	queueSize int
	logger    *slog.Logger
}

func NewQueued(config QueuedConfig) *QueuedPool {
	return NewQueuedWithLogger(config, slog.Default())
}

// NewQueuedWithLogger는 panic recover 로그에 사용할 logger를 주입한다.
// logger가 nil이면 slog.Default()를 사용한다.
func NewQueuedWithLogger(config QueuedConfig, logger *slog.Logger) *QueuedPool {
	if config.Workers < 1 {
		config.Workers = 1
	}
	if config.QueueSize < 1 {
		config.QueueSize = 1
	}
	if logger == nil {
		logger = slog.Default()
	}

	p := &QueuedPool{
		queue:     make(chan func(), config.QueueSize),
		stopCh:    make(chan struct{}),
		workers:   config.Workers,
		queueSize: config.QueueSize,
		logger:    logger,
	}
	for range config.Workers {
		p.workerWG.Add(1)
		go p.worker()
	}

	return p
}

func (p *QueuedPool) TrySubmit(task func()) bool {
	if p == nil || task == nil {
		return false
	}

	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.closed {
		return false
	}

	select {
	case p.queue <- task:
		return true
	default:
		return false
	}
}

func (p *QueuedPool) SubmitWait(task func()) bool {
	if p == nil || task == nil {
		return false
	}

	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.closed {
		return false
	}

	select {
	case p.queue <- task:
		return true
	case <-p.stopCh:
		return false
	}
}

func (p *QueuedPool) StopAndWait() {
	if p == nil {
		return
	}

	p.stopOnce.Do(func() {
		close(p.stopCh)
		p.mu.Lock()
		p.closed = true
		close(p.queue)
		p.mu.Unlock()
	})
	p.workerWG.Wait()
}

func (p *QueuedPool) StopAndWaitContext(ctx context.Context) error {
	if p == nil {
		return nil
	}

	p.stopOnce.Do(func() {
		close(p.stopCh)
		p.mu.Lock()
		p.closed = true
		close(p.queue)
		p.mu.Unlock()
	})

	done := make(chan struct{})
	go func() {
		p.workerWG.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (p *QueuedPool) Workers() int {
	if p == nil {
		return 0
	}
	return p.workers
}

func (p *QueuedPool) QueueSize() int {
	if p == nil {
		return 0
	}
	return p.queueSize
}

func (p *QueuedPool) Pending() int {
	if p == nil {
		return 0
	}
	return len(p.queue)
}

func (p *QueuedPool) worker() {
	defer p.workerWG.Done()

	for task := range p.queue {
		if task == nil {
			continue
		}
		p.safeRun(task)
	}
}

func (p *QueuedPool) safeRun(task func()) {
	defer func() {
		if r := recover(); r != nil {
			stack := debug.Stack()
			p.logger.Error("worker: task panicked",
				slog.Any("panic", fmt.Sprintf("%v", r)),
				slog.String("stack", string(stack)),
			)
		}
	}()
	task()
}
