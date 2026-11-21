//go:build !linux

package archive

import (
	"errors"
	"fmt"
	"os"
)

func setLogMode(path string, observed os.FileInfo, want os.FileMode) error {
	err := chmodThroughOpen(path, observed, want)
	if !errors.Is(err, errLogPathUnopenable) {
		return err
	}

	current, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("stat unreadable log path: %w", err)
	}

	if !os.SameFile(observed, current) {
		return fmt.Errorf("log path changed during permission update: %s", path)
	}

	err = chmodNoFollow(path, want)
	if err != nil {
		return fmt.Errorf("chmod unreadable log path: %w", err)
	}

	updated, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("verify log permissions: %w", err)
	}

	if !os.SameFile(observed, updated) {
		return fmt.Errorf("log path changed during permission update: %s", path)
	}

	return requireLogMode(path, updated, want)
}
