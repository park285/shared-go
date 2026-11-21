package archive

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
)

var errLogPathUnopenable = errors.New("log path cannot be opened for chmod")

func chmodThroughOpen(path string, observed os.FileInfo, want os.FileMode) error {
	// #nosec G304 -- 로그 파일 경로는 애플리케이션 설정이며 외부 입력이 아니다.
	file, err := os.OpenFile(path, os.O_RDONLY|logModeOpenGuard, 0)
	if errors.Is(err, fs.ErrPermission) && observed.Mode().IsRegular() {
		// #nosec G304 -- 로그 파일 경로는 애플리케이션 설정이며 외부 입력이 아니다.
		file, err = os.OpenFile(path, os.O_WRONLY|os.O_APPEND|logModeOpenGuard, 0)
	}

	if errors.Is(err, fs.ErrPermission) {
		return fmt.Errorf("%w: %w", errLogPathUnopenable, err)
	}

	if err != nil {
		return fmt.Errorf("open log path for chmod: %w", err)
	}

	defer func() { _ = file.Close() }()

	opened, err := file.Stat()
	if err != nil {
		return fmt.Errorf("stat opened log path: %w", err)
	}

	if !os.SameFile(observed, opened) {
		return fmt.Errorf("log path changed during permission update: %s", path)
	}

	err = file.Chmod(want &^ os.ModeType)
	if err != nil {
		return fmt.Errorf("chmod log path: %w", err)
	}

	updated, err := file.Stat()
	if err != nil {
		return fmt.Errorf("verify log permissions: %w", err)
	}

	return requireLogMode(path, updated, want)
}

func requireLogMode(path string, updated os.FileInfo, want os.FileMode) error {
	if got := updated.Mode(); got != want && got != want&^os.ModeSetgid {
		return fmt.Errorf("log path %s: mode %s, want %s", path, got, want)
	}

	return nil
}
