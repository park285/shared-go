package dbmigrate

import (
	"context"
	"errors"
	"io/fs"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

type manifestCase struct {
	name    string
	fsys    fs.FS
	want    []string
	wantErr string
}

func manifestOrderCases() []manifestCase {
	return []manifestCase{
		{
			name: "ordered entries",
			fsys: fstest.MapFS{
				ManifestName: {Data: []byte("\n# comment\n001 first.sql\n002 second.sql\n")},
			},
			want: []string{testFirstSQL, testSecondSQL},
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
	}
}

func manifestEntryCases() []manifestCase {
	return []manifestCase{
		{
			name:    "missing manifest",
			fsys:    fstest.MapFS{},
			wantErr: "open manifest",
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
}

func TestManifest(t *testing.T) {
	t.Parallel()

	for _, tt := range slices.Concat(manifestOrderCases(), manifestEntryCases()) {
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

func TestApplyOrderingAndOnly(t *testing.T) {
	t.Parallel()

	fsys := fstest.MapFS{
		ManifestName:  {Data: []byte("001 first.sql\n002 second.sql\n003 third.sql\n")},
		testFirstSQL:  {Data: []byte("select 1")},
		testSecondSQL: {Data: []byte("select 2")},
		"third.sql":   {Data: []byte("select 3")},
	}

	var got []string

	err := Apply(t.Context(), fsys, func(_ context.Context, query string, _ ...any) error {
		got = append(got, query)
		return nil
	}, ApplyOptions{Only: []string{"third.sql", testFirstSQL}})
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	want := []string{"select 1", "select 3"}
	if !slices.Equal(got, want) {
		t.Fatalf("executed SQL = %v, want %v", got, want)
	}
}

func TestApplyOnlyRejectsUnknownName(t *testing.T) {
	t.Parallel()

	fsys := fstest.MapFS{
		ManifestName:  {Data: []byte("001 first.sql\n002 second.sql\n")},
		testFirstSQL:  {Data: []byte("select 1")},
		testSecondSQL: {Data: []byte("select 2")},
	}

	db := newFakeLedgerDB()
	err := Apply(t.Context(), fsys, db.Exec, ApplyOptions{
		Only:          []string{testFirstSQL, "nope.sql"},
		Session:       SessionConfig{LockTimeout: time.Second, StatementTimeout: time.Second},
		Ledger:        &Ledger{},
		LedgerQuerier: db,
	})

	if err == nil || !strings.Contains(err.Error(), "nope.sql") {
		t.Fatalf("Apply() error = %v, want unknown Only name", err)
	}

	if !strings.Contains(err.Error(), "not in manifest") {
		t.Fatalf("Apply() error = %v, want 'not in manifest'", err)
	}

	if len(db.execs) != 0 {
		t.Fatalf("execs = %v, want none before Only validation (session/ledger must not run)", db.execs)
	}

	if len(db.events) != 0 {
		t.Fatalf("events = %v, want none before Only validation", db.events)
	}
}

func TestApplyOnlyUnknownNamesAreSortedDeterministically(t *testing.T) {
	t.Parallel()

	fsys := fstest.MapFS{
		ManifestName: {Data: []byte("001 first.sql\n")},
		testFirstSQL: {Data: []byte("select 1")},
	}

	for range 10 {
		err := Apply(t.Context(), fsys, func(context.Context, string, ...any) error {
			return nil
		}, ApplyOptions{Only: []string{"zz.sql", testFirstSQL, "aa.sql", "mm.sql"}})
		if err == nil || !strings.Contains(err.Error(), "not in manifest: aa.sql, mm.sql, zz.sql") {
			t.Fatalf("Apply() error = %v, want sorted missing names \"aa.sql, mm.sql, zz.sql\"", err)
		}
	}
}

func TestApplyIdempotentCoreHasNoHiddenState(t *testing.T) {
	t.Parallel()

	fsys := fstest.MapFS{
		ManifestName: {Data: []byte("001 first.sql\n002 second.sql\n")},
		testFirstSQL: {Data: []byte("create table if not exists a(id int)")},
		testSecondSQL: {
			Data: []byte("create index if not exists a_id_idx on a(id)"),
		},
	}

	var got []string

	exec := func(_ context.Context, query string, _ ...any) error {
		got = append(got, query)
		return nil
	}

	if err := Apply(t.Context(), fsys, exec, ApplyOptions{}); err != nil {
		t.Fatalf("first Apply() error = %v", err)
	}

	if err := Apply(t.Context(), fsys, exec, ApplyOptions{}); err != nil {
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
				testFirstSQL: {Data: []byte("select 1")},
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
				testFirstSQL: {Data: []byte("select 1")},
			},
			exec:    func(context.Context, string, ...any) error { return errors.New("boom") },
			wantErr: "exec first.sql",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := Apply(t.Context(), tt.fsys, tt.exec, ApplyOptions{})
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Apply() error = %v, want substring %q", err, tt.wantErr)
			}
		})
	}
}

func TestApplyRunsEachMigrationFileWithoutArguments(t *testing.T) {
	t.Parallel()

	body := "CREATE TABLE a(id int);\nCREATE TABLE b(id int);\n"
	fsys := fstest.MapFS{
		ManifestName: {Data: []byte("001 first.sql\n")},
		testFirstSQL: {Data: []byte(body)},
	}

	var calls []recordedExec

	if err := Apply(t.Context(), fsys, recordingExec(&calls, ""), ApplyOptions{}); err != nil {
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

func TestApplyOnlyRejectsEmptySelection(t *testing.T) {
	t.Parallel()

	fsys := fstest.MapFS{
		ManifestName:  {Data: []byte("001 first.sql\n002 second.sql\n")},
		testFirstSQL:  {Data: []byte("select 1")},
		testSecondSQL: {Data: []byte("select 2")},
	}

	db := newFakeLedgerDB()

	err := Apply(t.Context(), fsys, db.Exec, ApplyOptions{
		Only:          []string{},
		Session:       SessionConfig{LockTimeout: time.Second, StatementTimeout: time.Second},
		Ledger:        &Ledger{},
		LedgerQuerier: db,
	})
	if err == nil {
		t.Fatal("Apply() error = nil, want error for empty Only selection")
	}

	if !strings.Contains(err.Error(), "Only requires at least one migration name") {
		t.Fatalf("Apply() error = %v, want error naming Only", err)
	}

	if len(db.execs) != 0 {
		t.Fatalf("execs = %v, want none: empty Only must fail before session/ledger side effects", db.execs)
	}

	if len(db.events) != 0 {
		t.Fatalf("events = %v, want none before Only validation", db.events)
	}
}

func TestApplyRejectsLedgerQuerierWithoutLedger(t *testing.T) {
	t.Parallel()

	fsys := fstest.MapFS{
		ManifestName: {Data: []byte("001 first.sql\n")},
		testFirstSQL: {Data: []byte("select 1")},
	}

	db := newFakeLedgerDB()

	err := Apply(t.Context(), fsys, db.Exec, ApplyOptions{LedgerQuerier: db, Session: SessionConfig{LockTimeout: time.Second}})
	if err == nil || !strings.Contains(err.Error(), "LedgerQuerier requires Ledger") {
		t.Fatalf("Apply() error = %v, want LedgerQuerier without Ledger rejected", err)
	}

	if len(db.execs) != 0 {
		t.Fatalf("execs = %v, want no session or migration statement without the intended ledger", db.execs)
	}
}

func TestApplyRejectsLedgerWithoutQuerier(t *testing.T) {
	t.Parallel()

	fsys := fstest.MapFS{
		ManifestName: {Data: []byte("001 first.sql\n")},
		testFirstSQL: {Data: []byte("select 1")},
	}

	db := newFakeLedgerDB()

	err := Apply(t.Context(), fsys, db.Exec, ApplyOptions{Ledger: &Ledger{}, Session: SessionConfig{LockTimeout: time.Second}})
	if err == nil || !strings.Contains(err.Error(), "ledger querier is required") {
		t.Fatalf("Apply() error = %v, want missing ledger querier rejected", err)
	}

	if len(db.execs) != 0 {
		t.Fatalf("execs = %v, want no session or migration statement without a ledger querier", db.execs)
	}
}
