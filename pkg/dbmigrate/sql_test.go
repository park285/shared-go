package dbmigrate

import (
	"strings"
	"testing"
)

func TestSQLAssetsAreLoaded(t *testing.T) {
	t.Parallel()

	queries := []string{
		queryEnsureLedgerTemplate,
		queryLedgerAppliedTemplate,
		queryRecordLedgerTemplate,
		queryTryAdvisoryLock,
		queryAdvisoryUnlock,
	}
	for _, query := range queries {
		if strings.TrimSpace(query) == "" {
			t.Fatal("SQL asset is empty")
		}
	}
}

func TestSQLAssetRendering(t *testing.T) {
	t.Parallel()

	ensure := queryEnsureLedger(`"public"."schema_migrations"`)
	if !strings.Contains(ensure, `CREATE TABLE IF NOT EXISTS "public"."schema_migrations"`) {
		t.Fatalf("queryEnsureLedger() = %s, want ledger table replacement", ensure)
	}

	record := queryRecordLedger(`"schema_migrations"`, quoteSQLString("a'b.sql"))
	if !strings.Contains(record, "VALUES ('a''b.sql')") {
		t.Fatalf("queryRecordLedger() = %s, want escaped filename literal", record)
	}
}
