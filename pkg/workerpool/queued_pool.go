package workerpool

import (
	"context"
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
}

func NewQueued(config QueuedConfig) *QueuedPool {
	if config.Workers < 1 {
		config.Workers = 1
	}
	if config.QueueSize < 1 {
		config.QueueSize = 1
	}

	p := &QueuedPool{
		queue:     make(chan func(), config.QueueSize),
		stopCh:    make(chan struct{}),
		workers:   config.Workers,
		queueSize: config.QueueSize,
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
		task()
	}
}
