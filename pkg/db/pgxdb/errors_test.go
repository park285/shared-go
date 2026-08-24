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
		{name: "sqlite unique constraint message is not a pg unique violation", err: errors.New("UNIQUE constraint failed: auth_users.email"), want: false},
		{name: "untyped postgres duplicate key message", err: errors.New("ERROR: duplicate key value violates unique constraint"), want: false},
		{name: "attacker controlled message must not match", err: errors.New(`insert user display_name="duplicate key value violates unique constraint"`), want: false},
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
