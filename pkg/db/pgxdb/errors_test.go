package pgxdb

import (
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestIsDuplicateKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil", want: false},
		{name: "unique violation", err: &pgconn.PgError{Code: "23505"}, want: true},
		{name: "wrapped unique violation", err: fmt.Errorf("insert: %w", &pgconn.PgError{Code: "23505"}), want: true},
		{name: "sqlite unique constraint message", err: errors.New("UNIQUE constraint failed: auth_users.email"), want: true},
		{name: "postgres duplicate key message", err: errors.New("ERROR: duplicate key value violates unique constraint"), want: true},
		{name: "foreign key violation", err: &pgconn.PgError{Code: "23503"}, want: false},
		{name: "unrelated", err: errors.New("connection refused"), want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := IsDuplicateKey(tt.err); got != tt.want {
				t.Fatalf("IsDuplicateKey() = %v, want %v", got, tt.want)
			}
		})
	}
}
