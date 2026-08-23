package dbmigrate

import (
	"embed"
	"strings"

	"github.com/park285/shared-go/v2/pkg/sqlutil"
)

//go:embed queries/*.sql queries/*.sql.tpl
var queryFS embed.FS

func mustQuery(name string) string {
	return sqlutil.MustQuery(queryFS, name)
}

var (
	queryEnsureLedgerTemplate  = mustQuery("queries/ensure_ledger.sql.tpl")
	queryLedgerAppliedTemplate = mustQuery("queries/ledger_applied.sql.tpl")
	queryRecordLedgerTemplate  = mustQuery("queries/record_ledger.sql.tpl")
	queryTryAdvisoryLock       = mustQuery("queries/try_advisory_lock.sql")
	queryAdvisoryUnlock        = mustQuery("queries/advisory_unlock.sql")
	querySetLockTimeout        = mustQuery("queries/set_lock_timeout.sql")
	querySetStatementTimeout   = mustQuery("queries/set_statement_timeout.sql")
)

func queryEnsureLedger(table string) string {
	return strings.ReplaceAll(queryEnsureLedgerTemplate, "{{ledger_table}}", table)
}

func queryLedgerApplied(table string) string {
	return strings.ReplaceAll(queryLedgerAppliedTemplate, "{{ledger_table}}", table)
}

func queryRecordLedger(table string) string {
	return strings.ReplaceAll(queryRecordLedgerTemplate, "{{ledger_table}}", table)
}
