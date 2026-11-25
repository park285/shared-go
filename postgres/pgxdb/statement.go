package pgxdb

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/park285/shared-go/v2/pkg/sqlutil"
)

var ErrInvalidStatement = errors.New("pgxdb: invalid statement")

type Statement struct {
	sql string
}

type token string

type Querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func MustStatement(fsys embed.FS, name string) Statement {
	return Statement{sql: sqlutil.MustQuery(fsys, name)}
}

func (s Statement) With(placeholder string, fragment Statement) Statement {
	if fragment.sql == "" {
		panic(fmt.Sprintf("pgxdb: empty fragment for %s", placeholder))
	}

	return s.replace(placeholder, fragment.sql)
}

func (s Statement) WithToken(placeholder string, value token) Statement {
	if value == "" {
		panic(fmt.Sprintf("pgxdb: empty token for %s", placeholder))
	}

	return s.replace(placeholder, string(value))
}

func (s Statement) replace(placeholder, value string) Statement {
	if !strings.Contains(s.sql, placeholder) {
		panic(fmt.Sprintf("pgxdb: placeholder %s not found", placeholder))
	}

	return Statement{sql: strings.ReplaceAll(s.sql, placeholder, value)}
}

func (s Statement) SQL() string {
	return s.sql
}

func (s Statement) validate() error {
	switch {
	case s.sql == "":
		return fmt.Errorf("%w: empty SQL", ErrInvalidStatement)
	case strings.Contains(s.sql, "{{"):
		return fmt.Errorf("%w: unresolved placeholder", ErrInvalidStatement)
	default:
		return nil
	}
}

type invalidRow struct {
	err error
}

func (r invalidRow) Scan(...any) error {
	return r.err
}

func Exec(ctx context.Context, q Querier, s Statement, args ...any) (pgconn.CommandTag, error) {
	if err := s.validate(); err != nil {
		return pgconn.CommandTag{}, err
	}

	return q.Exec(ctx, s.sql, args...) //nolint:wrapcheck,forbidigo // 호출부가 동작 이름으로 오류를 감싸고, 이 래퍼만 Statement를 pgx에 넘긴다.
}

func Query(ctx context.Context, q Querier, s Statement, args ...any) (pgx.Rows, error) {
	if err := s.validate(); err != nil {
		return nil, err
	}

	return q.Query(ctx, s.sql, args...) //nolint:wrapcheck,forbidigo // 호출부가 동작 이름으로 오류를 감싸고, 이 래퍼만 Statement를 pgx에 넘긴다.
}

func QueryRow(ctx context.Context, q Querier, s Statement, args ...any) pgx.Row {
	if err := s.validate(); err != nil {
		return invalidRow{err: err}
	}

	return q.QueryRow(ctx, s.sql, args...) //nolint:forbidigo // 이 래퍼만 Statement를 pgx에 넘긴다.
}

func Queue(b *pgx.Batch, s Statement, args ...any) *pgx.QueuedQuery {
	if err := s.validate(); err != nil {
		panic(err.Error())
	}

	return b.Queue(s.sql, args...) //nolint:forbidigo // 이 래퍼만 Statement를 pgx에 넘긴다.
}
