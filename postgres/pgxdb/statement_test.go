package pgxdb_test

import (
	"context"
	"embed"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/park285/shared-go/postgres/pgxdb"
	"github.com/stretchr/testify/require"
)

//go:embed testdata/queries/*.sql
var statementFS embed.FS

type recordingQuerier struct {
	calls int
	sql   string
	args  []any
}

func (r *recordingQuerier) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	r.calls++

	r.sql, r.args = sql, args

	return pgconn.NewCommandTag("UPDATE 1"), nil
}

var errQueryRecorded = errors.New("query recorded")

func (r *recordingQuerier) Query(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
	r.calls++

	r.sql, r.args = sql, args

	return nil, errQueryRecorded
}

func (r *recordingQuerier) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	r.calls++

	r.sql, r.args = sql, args

	return nil
}

func claimStatement() pgxdb.Statement {
	return pgxdb.MustStatement(statementFS, "testdata/queries/claim.sql").
		With("{{columns}}", pgxdb.MustStatement(statementFS, "testdata/queries/columns.sql")).
		WithToken("{{table}}", "inbox")
}

func TestMustStatementLoadsTrimmedAsset(t *testing.T) {
	t.Parallel()

	statement := pgxdb.MustStatement(statementFS, "testdata/queries/claim.sql")

	require.Equal(t, "SELECT {{columns}} FROM {{table}} WHERE id = $1", statement.SQL())
}

func TestMustStatementRejectsMissingAndEmptyAssets(t *testing.T) {
	t.Parallel()

	require.PanicsWithValue(t, "empty SQL asset testdata/queries/empty.sql", func() {
		pgxdb.MustStatement(statementFS, "testdata/queries/empty.sql")
	})
	require.Panics(t, func() { pgxdb.MustStatement(statementFS, "testdata/queries/absent.sql") })
}

func TestWithReplacesEveryPlaceholderAndKeepsTemplate(t *testing.T) {
	t.Parallel()

	template := pgxdb.MustStatement(statementFS, "testdata/queries/claim.sql")

	require.Equal(t, "SELECT id, state FROM inbox WHERE id = $1", claimStatement().SQL())
	require.Equal(t, "SELECT {{columns}} FROM {{table}} WHERE id = $1", template.SQL())
}

func TestWithRejectsMissingPlaceholderAndEmptyValues(t *testing.T) {
	t.Parallel()

	template := pgxdb.MustStatement(statementFS, "testdata/queries/claim.sql")

	require.PanicsWithValue(t, "pgxdb: placeholder {{colums}} not found", func() {
		template.WithToken("{{colums}}", "id")
	})
	require.PanicsWithValue(t, "pgxdb: empty token for {{table}}", func() {
		template.WithToken("{{table}}", "")
	})
	require.PanicsWithValue(t, "pgxdb: empty fragment for {{columns}}", func() {
		template.With("{{columns}}", pgxdb.Statement{})
	})
}

func TestExecutorsSendStatementTextAndArguments(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	statement := claimStatement()
	want := "SELECT id, state FROM inbox WHERE id = $1"

	var exec recordingQuerier

	tag, err := pgxdb.Exec(ctx, &exec, statement, 7)
	require.NoError(t, err)
	require.EqualValues(t, 1, tag.RowsAffected())
	require.Equal(t, want, exec.sql)
	require.Equal(t, []any{7}, exec.args)

	var query recordingQuerier

	rows, err := pgxdb.Query(ctx, &query, statement, 8) //nolint:sqlclosecheck // 기록용 querier는 rows를 만들지 않는다.
	require.ErrorIs(t, err, errQueryRecorded)
	require.Nil(t, rows)
	require.Equal(t, want, query.sql)
	require.Equal(t, []any{8}, query.args)

	var row recordingQuerier

	pgxdb.QueryRow(ctx, &row, statement, 9)
	require.Equal(t, want, row.sql)
	require.Equal(t, []any{9}, row.args)

	var batch pgx.Batch

	queued := pgxdb.Queue(&batch, statement, 10)
	require.Equal(t, want, queued.SQL)
	require.Equal(t, []any{10}, queued.Arguments)
	require.Equal(t, 1, batch.Len())
}

func TestExecutorsRejectInvalidStatementsWithoutCallingDatabase(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	unresolved := pgxdb.MustStatement(statementFS, "testdata/queries/claim.sql")

	for _, statement := range []pgxdb.Statement{{}, unresolved} {
		var q recordingQuerier

		_, err := pgxdb.Exec(ctx, &q, statement)
		require.ErrorIs(t, err, pgxdb.ErrInvalidStatement)

		rows, err := pgxdb.Query(ctx, &q, statement) //nolint:sqlclosecheck // 검증 실패는 rows를 만들지 않는다.
		require.ErrorIs(t, err, pgxdb.ErrInvalidStatement)
		require.Nil(t, rows)

		require.ErrorIs(t, pgxdb.QueryRow(ctx, &q, statement).Scan(), pgxdb.ErrInvalidStatement)
		require.Zero(t, q.calls)

		var batch pgx.Batch

		require.Panics(t, func() { pgxdb.Queue(&batch, statement) })
		require.Zero(t, batch.Len())
	}
}
