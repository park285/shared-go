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
