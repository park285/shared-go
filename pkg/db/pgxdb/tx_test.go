package pgxdb

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
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
