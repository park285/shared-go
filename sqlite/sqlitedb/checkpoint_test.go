package sqlitedb

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

func TestPeriodicCheckpointDoesNotWaitForReaders(t *testing.T) {
	db, err := Open(t.Context(), Options{
		Path:               filepath.Join(t.TempDir(), "state", "state.db"),
		ReadConns:          1,
		ConnMaxIdleTime:    time.Minute,
		BusyTimeout:        2 * time.Second,
		JournalSizeLimit:   8 << 20,
		CheckpointInterval: time.Hour,
	})
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	t.Cleanup(func() {
		if closeErr := db.Close(context.WithoutCancel(t.Context())); closeErr != nil {
			t.Errorf("close: %v", closeErr)
		}
	})

	ctx := t.Context()

	if _, execErr := db.Writer().ExecContext(ctx, "CREATE TABLE t (v INTEGER) STRICT; INSERT INTO t VALUES (1)"); execErr != nil {
		t.Fatalf("seed: %v", execErr)
	}

	snapshot, err := db.Reader().BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatalf("begin reader: %v", err)
	}

	defer func() {
		if rollbackErr := snapshot.Rollback(); rollbackErr != nil {
			t.Errorf("end reader: %v", rollbackErr)
		}
	}()

	var count int

	if scanErr := snapshot.QueryRowContext(ctx, "SELECT count(*) FROM t").Scan(&count); scanErr != nil {
		t.Fatalf("read: %v", scanErr)
	}

	if _, execErr := db.Writer().ExecContext(ctx, "INSERT INTO t VALUES (2)"); execErr != nil {
		t.Fatalf("write after the snapshot: %v", execErr)
	}

	started := time.Now()

	if checkpointErr := db.checkpointPassive(ctx); checkpointErr != nil {
		t.Fatalf("periodic checkpoint while a reader holds a snapshot: %v", checkpointErr)
	}

	if waited := time.Since(started); waited >= time.Second {
		t.Fatalf("periodic checkpoint waited %s for the reader", waited)
	}
}
