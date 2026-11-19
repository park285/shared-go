package pgstore

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/park285/shared-go/postgres/pgxdb"

	"github.com/park285/shared-go/v2/pkg/irisdurable"
)

type Querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type txQuerier interface {
	Querier

	Commit(ctx context.Context) error
	Rollback(ctx context.Context) error
}

type beginner interface {
	Querier

	Begin(ctx context.Context) (pgx.Tx, error)
}

var (
	ErrNotFound  = errors.New("pgstore: row not found")
	ErrClaimLost = errors.New("pgstore: claim token no longer owns the row")
)

type Options struct {
	Scope                           string
	IrisAdmissionRetention          time.Duration
	IrisAutomaticReplayHorizon      time.Duration
	Lease                           time.Duration
	MaxAttempts                     int
	BackoffBase                     time.Duration
	BackoffMax                      time.Duration
	AutomaticReplayHorizon          time.Duration
	ReplyRetention                  time.Duration
	InboxTerminalRetention          time.Duration
	InboxManualReviewRetention      time.Duration
	InboxExplicitRecovery           bool
	DiscardInboxManualReviewPayload bool
}

func (o Options) validate() error {
	if o.IrisAdmissionRetention < 0 || o.IrisAutomaticReplayHorizon < 0 || o.Lease < 0 || o.MaxAttempts < 0 || o.BackoffBase < 0 || o.BackoffMax < 0 ||
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

	if o.InboxManualReviewRetention > 0 && o.InboxManualReviewRetention < o.InboxTerminalRetention {
		return fmt.Errorf("pgstore: InboxManualReviewRetention %s is shorter than InboxTerminalRetention %s", o.InboxManualReviewRetention, o.InboxTerminalRetention)
	}

	return nil
}

func (o Options) validateRetention() error {
	admission := o.IrisAdmissionRetention
	if admission == 0 {
		if o.IrisAutomaticReplayHorizon > 0 || o.AutomaticReplayHorizon > 0 || o.ReplyRetention > 0 || o.InboxTerminalRetention > 0 || o.InboxManualReviewRetention > 0 {
			return errors.New("pgstore: IrisAdmissionRetention is required with retention or replay horizon options")
		}

		return nil
	}

	if o.AutomaticReplayHorizon > 0 && o.IrisAutomaticReplayHorizon == 0 {
		return errors.New("pgstore: IrisAutomaticReplayHorizon is required with AutomaticReplayHorizon")
	}

	if o.IrisAutomaticReplayHorizon >= admission {
		return fmt.Errorf("pgstore: IrisAutomaticReplayHorizon %s must be shorter than the Iris admission retention %s", o.IrisAutomaticReplayHorizon, admission)
	}

	if o.AutomaticReplayHorizon > o.IrisAutomaticReplayHorizon {
		return fmt.Errorf("pgstore: AutomaticReplayHorizon %s exceeds the Iris automatic replay horizon %s", o.AutomaticReplayHorizon, o.IrisAutomaticReplayHorizon)
	}

	if o.ReplyRetention > 0 && o.ReplyRetention < admission {
		return fmt.Errorf("pgstore: ReplyRetention %s is shorter than the Iris admission retention %s", o.ReplyRetention, admission)
	}

	if o.InboxTerminalRetention > 0 && o.InboxTerminalRetention <= admission {
		return fmt.Errorf("pgstore: InboxTerminalRetention %s must exceed the Iris admission retention %s; a redelivered webhook would re-execute instead of dedup", o.InboxTerminalRetention, admission)
	}

	return nil
}

type Store struct {
	db   Querier
	opts Options
}

var (
	_ irisdurable.Admitter    = (*Store)(nil)
	_ irisdurable.NonceStore  = (*Store)(nil)
	_ irisdurable.ReplyOutbox = (*Store)(nil)
)

func New(db Querier, opts Options) (*Store, error) {
	if db == nil {
		return nil, errors.New("pgstore: querier is required")
	}

	if err := opts.validate(); err != nil {
		return nil, err
	}

	return &Store{db: db, opts: opts}, nil
}

func (s *Store) Options() Options {
	return s.opts
}

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

func (s *Store) backoff(attempt int) time.Duration {
	shift := min(max(attempt-1, 0), irisdurable.RetryBackoffMaxShift)

	delay := s.opts.BackoffBase << uint(shift)
	if delay > s.opts.BackoffMax || delay <= 0 {
		delay = s.opts.BackoffMax
	}

	return delay
}
