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
				ManifestName: {Data: []byte("\n# comment\n001 first.sql\n002 second.sql extra\n")},
			},
			want: []string{"first.sql", "extra"},
		},
		{
			name:    "missing manifest",
			fsys:    fstest.MapFS{},
			wantErr: "open manifest",
		},
		{
			name: "malformed line",
			fsys: fstest.MapFS{
				ManifestName: {Data: []byte("001\n")},
			},
			wantErr: "malformed manifest line",
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
