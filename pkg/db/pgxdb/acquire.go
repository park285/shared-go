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

// AcquirePolicy bounds acquisition, not SQL execution. Ordinary acquisitions
// share MaxConns-ReservedConns permits; control acquisitions still obey MaxConns.
// ReservedConns must be nonnegative and less than the effective pool maximum,
// and Timeout must be positive. A nil Options.AcquirePolicy preserves pgx behavior.
// AcquireAllIdle and Conn.Hijack are unsupported on policy-enabled pools.
type AcquirePolicy struct {
	ReservedConns int32
	Timeout       time.Duration
}

type controlAcquireKey struct{}

// WithControlAcquire marks a bounded, short lease, renewal, settlement or
// finalization operation. It preserves parent cancellation, deadlines and values,
// and grants no database privilege. Do not mark a worker, request, provider or
// service lifecycle, admission, reads, batches or maintenance operations.
func WithControlAcquire(ctx context.Context) context.Context {
	return context.WithValue(ctx, controlAcquireKey{}, true)
}

func installAcquirePolicy(cfg *pgxpool.Config, policy *AcquirePolicy) error {
	if policy == nil {
		return nil
	}

	p := *policy
	if p.ReservedConns < 0 || p.ReservedConns >= cfg.MaxConns || p.Timeout <= 0 {
		return errors.New("pgxdb: acquire policy requires 0 <= reserved conns < max conns and positive timeout")
	}

	gate := &acquireGate{timeout: p.Timeout}
	if p.ReservedConns > 0 {
		ordinaryCapacity := int64(cfg.MaxConns - p.ReservedConns)

		gate.permits = semaphore.NewWeighted(ordinaryCapacity)
		gate.held = make(map[*pgx.Conn]struct{})
	}

	// Preserve every optional pgx tracer interface, not just query tracing. Put
	// the gate last so instrumentation sees acquisition completion before the
	// temporary acquire context is canceled. pgx uses the original SQL context.
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

// The state belongs to one Acquire, never to a caller's reusable context. The
// connection map contains only successful ordinary leases, bounded by permits.
// Embedding the context avoids another WithValue allocation on each acquisition.
type acquireContext struct {
	context.Context //nolint:containedctx // This implements Context for one Acquire; it is never retained on a pool or worker.

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

	// Puddle checks this context before physical acquire; do not retain a
	// permit when cancellation races with ordinary admission.
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

	// pgx runs this before exposing the connection. Internal health/prepare
	// failures do not call TraceRelease; one permit spans those native attempts.
	g.mu.Lock()

	g.held[data.Conn] = struct{}{}
	g.mu.Unlock()
}

func (g *acquireGate) TraceRelease(_ *pgxpool.Pool, data pgxpool.TraceReleaseData) {
	if g.permits == nil {
		return
	}

	// pgx invokes this before returning OR destroying the physical resource,
	// including busy/closed connections, expiration and reset. Hijack skips it.
	g.mu.Lock()

	_, held := g.held[data.Conn]
	delete(g.held, data.Conn)
	g.mu.Unlock()

	if held {
		g.permits.Release(1)
	}
}
