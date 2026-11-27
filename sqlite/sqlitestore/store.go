package sqlitestore

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/park285/shared-go/sqlite/sqlitedb"

	"github.com/park285/shared-go/v2/pkg/irisdurable"
)

var ErrClaimLost = errors.New("sqlitestore: claim token no longer owns the row")

type Options struct {
	Scope                           string
	IrisAdmissionRetention          time.Duration
	Lease                           time.Duration
	InboxTerminalRetention          time.Duration
	InboxManualReviewRetention      time.Duration
	DiscardInboxManualReviewPayload bool
}

func (o Options) validate() error {
	if o.IrisAdmissionRetention < 0 || o.Lease < 0 || o.InboxTerminalRetention < 0 || o.InboxManualReviewRetention < 0 {
		return errors.New("sqlitestore: options must not be negative")
	}

	if o.IrisAdmissionRetention == 0 && (o.InboxTerminalRetention > 0 || o.InboxManualReviewRetention > 0) {
		return errors.New("sqlitestore: IrisAdmissionRetention is required with retention options")
	}

	if o.InboxTerminalRetention > 0 && o.InboxTerminalRetention <= o.IrisAdmissionRetention {
		return fmt.Errorf("sqlitestore: InboxTerminalRetention %s must exceed the Iris admission retention %s; a redelivered webhook would re-execute instead of dedup",
			o.InboxTerminalRetention, o.IrisAdmissionRetention)
	}

	if o.InboxManualReviewRetention > 0 && o.InboxManualReviewRetention < o.InboxTerminalRetention {
		return fmt.Errorf("sqlitestore: InboxManualReviewRetention %s is shorter than InboxTerminalRetention %s",
			o.InboxManualReviewRetention, o.InboxTerminalRetention)
	}

	return nil
}

type querier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

type Store struct {
	db   *sqlitedb.DB
	tx   *sql.Tx
	opts Options
}

var (
	_ irisdurable.Admitter   = (*Store)(nil)
	_ irisdurable.NonceStore = (*Store)(nil)
)

func New(db *sqlitedb.DB, opts Options) (*Store, error) {
	if db == nil {
		return nil, errors.New("sqlitestore: database is required")
	}

	if err := opts.validate(); err != nil {
		return nil, err
	}

	return &Store{db: db, opts: opts}, nil
}

func (s *Store) WithTx(tx *sql.Tx) *Store {
	return &Store{db: s.db, tx: tx, opts: s.opts}
}

func (s *Store) Options() Options {
	return s.opts
}

func (s *Store) write(ctx context.Context, fn func(querier, int64) error) error {
	if s.tx != nil {
		return fn(s.tx, time.Now().UnixMilli())
	}

	if err := s.db.WriteTx(ctx, func(tx *sql.Tx) error { return fn(tx, time.Now().UnixMilli()) }); err != nil {
		return fmt.Errorf("sqlitestore: %w", err)
	}

	return nil
}

func (s *Store) read() querier {
	if s.tx != nil {
		return s.tx
	}

	return s.db.Reader()
}

func execFenced(ctx context.Context, q querier, action, statement string, args ...any) error {
	result, err := q.ExecContext(ctx, statement, args...)
	if err != nil {
		return fmt.Errorf("sqlitestore: %s: %w", action, err)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("sqlitestore: %s rows affected: %w", action, err)
	}

	if affected == 0 {
		return fmt.Errorf("sqlitestore: %s: %w", action, ErrClaimLost)
	}

	return nil
}

func rowsAffected(ctx context.Context, q querier, action, statement string, args ...any) (int64, error) {
	result, err := q.ExecContext(ctx, statement, args...)
	if err != nil {
		return 0, fmt.Errorf("sqlitestore: %s: %w", action, err)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("sqlitestore: %s rows affected: %w", action, err)
	}

	return affected, nil
}

func newClaimToken() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("sqlitestore: generate claim token: %w", err)
	}

	return hex.EncodeToString(buf), nil
}
