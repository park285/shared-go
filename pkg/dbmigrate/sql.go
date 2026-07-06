package dbmigrate

import (
	"embed"
	"fmt"
	"strings"
)

//go:embed queries/*.sql queries/*.sql.tpl
var queryFS embed.FS

func mustQuery(name string) string {
	b, err := queryFS.ReadFile(name)
	if err != nil {
		panic(fmt.Sprintf("missing SQL asset %s: %v", name, err))
	}
	q := strings.TrimSpace(string(b))
	if q == "" {
		panic(fmt.Sprintf("empty SQL asset %s", name))
	}
	return q
}

var (
	queryEnsureLedgerTemplate  = mustQuery("queries/ensure_ledger.sql.tpl")
	queryLedgerAppliedTemplate = mustQuery("queries/ledger_applied.sql.tpl")
	queryRecordLedgerTemplate  = mustQuery("queries/record_ledger.sql.tpl")
	queryTryAdvisoryLock       = mustQuery("queries/try_advisory_lock.sql")
	queryAdvisoryUnlock        = mustQuery("queries/advisory_unlock.sql")
)

func queryEnsureLedger(table string) string {
	return strings.ReplaceAll(queryEnsureLedgerTemplate, "{{ledger_table}}", table)
}

func queryLedgerApplied(table string) string {
	return strings.ReplaceAll(queryLedgerAppliedTemplate, "{{ledger_table}}", table)
}

func queryRecordLedger(table, filenameLiteral string) string {
	q := strings.ReplaceAll(queryRecordLedgerTemplate, "{{ledger_table}}", table)
	return strings.ReplaceAll(q, "{{filename_literal}}", filenameLiteral)
}
