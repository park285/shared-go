package workerpool

import (
	"context"
	"sync"
	"time"

	"github.com/panjf2000/ants/v2"
)

type Pool struct {
	pool *ants.Pool
	wg   sync.WaitGroup
}

type Config struct {
	Size            int
	PreAlloc        bool
	ExpiryDuration  time.Duration
	Nonblocking     bool
	MaxBlockingTask int
}

func DefaultConfig() Config {
	return Config{
		Size:            10,
		PreAlloc:        false,
		ExpiryDuration:  time.Second * 10,
		Nonblocking:     false,
		MaxBlockingTask: 0,
	}
}

func New(config Config) (*Pool, error) {
	opts := []ants.Option{
		ants.WithExpiryDuration(config.ExpiryDuration),
		ants.WithNonblocking(config.Nonblocking),
	}
	if config.PreAlloc {
		opts = append(opts, ants.WithPreAlloc(true))
	}
	if config.MaxBlockingTask > 0 {
		opts = append(opts, ants.WithMaxBlockingTasks(config.MaxBlockingTask))
	}

	pool, err := ants.NewPool(config.Size, opts...)
	if err != nil {
		return nil, err
	}
	return &Pool{pool: pool}, nil
}

func (p *Pool) Submit(task func()) error {
	p.wg.Add(1)
	err := p.pool.Submit(func() {
		defer p.wg.Done()
		task()
	})
	if err != nil {
		p.wg.Done()
		return err
	}
	return nil
}

func (p *Pool) Running() int {
	return p.pool.Running()
}

func (p *Pool) Cap() int {
	return p.pool.Cap()
}

func (p *Pool) Free() int {
	return p.pool.Free()
}

func (p *Pool) Wait() {
	p.wg.Wait()
}

func (p *Pool) Shutdown() {
	p.pool.Release()
}

func (p *Pool) ShutdownWait(ctx context.Context) error {
	done := make(chan struct{})
	go func() {
		p.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		p.pool.Release()
		return nil
	case <-ctx.Done():
		gracePeriod := 5 * time.Second
		if deadline, ok := ctx.Deadline(); ok {
			remaining := time.Until(deadline)
			gracePeriod = max(remaining,
				0)
		}

		_ = p.pool.ReleaseTimeout(gracePeriod) //nolint:errcheck
		return ctx.Err()
	}
}
