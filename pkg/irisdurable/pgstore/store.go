package pgstore

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/park285/shared-go/v2/pkg/irisdurable"
)

// Querier는 pgxpool.Pool과 pgx.Tx가 함께 만족하는 최소 실행 계약이다.
type Querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

const (
	defaultLease       = 30 * time.Second
	defaultMaxAttempts = 8
	defaultBackoffBase = time.Second
	defaultBackoffMax  = time.Minute
)

var (
	// ErrNotFound는 주어진 식별자의 행이 없을 때 반환한다.
	ErrNotFound = errors.New("pgstore: row not found")
	// ErrClaimLost는 claim fence가 맞지 않아 상태를 바꾸지 못했을 때 반환한다.
	ErrClaimLost = errors.New("pgstore: claim token no longer owns the row")
)

// Options는 저장소의 정책 값이다. 0인 항목은 기본값으로 채운다.
type Options struct {
	// Scope는 한 데이터베이스가 여러 봇이나 게임을 담을 때 행을 구분한다. 빈 값도 유효한 scope다.
	Scope string
	// Lease는 claim 소유권의 유효 기간이다.
	Lease time.Duration
	// MaxAttempts는 한 행의 재시도 상한이다. 이 값에 도달하면 Retire가 종단으로 보낸다.
	MaxAttempts int
	// BackoffBase와 BackoffMax는 재시도 간격의 지수 백오프 범위다.
	BackoffBase time.Duration
	BackoffMax  time.Duration
	// AutomaticReplayHorizon은 저장된 clientRequestId로 자동 재발송을 허용하는 상한이다.
	// irisdurable.AutomaticReplayHorizon을 넘을 수 없다.
	AutomaticReplayHorizon time.Duration
	// ReplyRetention은 reply outbox 행의 보존이다. Iris admission 보존보다 짧을 수 없다.
	ReplyRetention time.Duration
	// InboxTerminalRetention은 inbox 종단 행의 보존이다. 자동 replay 지평보다 짧을 수 없다.
	InboxTerminalRetention time.Duration
	// InboxManualReviewRetention은 inbox manual_review 행의 보존이다. 사람이 볼 payload를 남기는
	// 갈래라 종단 보존과 따로 잡는다. 0이면 PruneInbox가 이 갈래를 지우지 않는다.
	InboxManualReviewRetention time.Duration
}

func (o Options) withDefaults() Options {
	if o.Lease <= 0 {
		o.Lease = defaultLease
	}

	if o.MaxAttempts <= 0 {
		o.MaxAttempts = defaultMaxAttempts
	}

	if o.BackoffBase <= 0 {
		o.BackoffBase = defaultBackoffBase
	}

	if o.BackoffMax <= 0 {
		o.BackoffMax = defaultBackoffMax
	}

	if o.AutomaticReplayHorizon <= 0 {
		o.AutomaticReplayHorizon = irisdurable.AutomaticReplayHorizon
	}

	if o.ReplyRetention <= 0 {
		o.ReplyRetention = irisdurable.ReplyOutboxMinRetention
	}

	// 재전송 창은 Iris가 정하므로 이 저장소의 지평이 아니라 스택 상수를 따른다.
	if o.InboxTerminalRetention <= 0 {
		o.InboxTerminalRetention = irisdurable.AutomaticReplayHorizon
	}

	return o
}

// validate는 스택 보존 계약을 어기는 구성을 기동 시점에 막는다.
func (o Options) validate() error {
	if o.BackoffMax < o.BackoffBase {
		return fmt.Errorf("pgstore: BackoffMax %s is shorter than BackoffBase %s", o.BackoffMax, o.BackoffBase)
	}

	if o.AutomaticReplayHorizon > irisdurable.AutomaticReplayHorizon {
		return fmt.Errorf("pgstore: AutomaticReplayHorizon %s exceeds the stack horizon %s", o.AutomaticReplayHorizon, irisdurable.AutomaticReplayHorizon)
	}

	if o.ReplyRetention < irisdurable.ReplyOutboxMinRetention {
		return fmt.Errorf("pgstore: ReplyRetention %s is shorter than the Iris admission retention %s", o.ReplyRetention, irisdurable.ReplyOutboxMinRetention)
	}

	if o.AutomaticReplayHorizon+irisdurable.ReplayHorizonMargin > o.ReplyRetention {
		return fmt.Errorf("pgstore: AutomaticReplayHorizon %s leaves less than %s before ReplyRetention %s", o.AutomaticReplayHorizon, irisdurable.ReplayHorizonMargin, o.ReplyRetention)
	}

	// 인스턴스 지평을 낮춰도 Iris 재전송 창은 그대로이므로 스택 상수와 비교한다. 호출자 값과
	// 비교하면 지평을 낮춘 구성이 New를 통과하고도 contracttest의 Retention 절에서 떨어진다.
	if o.InboxTerminalRetention < irisdurable.AutomaticReplayHorizon {
		return fmt.Errorf("pgstore: InboxTerminalRetention %s is shorter than the stack replay horizon %s; a redelivered webhook would re-execute instead of dedup", o.InboxTerminalRetention, irisdurable.AutomaticReplayHorizon)
	}

	// manual_review는 사람이 처리할 때까지 남기는 갈래이므로 종단 보존보다 짧으면 안 된다. 짧으면
	// 아직 검토하지 않은 행이 이미 검토가 끝난 completed 행보다 먼저 사라진다.
	if o.InboxManualReviewRetention > 0 && o.InboxManualReviewRetention < o.InboxTerminalRetention {
		return fmt.Errorf("pgstore: InboxManualReviewRetention %s is shorter than InboxTerminalRetention %s", o.InboxManualReviewRetention, o.InboxTerminalRetention)
	}

	return nil
}

// Store는 irisdurable.Admitter·NonceStore·ReplyOutbox의 PostgreSQL 구현이다.
type Store struct {
	db   Querier
	opts Options
}

var (
	_ irisdurable.Admitter    = (*Store)(nil)
	_ irisdurable.NonceStore  = (*Store)(nil)
	_ irisdurable.ReplyOutbox = (*Store)(nil)
)

// New는 Querier와 정책으로 Store를 만든다. 정책이 스택 보존 계약을 어기면 오류를 반환한다.
func New(db Querier, opts Options) (*Store, error) {
	if db == nil {
		return nil, errors.New("pgstore: querier is required")
	}

	resolved := opts.withDefaults()
	if err := resolved.validate(); err != nil {
		return nil, err
	}

	return &Store{db: db, opts: resolved}, nil
}

// Options는 기본값이 채워진 실제 정책이다.
func (s *Store) Options() Options {
	return s.opts
}

// backoffSeconds는 attempt번째 재시도의 대기 시간을 초 단위로 돌려준다.
func (s *Store) backoffSeconds(attempt int) float64 {
	shift := min(max(attempt-1, 0), irisdurable.RetryBackoffMaxShift)

	delay := s.opts.BackoffBase << uint(shift)
	if delay > s.opts.BackoffMax || delay <= 0 {
		delay = s.opts.BackoffMax
	}

	return delay.Seconds()
}
