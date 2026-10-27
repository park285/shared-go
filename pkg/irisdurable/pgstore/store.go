package pgstore

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/park285/shared-go/v2/pkg/db/pgxdb"
	"github.com/park285/shared-go/v2/pkg/irisdurable"
)

// Querier는 pgxpool.Pool과 pgx.Tx가 함께 만족하는 최소 실행 계약이다.
type Querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// 인터페이스 txQuerier는 이미 트랜잭션 안에 있는 Querier다. 이 계약은 pgx.Tx가 만족한다.
type txQuerier interface {
	Querier

	Commit(ctx context.Context) error
	Rollback(ctx context.Context) error
}

// 인터페이스 beginner는 자기 트랜잭션을 열 수 있는 Querier다. 이 계약은 pgxpool.Pool이 만족한다.
type beginner interface {
	Querier

	Begin(ctx context.Context) (pgx.Tx, error)
}

var (
	// ErrNotFound는 주어진 식별자의 행이 없을 때 반환한다.
	ErrNotFound = errors.New("pgstore: row not found")
	// ErrClaimLost는 claim fence가 맞지 않아 상태를 바꾸지 못했을 때 반환한다.
	ErrClaimLost = errors.New("pgstore: claim token no longer owns the row")
)

// Options는 저장소의 정책 값이다. 라이브러리 기본값은 없다. 0인 정책은 그 값을 쓰는 연산이 호출될 때
// 오류를 반환하므로, nonce 전용·stage 전용 Store는 쓰지 않는 정책 없이 만들 수 있다. 0이 아닌 값은
// New가 검증한다. 같은 테이블을 여러 Store가 다루면 각 Store에 같은 정책을 넘긴다.
type Options struct {
	// Scope는 한 데이터베이스가 여러 봇이나 게임을 담을 때 행을 구분한다. 빈 값도 유효한 scope다.
	Scope string
	// IrisAdmissionRetention은 Iris reply admission 원장의 보존 기간이다. 정본은 Iris 계약 파일과
	// iris-client-go 공개 상수이며 호출자가 넘긴다. 보존·지평 항목을 하나라도 지정하면 필수다.
	IrisAdmissionRetention time.Duration
	// Lease는 claim 소유권의 유효 기간이다. Claim·ClaimBatch·RenewInbox·BeginAttempt가 쓴다.
	Lease time.Duration
	// MaxAttempts는 한 reply 행의 재시도 상한이다. 이 값에 도달하면 Retire가 종단으로 보낸다.
	// BeginAttempt·Redrive·Retire·ReplyReadySnapshot이 쓴다.
	MaxAttempts int
	// BackoffBase와 BackoffMax는 BackoffFor의 지수 백오프 범위다. 둘을 함께 지정한다.
	BackoffBase time.Duration
	BackoffMax  time.Duration
	// AutomaticReplayHorizon은 저장된 clientRequestId로 자동 재발송을 허용하는 상한이다.
	// IrisAdmissionRetention보다 짧아야 하며 BeginAttempt·Redrive·Retire·ReplyReadySnapshot이 쓴다.
	AutomaticReplayHorizon time.Duration
	// ReplyRetention은 reply outbox 행의 보존이다. IrisAdmissionRetention보다 짧을 수 없고 Stage가 쓴다.
	ReplyRetention time.Duration
	// InboxTerminalRetention은 inbox 종단 행의 보존이다. IrisAdmissionRetention보다 길어야 하며
	// PruneInbox·PruneInboxBefore가 쓴다. 같으면 보존 경계에서 지운 행의 재전송이 다시 실행된다.
	InboxTerminalRetention time.Duration
	// InboxManualReviewRetention은 inbox manual_review 행의 보존이다. 사람이 볼 payload를 남기는
	// 갈래라 종단 보존과 따로 잡는다. 0이면 PruneInbox가 이 갈래를 지우지 않는다.
	InboxManualReviewRetention time.Duration
	// InboxExplicitRecovery는 만료 claim을 Claim이 직접 다시 잡지 않게 하고, 소비자가 ApplyInboxRecovery로
	// 판정한 뒤에만 재처리한다. consumer가 복구 시 attempt 상한과 dead-letter 관측을 소유할 때 사용한다.
	InboxExplicitRecovery bool
	// DiscardInboxManualReviewPayload는 manual_review에도 본문을 남기지 않는 소비자의 보존 계약이다.
	// 원인과 identity는 유지하며 이미 지운 본문을 재구성하지 않는다.
	DiscardInboxManualReviewPayload bool
}

// validate는 지정한 정책 값이 스택 보존 계약을 어기는지 기동 시점에 검사한다.
func (o Options) validate() error {
	if o.IrisAdmissionRetention < 0 || o.Lease < 0 || o.MaxAttempts < 0 || o.BackoffBase < 0 || o.BackoffMax < 0 ||
		o.AutomaticReplayHorizon < 0 || o.ReplyRetention < 0 || o.InboxTerminalRetention < 0 || o.InboxManualReviewRetention < 0 {
		return errors.New("pgstore: options must not be negative")
	}

	if (o.BackoffBase > 0) != (o.BackoffMax > 0) {
		return errors.New("pgstore: BackoffBase and BackoffMax must be set together")
	}

	if o.BackoffMax < o.BackoffBase {
		return fmt.Errorf("pgstore: BackoffMax %s is shorter than BackoffBase %s", o.BackoffMax, o.BackoffBase)
	}

	if err := o.validateRetention(); err != nil {
		return err
	}

	// manual_review는 사람이 처리할 때까지 남기는 갈래이므로 종단 보존보다 짧으면 안 된다. 짧으면
	// 아직 검토하지 않은 행이 이미 검토가 끝난 completed 행보다 먼저 사라진다.
	if o.InboxManualReviewRetention > 0 && o.InboxManualReviewRetention < o.InboxTerminalRetention {
		return fmt.Errorf("pgstore: InboxManualReviewRetention %s is shorter than InboxTerminalRetention %s", o.InboxManualReviewRetention, o.InboxTerminalRetention)
	}

	return nil
}

func (o Options) validateRetention() error {
	admission := o.IrisAdmissionRetention
	if admission == 0 {
		if o.AutomaticReplayHorizon > 0 || o.ReplyRetention > 0 || o.InboxTerminalRetention > 0 || o.InboxManualReviewRetention > 0 {
			return errors.New("pgstore: IrisAdmissionRetention is required with retention or replay horizon options")
		}

		return nil
	}

	// 지평 끝에서 재발송한 요청도 Iris가 같은 clientRequestId를 아직 기억해야 중복 발화가 없다.
	if o.AutomaticReplayHorizon >= admission {
		return fmt.Errorf("pgstore: AutomaticReplayHorizon %s must be shorter than the Iris admission retention %s", o.AutomaticReplayHorizon, admission)
	}

	if o.ReplyRetention > 0 && o.ReplyRetention < admission {
		return fmt.Errorf("pgstore: ReplyRetention %s is shorter than the Iris admission retention %s", o.ReplyRetention, admission)
	}

	// 종단 inbox 행은 Iris가 같은 메시지를 다시 보낼 수 있는 동안 dedup 근거로 남아야 한다.
	// 스택 계약은 봇 종단 보존이 Iris admission 보존보다 클 것을 요구한다.
	if o.InboxTerminalRetention > 0 && o.InboxTerminalRetention <= admission {
		return fmt.Errorf("pgstore: InboxTerminalRetention %s must exceed the Iris admission retention %s; a redelivered webhook would re-execute instead of dedup", o.InboxTerminalRetention, admission)
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

// New는 Querier와 정책으로 Store를 만든다. 지정한 정책이 스택 보존 계약을 어기면 오류를 반환한다.
func New(db Querier, opts Options) (*Store, error) {
	if db == nil {
		return nil, errors.New("pgstore: querier is required")
	}

	if err := opts.validate(); err != nil {
		return nil, err
	}

	return &Store{db: db, opts: opts}, nil
}

// Options는 New에 넘긴 정책이다.
func (s *Store) Options() Options {
	return s.opts
}

// inTransaction은 여러 문이 한 트랜잭션에서 돌아야 하는 연산을 실행한다. 이미 트랜잭션 안이면
// 그 트랜잭션을 그대로 쓰고(호출자가 commit 시점을 소유한다), pool이면 자기 트랜잭션을 연다.
func (s *Store) inTransaction(ctx context.Context, action string, run func(Querier) error) (resultErr error) {
	switch db := s.db.(type) {
	case txQuerier:
		return run(db)
	case beginner:
		tx, err := db.Begin(ctx)
		if err != nil {
			return fmt.Errorf("pgstore: begin %s: %w", action, err)
		}

		defer pgxdb.RollbackDeferred(ctx, tx, &resultErr)

		if err := run(tx); err != nil {
			return err
		}

		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("pgstore: commit %s: %w", action, err)
		}

		return nil
	default:
		return fmt.Errorf("pgstore: %s needs a querier that is a transaction or can begin one", action)
	}
}

// requirePolicy는 연산이 쓰는 정책 값이 지정됐는지 확인한다. 기본값으로 채우지 않으므로 빠진 값은
// 호출 시점 오류로 드러난다.
func (s *Store) requirePolicy(action string, lease, attempts, horizon bool) error {
	switch {
	case lease && s.opts.Lease <= 0:
		return fmt.Errorf("pgstore: %s requires Options.Lease", action)
	case attempts && s.opts.MaxAttempts <= 0:
		return fmt.Errorf("pgstore: %s requires Options.MaxAttempts", action)
	case horizon && s.opts.AutomaticReplayHorizon <= 0:
		return fmt.Errorf("pgstore: %s requires Options.AutomaticReplayHorizon", action)
	default:
		return nil
	}
}

// backoff는 attempt번째 재시도의 대기 시간이다.
func (s *Store) backoff(attempt int) time.Duration {
	shift := min(max(attempt-1, 0), irisdurable.RetryBackoffMaxShift)

	delay := s.opts.BackoffBase << uint(shift)
	if delay > s.opts.BackoffMax || delay <= 0 {
		delay = s.opts.BackoffMax
	}

	return delay
}
