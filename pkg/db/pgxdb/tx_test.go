package pgxdb

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

type stubRollbacker struct {
	err    error
	called bool
}

func (s *stubRollbacker) Rollback(context.Context) error {
	s.called = true

	return s.err
}

func TestRollbackDeferred(t *testing.T) {
	t.Parallel()

	errCause := errors.New("cause")
	errRollback := errors.New("rollback failed")

	tests := []struct {
		name         string
		initial      error
		rollback     error
		wantCause    bool
		wantRollback bool
	}{
		{name: "success keeps nil", initial: nil, rollback: nil},
		{name: "closed after commit is ignored", initial: nil, rollback: pgx.ErrTxClosed},
		{name: "wrapped closed keeps cause", initial: errCause, rollback: fmt.Errorf("closed: %w", pgx.ErrTxClosed), wantCause: true},
		{name: "failure is joined to nil", initial: nil, rollback: errRollback, wantRollback: true},
		{name: "failure is joined to cause", initial: errCause, rollback: errRollback, wantCause: true, wantRollback: true},
		{name: "cause survives successful rollback", initial: errCause, rollback: nil, wantCause: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tx := &stubRollbacker{err: tc.rollback}
			err := tc.initial

			RollbackDeferred(t.Context(), tx, &err)

			if !tx.called {
				t.Fatal("Rollback() was not called")
			}

			if got := errors.Is(err, errCause); got != tc.wantCause {
				t.Fatalf("errors.Is(err, cause) = %v, want %v (err = %v)", got, tc.wantCause, err)
			}

			if got := errors.Is(err, errRollback); got != tc.wantRollback {
				t.Fatalf("errors.Is(err, rollback) = %v, want %v (err = %v)", got, tc.wantRollback, err)
			}

			if !tc.wantCause && !tc.wantRollback && err != nil {
				t.Fatalf("err = %v, want nil", err)
			}
		})
	}
}

type rollbackFunc func(context.Context) error

func (f rollbackFunc) Rollback(ctx context.Context) error {
	return f(ctx)
}

func TestRollbackCleanupContext(t *testing.T) {
	t.Parallel()

	for _, deferred := range []bool{false, true} {
		for _, expired := range []bool{false, true} {
			t.Run(fmt.Sprintf("deferred=%t/expired=%t", deferred, expired), func(t *testing.T) {
				t.Parallel()

				type key struct{}

				parent := context.WithValue(t.Context(), key{}, "request-value")

				var (
					ctx    context.Context
					cancel context.CancelFunc
				)

				if expired {
					ctx, cancel = context.WithDeadline(parent, time.Now().Add(-time.Second))
				} else {
					ctx, cancel = context.WithCancel(parent)
				}

				cancel()

				before := time.Now()

				var cleanupDone <-chan struct{}

				tx := rollbackFunc(func(ctx context.Context) error {
					cleanupDone = ctx.Done()
					require.NoError(t, ctx.Err())
					require.NoError(t, context.Cause(ctx))
					require.Equal(t, "request-value", ctx.Value(key{}))

					deadline, ok := ctx.Deadline()
					require.True(t, ok)
					require.False(t, deadline.Before(before.Add(5*time.Second)))
					require.False(t, deadline.After(time.Now().Add(5*time.Second)))

					return nil
				})

				var err error

				if deferred {
					RollbackDeferred(ctx, tx, &err)
				} else {
					err = Rollback(ctx, tx)
				}

				require.NoError(t, err)
				require.NotNil(t, cleanupDone)

				select {
				case <-cleanupDone:
				default:
					t.Fatal("cleanup context must be canceled after rollback completes")
				}
			})
		}
	}
}

func TestRollbackPreservesErrors(t *testing.T) {
	t.Parallel()

	failure := errors.New("rollback failed")
	for _, tc := range []struct {
		name string
		err  error
		want error
	}{
		{"success", nil, nil},
		{"closed", pgx.ErrTxClosed, nil},
		{"wrapped closed", fmt.Errorf("closed: %w", pgx.ErrTxClosed), nil},
		{"failure", failure, failure},
		{"cleanup timeout", context.DeadlineExceeded, context.DeadlineExceeded},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tx := &stubRollbacker{err: tc.err}
			err := Rollback(t.Context(), tx)

			if !tx.called || !errors.Is(err, tc.want) {
				t.Fatalf("called=%t, err=%v; want %v", tx.called, err, tc.want)
			}
		})
	}
}
