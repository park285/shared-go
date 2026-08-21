package dbmigrate

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io/fs"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

func TestManifest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		fsys    fs.FS
		want    []string
		wantErr string
	}{
		{
			name: "ordered entries",
			fsys: fstest.MapFS{
				ManifestName: {Data: []byte("\n# comment\n001 first.sql\n002 second.sql\n")},
			},
			want: []string{"first.sql", "second.sql"},
		},
		{
			name:    "missing manifest",
			fsys:    fstest.MapFS{},
			wantErr: "open manifest",
		},
		{
			name: "descending order token rejected",
			fsys: fstest.MapFS{
				ManifestName: {Data: []byte("002 second.sql\n001 first.sql\n")},
			},
			wantErr: `manifest line 2: order "001" must be greater than previous order "002"`,
		},
		{
			name: "order token width does not decide precedence",
			fsys: fstest.MapFS{
				ManifestName: {Data: []byte("010 tenth.sql\n009 ninth.sql\n")},
			},
			wantErr: `manifest line 2: order "009" must be greater than previous order "010"`,
		},
		{
			name: "zero padded ascending order accepted",
			fsys: fstest.MapFS{
				ManifestName: {Data: []byte("009 ninth.sql\n010 tenth.sql\n")},
			},
			want: []string{"ninth.sql", "tenth.sql"},
		},
		{
			name: "too few fields",
			fsys: fstest.MapFS{
				ManifestName: {Data: []byte("001\n")},
			},
			wantErr: `manifest line 1: malformed "001"`,
		},
		{
			name: "trailing extra field rejected with line number",
			fsys: fstest.MapFS{
				ManifestName: {Data: []byte("# c\n001 first.sql\n002 second.sql extra\n")},
			},
			wantErr: `manifest line 3: malformed "002 second.sql extra"`,
		},
		{
			name: "duplicate filename rejected counting comment and blank lines",
			fsys: fstest.MapFS{
				ManifestName: {Data: []byte("# h\n\n001 a.sql\n\n002 a.sql\n")},
			},
			wantErr: `manifest line 5: duplicate filename "a.sql" (first seen at line 3)`,
		},
		{
			name: "duplicate order rejected",
			fsys: fstest.MapFS{
				ManifestName: {Data: []byte("001 a.sql\n001 b.sql\n")},
			},
			wantErr: `manifest line 2: duplicate order "001" (first seen at line 1)`,
		},
		{
			name: "non decimal order rejected",
			fsys: fstest.MapFS{
				ManifestName: {Data: []byte("one first.sql\n")},
			},
			wantErr: `manifest line 1: order "one" must contain only decimal digits`,
		},
		{
			name: "nested migration path rejected",
			fsys: fstest.MapFS{
				ManifestName: {Data: []byte("001 nested/first.sql\n")},
			},
			wantErr: `manifest line 1: migration "nested/first.sql" must be a single .sql filename`,
		},
		{
			name: "non sql migration rejected",
			fsys: fstest.MapFS{
				ManifestName: {Data: []byte("001 first.txt\n")},
			},
			wantErr: `manifest line 1: migration "first.txt" must be a single .sql filename`,
		},
		{
			name: "empty manifest",
			fsys: fstest.MapFS{
				ManifestName: {Data: []byte("# only comments\n")},
			},
			wantErr: "has no entries",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := Manifest(tt.fsys)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Manifest() error = %v, want substring %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Manifest() error = %v", err)
			}
			if !slices.Equal(got, tt.want) {
				t.Fatalf("Manifest() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestApplyOrderingAndWithOnly(t *testing.T) {
	t.Parallel()

	fsys := fstest.MapFS{
		ManifestName: {Data: []byte("001 first.sql\n002 second.sql\n003 third.sql\n")},
		"first.sql":  {Data: []byte("select 1")},
		"second.sql": {Data: []byte("select 2")},
		"third.sql":  {Data: []byte("select 3")},
	}

	var got []string
	err := Apply(context.Background(), fsys, func(_ context.Context, query string, _ ...any) error {
		got = append(got, query)
		return nil
	}, WithOnly("third.sql", "first.sql"))
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	want := []string{"select 1", "select 3"}
	if !slices.Equal(got, want) {
		t.Fatalf("executed SQL = %v, want %v", got, want)
	}
}

func TestApplyWithOnlyRejectsUnknownName(t *testing.T) {
	t.Parallel()

	fsys := fstest.MapFS{
		ManifestName: {Data: []byte("001 first.sql\n002 second.sql\n")},
		"first.sql":  {Data: []byte("select 1")},
		"second.sql": {Data: []byte("select 2")},
	}

	db := newFakeLedgerDB()
	err := Apply(context.Background(), fsys, db.Exec,
		WithOnly("first.sql", "nope.sql"),
		WithSession(SessionConfig{LockTimeout: time.Second, StatementTimeout: time.Second}),
		WithLedger(Ledger{}, db),
	)
	if err == nil || !strings.Contains(err.Error(), "nope.sql") {
		t.Fatalf("Apply() error = %v, want unknown WithOnly name", err)
	}
	if !strings.Contains(err.Error(), "not in manifest") {
		t.Fatalf("Apply() error = %v, want 'not in manifest'", err)
	}
	if len(db.execs) != 0 {
		t.Fatalf("execs = %v, want none before WithOnly validation (session/ledger must not run)", db.execs)
	}
	if len(db.events) != 0 {
		t.Fatalf("events = %v, want none before WithOnly validation", db.events)
	}
}

func TestApplyWithOnlyUnknownNamesAreSortedDeterministically(t *testing.T) {
	t.Parallel()

	fsys := fstest.MapFS{
		ManifestName: {Data: []byte("001 first.sql\n")},
		"first.sql":  {Data: []byte("select 1")},
	}

	for range 10 {
		err := Apply(context.Background(), fsys, func(context.Context, string, ...any) error {
			return nil
		}, WithOnly("zz.sql", "first.sql", "aa.sql", "mm.sql"))
		if err == nil || !strings.Contains(err.Error(), "not in manifest: aa.sql, mm.sql, zz.sql") {
			t.Fatalf("Apply() error = %v, want sorted missing names \"aa.sql, mm.sql, zz.sql\"", err)
		}
	}
}

func TestApplyIdempotentCoreHasNoHiddenState(t *testing.T) {
	t.Parallel()

	fsys := fstest.MapFS{
		ManifestName: {Data: []byte("001 first.sql\n002 second.sql\n")},
		"first.sql":  {Data: []byte("create table if not exists a(id int)")},
		"second.sql": {
			Data: []byte("create index if not exists a_id_idx on a(id)"),
		},
	}

	var got []string
	exec := func(_ context.Context, query string, _ ...any) error {
		got = append(got, query)
		return nil
	}

	if err := Apply(context.Background(), fsys, exec); err != nil {
		t.Fatalf("first Apply() error = %v", err)
	}
	if err := Apply(context.Background(), fsys, exec); err != nil {
		t.Fatalf("second Apply() error = %v", err)
	}

	want := []string{
		"create table if not exists a(id int)",
		"create index if not exists a_id_idx on a(id)",
		"create table if not exists a(id int)",
		"create index if not exists a_id_idx on a(id)",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("executed SQL = %v, want %v", got, want)
	}
}

func TestApplyErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		fsys    fs.FS
		exec    Execer
		wantErr string
	}{
		{
			name: "nil exec",
			fsys: fstest.MapFS{
				ManifestName: {Data: []byte("001 first.sql\n")},
				"first.sql":  {Data: []byte("select 1")},
			},
			wantErr: "exec is required",
		},
		{
			name: "read missing sql",
			fsys: fstest.MapFS{
				ManifestName: {Data: []byte("001 missing.sql\n")},
			},
			exec:    func(context.Context, string, ...any) error { return nil },
			wantErr: "read missing.sql",
		},
		{
			name: "exec error names file",
			fsys: fstest.MapFS{
				ManifestName: {Data: []byte("001 first.sql\n")},
				"first.sql":  {Data: []byte("select 1")},
			},
			exec:    func(context.Context, string, ...any) error { return errors.New("boom") },
			wantErr: "exec first.sql",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := Apply(context.Background(), tt.fsys, tt.exec)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Apply() error = %v, want substring %q", err, tt.wantErr)
			}
		})
	}
}

func TestSQLExec(t *testing.T) {
	t.Parallel()

	db := sql.OpenDB(&fakeSQLConnector{})
	defer func() { _ = db.Close() }()

	if err := SQLExec(db)(context.Background(), "select 1 where $1 = $1", 1); err != nil {
		t.Fatalf("SQLExec() error = %v", err)
	}
}

type fakeSQLConnector struct{}

func (c *fakeSQLConnector) Connect(context.Context) (driver.Conn, error) {
	return &fakeSQLConn{}, nil
}

func (c *fakeSQLConnector) Driver() driver.Driver {
	return fakeSQLDriver{}
}

type fakeSQLDriver struct{}

func (d fakeSQLDriver) Open(string) (driver.Conn, error) {
	return &fakeSQLConn{}, nil
}

type fakeSQLConn struct{}

func (c *fakeSQLConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("prepare not supported")
}

func (c *fakeSQLConn) Close() error {
	return nil
}

func (c *fakeSQLConn) Begin() (driver.Tx, error) {
	return nil, errors.New("begin not supported")
}

func (c *fakeSQLConn) ExecContext(context.Context, string, []driver.NamedValue) (driver.Result, error) {
	return driver.RowsAffected(1), nil
}

func TestApplyRunsEachMigrationFileWithoutArguments(t *testing.T) {
	t.Parallel()

	body := "CREATE TABLE a(id int);\nCREATE TABLE b(id int);\n"
	fsys := fstest.MapFS{
		ManifestName: {Data: []byte("001 first.sql\n")},
		"first.sql":  {Data: []byte(body)},
	}

	var calls []recordedExec
	if err := Apply(context.Background(), fsys, recordingExec(&calls, "")); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	var applied *recordedExec

	for i := range calls {
		if calls[i].query == body {
			applied = &calls[i]

			break
		}
	}

	if applied == nil {
		t.Fatalf("migration body was never executed; calls = %v", calls)
	}

	if len(applied.args) != 0 {
		t.Fatalf("migration body executed with %d arguments (%v); pgx only takes the simple protocol — and with it the implicit transaction that makes a multi-statement file atomic — when the argument list is empty", len(applied.args), applied.args)
	}
}

func TestApplyWithOnlyRejectsEmptySelection(t *testing.T) {
	t.Parallel()

	fsys := fstest.MapFS{
		ManifestName: {Data: []byte("001 first.sql\n002 second.sql\n")},
		"first.sql":  {Data: []byte("select 1")},
		"second.sql": {Data: []byte("select 2")},
	}

	db := newFakeLedgerDB()
	err := Apply(context.Background(), fsys, db.Exec,
		WithOnly(),
		WithSession(SessionConfig{LockTimeout: time.Second, StatementTimeout: time.Second}),
		WithLedger(Ledger{}, db),
	)
	if err == nil {
		t.Fatalf("Apply() error = nil, want error for empty WithOnly selection")
	}
	if !strings.Contains(err.Error(), "WithOnly") {
		t.Fatalf("Apply() error = %v, want error naming WithOnly", err)
	}
	if len(db.execs) != 0 {
		t.Fatalf("execs = %v, want none: empty WithOnly must fail before session/ledger side effects", db.execs)
	}
	if len(db.events) != 0 {
		t.Fatalf("events = %v, want none before WithOnly validation", db.events)
	}
}

func TestApplyWithOnlyEmptyVariadicSliceRejected(t *testing.T) {
	t.Parallel()

	fsys := fstest.MapFS{
		ManifestName: {Data: []byte("001 first.sql\n")},
		"first.sql":  {Data: []byte("select 1")},
	}

	var selected []string
	var got []string
	err := Apply(context.Background(), fsys, func(_ context.Context, query string, _ ...any) error {
		got = append(got, query)
		return nil
	}, WithOnly(selected...))
	if err == nil {
		t.Fatalf("Apply() error = nil, want error for empty WithOnly selection")
	}
	if len(got) != 0 {
		t.Fatalf("executed SQL = %v, want none", got)
	}
}
