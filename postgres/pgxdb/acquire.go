package pgxdb

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/multitracer"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/sync/semaphore"
)

type AcquirePolicy struct {
	ReservedConns int32
	Timeout       time.Duration
}

type controlAcquireKey struct{}

func WithControlAcquire(ctx context.Context) context.Context {
	return context.WithValue(ctx, controlAcquireKey{}, true)
}

func validateAcquirePolicy(maxConns int32, policy *AcquirePolicy) error {
	if policy != nil && (policy.ReservedConns < 0 || policy.ReservedConns >= maxConns || policy.Timeout <= 0) {
		return errors.New("pgxdb: acquire policy requires 0 <= reserved conns < max conns and positive timeout")
	}

	return nil
}

func installAcquirePolicy(cfg *pgxpool.Config, policy *AcquirePolicy) error {
	if policy == nil {
		return nil
	}

	if err := validateAcquirePolicy(cfg.MaxConns, policy); err != nil {
		return err
	}

	p := *policy

	gate := &acquireGate{timeout: p.Timeout}
	if p.ReservedConns > 0 {
		ordinaryCapacity := int64(cfg.MaxConns - p.ReservedConns)

		gate.permits = semaphore.NewWeighted(ordinaryCapacity)
		gate.held = make(map[*pgx.Conn]struct{})
	}

	tracer := &multitracer.Tracer{}

	if cfg.ConnConfig.Tracer != nil {
		tracer = multitracer.New(cfg.ConnConfig.Tracer)
	}

	tracer.PoolAcquireTracers = append(tracer.PoolAcquireTracers, gate)
	tracer.PoolReleaseTracers = append(tracer.PoolReleaseTracers, gate)
	cfg.ConnConfig.Tracer = tracer

	return nil
}

type acquireGate struct {
	timeout time.Duration
	permits *semaphore.Weighted
	mu      sync.Mutex
	held    map[*pgx.Conn]struct{}
}

type acquireContext struct {
	context.Context //nolint:containedctx // 단일 Acquire의 Context 구현이며 풀이나 워커에 보관하지 않는다.

	cancel context.CancelFunc
	permit bool
}

type acquireContextKey struct{}

func (ctx *acquireContext) Value(key any) any {
	if _, ok := key.(acquireContextKey); ok {
		return ctx
	}

	return ctx.Context.Value(key)
}

func (g *acquireGate) TraceAcquireStart(ctx context.Context, _ *pgxpool.Pool, _ pgxpool.TraceAcquireStartData) context.Context {
	bounded, cancel := context.WithTimeout(ctx, g.timeout)
	state := &acquireContext{Context: bounded, cancel: cancel}

	if g.permits == nil || ctx.Value(controlAcquireKey{}) == true {
		return state
	}

	if err := g.permits.Acquire(bounded, 1); err != nil {
		return state
	}

	if bounded.Err() != nil {
		g.permits.Release(1)

		return state
	}

	state.permit = true

	return state
}

func (g *acquireGate) TraceAcquireEnd(ctx context.Context, _ *pgxpool.Pool, data pgxpool.TraceAcquireEndData) {
	state, ok := ctx.Value(acquireContextKey{}).(*acquireContext)
	if !ok {
		panic("pgxdb: acquire tracing context missing")
	}

	defer state.cancel()

	if !state.permit {
		return
	}

	if data.Err != nil || data.Conn == nil {
		g.permits.Release(1)

		return
	}

	g.mu.Lock()

	g.held[data.Conn] = struct{}{}
	g.mu.Unlock()
}

func (g *acquireGate) TraceRelease(_ *pgxpool.Pool, data pgxpool.TraceReleaseData) {
	if g.permits == nil {
		return
	}

	g.mu.Lock()

	_, held := g.held[data.Conn]
	delete(g.held, data.Conn)
	g.mu.Unlock()

	if held {
		g.permits.Release(1)
	}
}
