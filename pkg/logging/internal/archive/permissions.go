//go:build unix || plan9

package archive

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

func EnsureLogFilePerm(path string) error {
	path = filepath.Clean(path)

	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		err = createLogFile(path)
		if !errors.Is(err, fs.ErrExist) {
			return err
		}

		info, err = os.Lstat(path)
	}

	if err != nil {
		return fmt.Errorf("stat log file failed: %w", err)
	}

	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%w: %s", ErrLogPathSymlink, path)
	}

	if info.IsDir() {
		return fmt.Errorf("log path is directory: %s", path)
	}

	if !info.Mode().IsRegular() {
		return fmt.Errorf("log path is not a regular file: %s", path)
	}

	if info.Mode() == LogFilePerm {
		return nil
	}

	return setLogMode(path, info, LogFilePerm)
}

func createLogFile(path string) error {
	// #nosec G304 -- 로그 파일 경로는 애플리케이션 설정이며 외부 입력이 아니다.
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_APPEND|os.O_WRONLY|logFileCreateGuard, LogFilePerm)
	if err != nil {
		return fmt.Errorf("create log file failed: %w", err)
	}

	if err := file.Chmod(LogFilePerm); err != nil {
		return fmt.Errorf("chmod log file failed: %w", errors.Join(err, file.Close()))
	}

	if err := file.Close(); err != nil {
		return fmt.Errorf("close log file failed: %w", err)
	}

	return verifyLogMode(path, LogFilePerm)
}

func EnsureLogDirPerm(path string) error {
	path = filepath.Clean(path)

	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		if err = os.MkdirAll(path, LogDirPerm); err != nil {
			return fmt.Errorf("create log dir failed: %w", err)
		}

		info, err = os.Lstat(path)
	}

	if err != nil {
		return fmt.Errorf("stat log dir failed: %w", err)
	}

	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%w: %s", ErrLogPathSymlink, path)
	}

	if !info.IsDir() {
		return fmt.Errorf("log dir path is not directory: %s", path)
	}

	if info.Mode().Perm() == LogDirPerm {
		return nil
	}

	return setLogMode(path, info, info.Mode()&(os.ModeDir|os.ModeSetgid|os.ModeSticky)|LogDirPerm)
}

func verifyLogMode(path string, want os.FileMode) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("verify log permissions: %w", err)
	}

	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%w: %s", ErrLogPathSymlink, path)
	}

	if got := info.Mode(); got != want {
		return fmt.Errorf("log path %s: mode %s, want %s", path, got, want)
	}

	return nil
}
