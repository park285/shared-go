package dbmigrate

import (
	"embed"
	"strings"

	"github.com/park285/shared-go/v2/pkg/sqlutil"
)

//go:embed queries/*.sql queries/*.sql.tpl
var queryFS embed.FS

var (
	ensureLedgerTemplateSQL  = sqlutil.MustQuery(queryFS, "queries/ensure_ledger.sql.tpl")
	ledgerAppliedTemplateSQL = sqlutil.MustQuery(queryFS, "queries/ledger_applied.sql.tpl")
	recordLedgerTemplateSQL  = sqlutil.MustQuery(queryFS, "queries/record_ledger.sql.tpl")
	setLockTimeoutSQL        = sqlutil.MustQuery(queryFS, "queries/set_lock_timeout.sql")
	setStatementTimeoutSQL   = sqlutil.MustQuery(queryFS, "queries/set_statement_timeout.sql")
)

func queryEnsureLedger(table string) string {
	return strings.ReplaceAll(ensureLedgerTemplateSQL, "{{ledger_table}}", table)
}

func queryLedgerApplied(table string) string {
	return strings.ReplaceAll(ledgerAppliedTemplateSQL, "{{ledger_table}}", table)
}

func queryRecordLedger(table string) string {
	return strings.ReplaceAll(recordLedgerTemplateSQL, "{{ledger_table}}", table)
}
