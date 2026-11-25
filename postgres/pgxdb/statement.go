package pgxdb

import (
	"context"
	"io/fs"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/park285/shared-go/v2/pkg/sqlutil"
)

type Statement struct {
	sql string
}

type Querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func MustStatement(fsys fs.FS, name string) Statement {
	return Statement{sql: sqlutil.MustQuery(fsys, name)}
}

func (s Statement) Replace(oldnew ...string) Statement {
	return Statement{sql: strings.NewReplacer(oldnew...).Replace(s.sql)}
}

func (s Statement) SQL() string {
	return s.sql
}

func Exec(ctx context.Context, q Querier, s Statement, args ...any) (pgconn.CommandTag, error) {
	return q.Exec(ctx, s.sql, args...) //nolint:wrapcheck // 호출부가 동작 이름으로 오류를 감싼다.
}

func Query(ctx context.Context, q Querier, s Statement, args ...any) (pgx.Rows, error) {
	return q.Query(ctx, s.sql, args...) //nolint:wrapcheck // 호출부가 동작 이름으로 오류를 감싼다.
}

func QueryRow(ctx context.Context, q Querier, s Statement, args ...any) pgx.Row {
	return q.QueryRow(ctx, s.sql, args...)
}

func Queue(b *pgx.Batch, s Statement, args ...any) *pgx.QueuedQuery {
	return b.Queue(s.sql, args...)
}
