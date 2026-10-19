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

// AcquirePolicy는 SQL 실행이 아닌 연결 획득을 제한한다. 일반 획득은
// MaxConns-ReservedConns 허가를 공유하며 제어 획득도 MaxConns를 넘지 못한다.
// 0 <= ReservedConns < 유효 풀 최대값, Timeout > 0이어야 한다.
// Options.AcquirePolicy가 nil이면 pgx 동작을 유지한다.
// 정책을 적용한 풀에서는 AcquireAllIdle과 Conn.Hijack을 지원하지 않는다.
type AcquirePolicy struct {
	ReservedConns int32
	Timeout       time.Duration
}

type controlAcquireKey struct{}

// WithControlAcquire는 기한이 짧고 제한된 임대·갱신·정산·종결 작업을 표시한다.
// 부모의 취소·기한·값을 보존하며 DB 권한은 부여하지 않는다.
// 워커·요청·공급자·서비스 수명 전체나 입장·조회·배치·유지보수에는 사용하지 않는다.
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

	// 모든 pgx 추적 인터페이스를 보존한다. 임시 컨텍스트 취소 전에 획득 완료를
	// 관측하도록 게이트를 마지막에 둔다. SQL은 원래 컨텍스트를 사용한다.
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

// 상태는 재사용되는 호출자 컨텍스트가 아닌 단일 Acquire에 속한다.
// 연결 맵은 허가 수 이내의 성공한 일반 임대만 담는다.
// 컨텍스트 임베딩으로 획득마다 추가 WithValue 할당을 피한다.
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

	// Puddle의 실제 획득 전 취소 검사와 입장이 경합해도 허가가 남지 않도록 한다.
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

	// 연결 공개 전에 호출된다. 내부 상태 검사·준비 실패에는 TraceRelease가
	// 호출되지 않으므로 해당 시도들에 하나의 허가를 유지한다.
	g.mu.Lock()

	g.held[data.Conn] = struct{}{}
	g.mu.Unlock()
}

func (g *acquireGate) TraceRelease(_ *pgxpool.Pool, data pgxpool.TraceReleaseData) {
	if g.permits == nil {
		return
	}

	// 사용 중·닫힘·만료·초기화 연결의 실제 반환·폐기 전에 호출된다. Hijack은 예외다.
	g.mu.Lock()

	_, held := g.held[data.Conn]
	delete(g.held, data.Conn)
	g.mu.Unlock()

	if held {
		g.permits.Release(1)
	}
}
