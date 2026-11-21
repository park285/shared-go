//go:build linux

package archive

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strconv"

	"golang.org/x/sys/unix"
)

func setLogMode(path string, observed os.FileInfo, want os.FileMode) error {
	fd, err := unix.Open(path, unix.O_PATH|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return fmt.Errorf("open log path for chmod: %w", err)
	}

	file := os.NewFile(uintptr(fd), path)

	defer func() { _ = file.Close() }()

	opened, err := file.Stat()
	if err != nil {
		return fmt.Errorf("stat opened log path: %w", err)
	}

	if !os.SameFile(observed, opened) {
		return fmt.Errorf("log path changed during permission update: %s", path)
	}

	err = os.Chmod("/proc/self/fd/"+strconv.Itoa(fd), want&^os.ModeType)
	if errors.Is(err, fs.ErrNotExist) || errors.Is(err, fs.ErrPermission) {
		return chmodThroughOpen(path, observed, want)
	}

	if err != nil {
		return fmt.Errorf("chmod log path: %w", err)
	}

	updated, err := file.Stat()
	if err != nil {
		return fmt.Errorf("verify log permissions: %w", err)
	}

	return requireLogMode(path, updated, want)
}
