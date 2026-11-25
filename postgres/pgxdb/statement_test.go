package pgxdb_test

import (
	"context"
	"errors"
	"testing"
	"testing/fstest"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/park285/shared-go/postgres/pgxdb"
	"github.com/stretchr/testify/require"
)

type recordingQuerier struct {
	sql  string
	args []any
}

func (r *recordingQuerier) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	r.sql, r.args = sql, args

	return pgconn.NewCommandTag("UPDATE 1"), nil
}

var errQueryRecorded = errors.New("query recorded")

func (r *recordingQuerier) Query(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
	r.sql, r.args = sql, args

	return nil, errQueryRecorded
}

func (r *recordingQuerier) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	r.sql, r.args = sql, args

	return nil
}

var statementFS = fstest.MapFS{
	"queries/claim.sql": {Data: []byte("\n  SELECT {{columns}} FROM {{table}} WHERE id = $1\n")},
	"queries/empty.sql": {Data: []byte(" \n")},
}

func TestMustStatementLoadsTrimmedAsset(t *testing.T) {
	t.Parallel()

	statement := pgxdb.MustStatement(statementFS, "queries/claim.sql")

	require.Equal(t, "SELECT {{columns}} FROM {{table}} WHERE id = $1", statement.SQL())
}

func TestMustStatementRejectsMissingAndEmptyAssets(t *testing.T) {
	t.Parallel()

	require.PanicsWithValue(t, "empty SQL asset queries/empty.sql", func() {
		pgxdb.MustStatement(statementFS, "queries/empty.sql")
	})
	require.Panics(t, func() { pgxdb.MustStatement(statementFS, "queries/absent.sql") })
}

func TestReplaceReturnsNewStatement(t *testing.T) {
	t.Parallel()

	template := pgxdb.MustStatement(statementFS, "queries/claim.sql")
	replaced := template.Replace("{{columns}}", "id, state", "{{table}}", "inbox")

	require.Equal(t, "SELECT id, state FROM inbox WHERE id = $1", replaced.SQL())
	require.Equal(t, "SELECT {{columns}} FROM {{table}} WHERE id = $1", template.SQL())
}

func TestExecutorsSendStatementTextAndArguments(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	statement := pgxdb.MustStatement(statementFS, "queries/claim.sql").Replace("{{columns}}", "id", "{{table}}", "inbox")
	want := "SELECT id FROM inbox WHERE id = $1"

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
