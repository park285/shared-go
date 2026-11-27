package sqlitedb

import (
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"time"
)

type Options struct {
	Path               string
	ReadConns          int
	ConnMaxIdleTime    time.Duration
	BusyTimeout        time.Duration
	JournalSizeLimit   int64
	CheckpointInterval time.Duration
	Logger             *slog.Logger
}

func (o Options) validate() error {
	var problems []error

	if o.Path == "" || !filepath.IsAbs(o.Path) || filepath.Clean(o.Path) != o.Path {
		problems = append(problems, fmt.Errorf("path must be a clean absolute path: %q", o.Path))
	}

	if o.ReadConns < 1 {
		problems = append(problems, fmt.Errorf("read connections must be positive: %d", o.ReadConns))
	}

	if o.ConnMaxIdleTime <= 0 {
		problems = append(problems, fmt.Errorf("connection max idle time must be positive: %s", o.ConnMaxIdleTime))
	}

	if o.BusyTimeout < time.Millisecond {
		problems = append(problems, fmt.Errorf("busy timeout must be at least 1ms: %s", o.BusyTimeout))
	}

	if o.JournalSizeLimit <= 0 {
		problems = append(problems, fmt.Errorf("journal size limit must be positive: %d", o.JournalSizeLimit))
	}

	if o.CheckpointInterval <= 0 {
		problems = append(problems, fmt.Errorf("checkpoint interval must be positive: %s", o.CheckpointInterval))
	}

	return errors.Join(problems...)
}
