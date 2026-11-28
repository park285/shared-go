package sqlitedb

import (
	"embed"
	"fmt"
	"io/fs"
)

//go:embed queries/*.sql
var queryFS embed.FS

var (
	walCheckpointSQL         = MustQuery(queryFS, "queries/wal_checkpoint.sql")
	walCheckpointPassiveSQL  = MustQuery(queryFS, "queries/wal_checkpoint_passive.sql")
	writerSettingsSQL        = MustQuery(queryFS, "queries/writer_settings.sql")
	createMigrationLedgerSQL = MustQuery(queryFS, "queries/create_migration_ledger.sql")
	insertMigrationSQL       = MustQuery(queryFS, "queries/insert_migration.sql")
	selectMigrationsSQL      = MustQuery(queryFS, "queries/select_migrations.sql")
	foreignKeysOffSQL        = MustQuery(queryFS, "queries/foreign_keys_off.sql")
	foreignKeysOnSQL         = MustQuery(queryFS, "queries/foreign_keys_on.sql")
	foreignKeysStateSQL      = MustQuery(queryFS, "queries/foreign_keys_state.sql")
	foreignKeyCheckSQL       = MustQuery(queryFS, "queries/foreign_key_check.sql")
	noBusyWaitSQL            = MustQuery(queryFS, "queries/no_busy_wait.sql")
	optimizeOpenSQL          = MustQuery(queryFS, "queries/optimize_open.sql")
	optimizeSQL              = MustQuery(queryFS, "queries/optimize.sql")
)

func MustQuery(fsys fs.FS, name string) string {
	body, err := fs.ReadFile(fsys, name)
	if err != nil {
		panic(fmt.Sprintf("sqlitedb: load query %s: %v", name, err))
	}

	if len(body) == 0 {
		panic(fmt.Sprintf("sqlitedb: query %s is empty", name))
	}

	return string(body)
}
