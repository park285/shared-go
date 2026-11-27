package sqlitedb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/ncruces/go-sqlite3/driver"
)

var (
	ErrInUse                = errors.New("sqlitedb: database is open in another process")
	ErrSharedDirectory      = errors.New("sqlitedb: database directory is accessible to group or others")
	ErrCheckpointIncomplete = errors.New("sqlitedb: wal checkpoint did not complete")
)

type DB struct {
	write     *sql.DB
	read      *sql.DB
	lock      *os.File
	logger    *slog.Logger
	stop      context.CancelFunc
	stopped   chan struct{}
	closeOnce sync.Once
	closeErr  error
}

func Open(ctx context.Context, opts Options) (*DB, error) {
	if err := opts.validate(); err != nil {
		return nil, fmt.Errorf("sqlitedb: options: %w", err)
	}

	if err := checkPrivateDirectory(filepath.Dir(opts.Path)); err != nil {
		return nil, err
	}

	lock, err := acquireLock(opts.Path + ".lock")
	if err != nil {
		return nil, err
	}

	write, read, err := openPools(ctx, opts, "rwc")
	if err != nil {
		return nil, errors.Join(err, lock.Close())
	}

	loopCtx, stop := context.WithCancel(context.WithoutCancel(ctx))
	db := &DB{write: write, read: read, lock: lock, logger: loggerOf(opts), stop: stop, stopped: make(chan struct{})}

	go db.runCheckpoints(loopCtx, opts.CheckpointInterval)

	return db, nil
}

func Attach(ctx context.Context, opts Options) (*DB, error) {
	if err := opts.validate(); err != nil {
		return nil, fmt.Errorf("sqlitedb: options: %w", err)
	}

	if _, err := os.Stat(opts.Path); err != nil {
		return nil, fmt.Errorf("sqlitedb: attach: %w", err)
	}

	write, read, err := openPools(ctx, opts, "rw")
	if err != nil {
		return nil, err
	}

	return &DB{write: write, read: read, logger: loggerOf(opts)}, nil
}

func loggerOf(opts Options) *slog.Logger {
	if opts.Logger == nil {
		return slog.Default()
	}

	return opts.Logger
}

func (db *DB) Writer() *sql.DB {
	return db.write
}

func (db *DB) Reader() *sql.DB {
	return db.read
}

func (db *DB) Checkpoint(ctx context.Context) error {
	var busy, frames, checkpointed int64

	if err := db.write.QueryRowContext(ctx, walCheckpointSQL).Scan(&busy, &frames, &checkpointed); err != nil {
		return fmt.Errorf("sqlitedb: wal checkpoint: %w", err)
	}

	if busy != 0 {
		return fmt.Errorf("%w: %d of %d frames", ErrCheckpointIncomplete, checkpointed, frames)
	}

	return nil
}

func (db *DB) checkpointPassive(ctx context.Context) error {
	var busy, frames, checkpointed int64

	if err := db.write.QueryRowContext(ctx, walCheckpointPassiveSQL).Scan(&busy, &frames, &checkpointed); err != nil {
		return fmt.Errorf("sqlitedb: passive wal checkpoint: %w", err)
	}

	return nil
}

func (db *DB) Close(ctx context.Context) error {
	db.closeOnce.Do(func() {
		owner := db.lock != nil
		if owner {
			db.stop()
			<-db.stopped
		}

		var errs []error

		if err := db.read.Close(); err != nil {
			errs = append(errs, fmt.Errorf("sqlitedb: close readers: %w", err))
		}

		if owner {
			if err := db.Checkpoint(ctx); err != nil {
				errs = append(errs, err)
			}
		}

		if err := db.write.Close(); err != nil {
			errs = append(errs, fmt.Errorf("sqlitedb: close writer: %w", err))
		}

		if owner {
			if err := db.lock.Close(); err != nil {
				errs = append(errs, fmt.Errorf("sqlitedb: release lock: %w", err))
			}
		}

		db.closeErr = errors.Join(errs...)
	})

	return db.closeErr
}

func (db *DB) runCheckpoints(ctx context.Context, interval time.Duration) {
	defer close(db.stopped)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := db.checkpointPassive(ctx); err != nil && ctx.Err() == nil {
				db.logger.Warn("sqlitedb: periodic wal checkpoint failed", slog.Any("error", err))
			}
		}
	}
}

func checkPrivateDirectory(dir string) error {
	if err := os.Mkdir(dir, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
		return fmt.Errorf("sqlitedb: create database directory: %w", err)
	}

	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("sqlitedb: database directory: %w", err)
	}

	if !info.IsDir() {
		return fmt.Errorf("sqlitedb: database directory is not a directory: %s", dir)
	}

	if info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("%w: %s has mode %s", ErrSharedDirectory, dir, info.Mode().Perm())
	}

	return nil
}

func acquireLock(path string) (*os.File, error) {
	file, err := os.OpenFile(filepath.Clean(path), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, fmt.Errorf("sqlitedb: open lock file: %w", err)
	}

	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		closeErr := file.Close()

		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, errors.Join(ErrInUse, closeErr)
		}

		return nil, errors.Join(fmt.Errorf("sqlitedb: lock database: %w", err), closeErr)
	}

	return file, nil
}

func openPools(ctx context.Context, opts Options, mode string) (*sql.DB, *sql.DB, error) {
	busy := strconv.FormatInt(opts.BusyTimeout.Milliseconds(), 10)

	write, err := openPool(ctx, dataSource(opts.Path, mode, "immediate",
		"busy_timeout("+busy+")",
		"journal_mode(wal)",
		"synchronous(full)",
		"foreign_keys(1)",
		"journal_size_limit("+strconv.FormatInt(opts.JournalSizeLimit, 10)+")",
	), 1, opts.ConnMaxIdleTime)
	if err != nil {
		return nil, nil, fmt.Errorf("sqlitedb: open writer: %w", err)
	}

	if verifyErr := verifyWriter(ctx, write, opts); verifyErr != nil {
		return nil, nil, errors.Join(verifyErr, write.Close())
	}

	read, err := openPool(ctx, dataSource(opts.Path, "rw", "deferred",
		"busy_timeout("+busy+")",
		"foreign_keys(1)",
		"query_only(1)",
	), opts.ReadConns, opts.ConnMaxIdleTime)
	if err != nil {
		return nil, nil, errors.Join(fmt.Errorf("sqlitedb: open readers: %w", err), write.Close())
	}

	return write, read, nil
}

func dataSource(path, mode, txlock string, pragmas ...string) string {
	query := url.Values{"mode": {mode}, "_txlock": {txlock}, "_pragma": pragmas}
	source := url.URL{Scheme: "file", OmitHost: true, Path: path, RawQuery: query.Encode()}

	return source.String()
}

func openPool(ctx context.Context, source string, conns int, idle time.Duration) (*sql.DB, error) {
	pool, err := driver.Open(source)
	if err != nil {
		return nil, fmt.Errorf("sqlitedb: open pool: %w", err)
	}

	pool.SetMaxOpenConns(conns)
	pool.SetMaxIdleConns(conns)
	pool.SetConnMaxIdleTime(idle)

	if err := pool.PingContext(ctx); err != nil {
		return nil, errors.Join(fmt.Errorf("sqlitedb: ping: %w", err), pool.Close())
	}

	return pool, nil
}

func verifyWriter(ctx context.Context, write *sql.DB, opts Options) error {
	var (
		mode        string
		synchronous int64
		foreignKeys int64
		limit       int64
		busy        int64
	)

	if err := write.QueryRowContext(ctx, writerSettingsSQL).Scan(&mode, &synchronous, &foreignKeys, &limit, &busy); err != nil {
		return fmt.Errorf("sqlitedb: read writer settings: %w", err)
	}

	if mode != "wal" || synchronous != 2 || foreignKeys != 1 || limit != opts.JournalSizeLimit || busy != opts.BusyTimeout.Milliseconds() {
		return fmt.Errorf("sqlitedb: writer settings not applied: journal_mode=%s synchronous=%d foreign_keys=%d journal_size_limit=%d busy_timeout=%d",
			mode, synchronous, foreignKeys, limit, busy)
	}

	return nil
}
