package dbmigrate

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
)

func TestLedgerEnsureCreatesCompatibleTable(t *testing.T) {
	t.Parallel()

	db := newFakeLedgerDB()
	if err := (Ledger{}).Ensure(t.Context(), db.Exec); err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}

	got := strings.Join(db.execs, "\n")

	for _, want := range []string{
		`CREATE TABLE IF NOT EXISTS "schema_migrations"`,
		"filename TEXT PRIMARY KEY",
		"applied_at TIMESTAMPTZ NOT NULL DEFAULT now()",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("Ensure() query = %s, want substring %q", got, want)
		}
	}
}

func TestLedgerRejectsInvalidTableName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		table string
	}{
		{name: "dash", table: "schema-migrations"},
		{name: "empty part", table: "public..schema_migrations"},
		{name: "leading digit", table: "1schema_migrations"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			db := newFakeLedgerDB()

			err := (Ledger{Table: tt.table}).Ensure(t.Context(), db.Exec)
			if err == nil {
				t.Fatal("Ensure() error = nil, want invalid table")
			}

			if !strings.Contains(err.Error(), "invalid ledger table") {
				t.Fatalf("Ensure() error = %v, want invalid ledger table", err)
			}

			if len(db.execs) != 0 {
				t.Fatalf("execs = %v, want none on invalid table", db.execs)
			}
		})
	}
}

func TestLedgerRecordIsIdempotentOnDuplicateInsert(t *testing.T) {
	t.Parallel()

	db := newFakeLedgerDB()
	if err := (Ledger{}).Record(t.Context(), db.Exec, "one.sql"); err != nil {
		t.Fatalf("Record(first) error = %v", err)
	}

	if err := (Ledger{}).Record(t.Context(), db.Exec, "one.sql"); err != nil {
		t.Fatalf("Record(duplicate) error = %v", err)
	}

	if len(db.records) != 1 {
		t.Fatalf("records = %v, want one idempotent record", db.records)
	}

	if len(db.execs) == 0 || !strings.Contains(db.execs[len(db.execs)-1], "ON CONFLICT (filename) DO NOTHING") {
		t.Fatalf("Record() query = %v, want ON CONFLICT DO NOTHING", db.execs)
	}
}

func TestLedgerRendersQuotedDottedIdentifier(t *testing.T) {
	t.Parallel()

	db := newFakeLedgerDB()
	if err := (Ledger{Table: "public.schema_migrations"}).Ensure(t.Context(), db.Exec); err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}

	if len(db.execs) != 1 {
		t.Fatalf("execs = %v, want one", db.execs)
	}

	if !strings.Contains(db.execs[0], `CREATE TABLE IF NOT EXISTS "public"."schema_migrations"`) {
		t.Fatalf("Ensure() query = %s, want quoted dotted identifier", db.execs[0])
	}
}

func TestLedgerRecordBindsFilenameParameter(t *testing.T) {
	t.Parallel()

	db := newFakeLedgerDB()
	name := "owner's-change.sql"

	if err := (Ledger{}).Record(t.Context(), db.Exec, name); err != nil {
		t.Fatalf("Record() error = %v", err)
	}

	if len(db.execs) != 1 {
		t.Fatalf("execs = %v, want one", db.execs)
	}

	if !strings.Contains(db.execs[0], "VALUES ($1)") {
		t.Fatalf("Record() query = %s, want bind parameter", db.execs[0])
	}

	if !slices.Equal(db.args[0], []any{name}) {
		t.Fatalf("Record() args = %v, want bound filename", db.args[0])
	}

	if !slices.Equal(db.records, []string{name}) {
		t.Fatalf("records = %v, want %v", db.records, []string{name})
	}
}

func TestBaselineRecordsManifestThroughWatermark(t *testing.T) {
	t.Parallel()

	db := newFakeLedgerDB()
	fsys := fstest.MapFS{
		ManifestName: {Data: []byte("001 one.sql\n002 two.sql\n003 three.sql\n")},
	}

	if err := Baseline(t.Context(), fsys, db.Exec, "two.sql", Ledger{}); err != nil {
		t.Fatalf("Baseline() error = %v", err)
	}

	want := []string{"one.sql", "two.sql"}
	if !slices.Equal(db.records, want) {
		t.Fatalf("records = %v, want %v", db.records, want)
	}

	if db.applied["three.sql"] {
		t.Fatal("Baseline() recorded migration after watermark")
	}
}

func TestBaselineErrorsWhenWatermarkMissing(t *testing.T) {
	t.Parallel()

	db := newFakeLedgerDB()
	fsys := fstest.MapFS{
		ManifestName: {Data: []byte("001 one.sql\n")},
	}

	err := Baseline(t.Context(), fsys, db.Exec, "missing.sql", Ledger{})
	if err == nil {
		t.Fatal("Baseline() error = nil, want missing watermark")
	}

	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("Baseline() error = %v, want not found", err)
	}

	if len(db.records) != 0 {
		t.Fatalf("records = %v, want none on missing watermark", db.records)
	}
}

func TestApplyWithLedgerSkipsAppliedAndRecordsAfterExec(t *testing.T) {
	t.Parallel()

	db := newFakeLedgerDB()

	db.applied["two.sql"] = true

	fsys := fstest.MapFS{
		ManifestName: {Data: []byte("001 one.sql\n002 two.sql\n003 three.sql\n")},
		"one.sql":    {Data: []byte("select 1")},
		"two.sql":    {Data: []byte("select 2")},
		"three.sql":  {Data: []byte("select 3")},
	}

	err := Apply(t.Context(), fsys, db.Exec, WithLedger(Ledger{}, db))
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	wantEvents := []string{
		"query:one.sql",
		"exec:select 1",
		"record:one.sql",
		"query:two.sql",
		"query:three.sql",
		"exec:select 3",
		"record:three.sql",
	}
	if !slices.Equal(db.events, wantEvents) {
		t.Fatalf("events = %v, want %v", db.events, wantEvents)
	}
}

type fakeLedgerDB struct {
	applied map[string]bool
	events  []string
	execs   []string
	args    [][]any
	records []string
}

func newFakeLedgerDB() *fakeLedgerDB {
	return &fakeLedgerDB{applied: make(map[string]bool)}
}

func (db *fakeLedgerDB) Exec(_ context.Context, query string, args ...any) error {
	db.execs = append(db.execs, query)
	db.args = append(db.args, slices.Clone(args))

	if strings.HasPrefix(query, "INSERT INTO ") {
		name, ok := args[0].(string)
		if !ok {
			return fmt.Errorf("ledger insert arg 0 type = %T, want string", args[0])
		}

		if db.applied[name] {
			return nil
		}

		db.applied[name] = true
		db.records = append(db.records, name)
		db.events = append(db.events, "record:"+name)

		return nil
	}

	if !strings.HasPrefix(query, "CREATE TABLE ") {
		db.events = append(db.events, "exec:"+query)
	}

	return nil
}

func (db *fakeLedgerDB) QueryRow(_ context.Context, _ string, args ...any) Row {
	name, ok := args[0].(string)
	if !ok {
		return fakeLedgerRow{err: fmt.Errorf("ledger query arg 0 type = %T, want string", args[0])}
	}

	db.events = append(db.events, "query:"+name)

	return fakeLedgerRow{applied: db.applied[name]}
}

type fakeLedgerRow struct {
	applied bool
	err     error
}

func (r fakeLedgerRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}

	target, ok := dest[0].(*bool)
	if !ok {
		return fmt.Errorf("ledger scan dest 0 type = %T, want *bool", dest[0])
	}

	*target = r.applied

	return nil
}
