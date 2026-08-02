package pgxdb

import (
	"errors"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
)

const sqlstateUniqueViolation = "23505"

func IsDuplicateKey(err error) bool {
	if err == nil {
		return false
	}
	if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok && pgErr.Code == sqlstateUniqueViolation {
		return true
	}
	message := err.Error()
	return strings.Contains(message, "UNIQUE constraint failed") ||
		strings.Contains(message, "duplicate key value violates unique constraint")
}
