package archive

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

var ErrLogPathSymlink = errors.New("log path is a symlink")

func EnsureLogFilePerm(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		if ensureErr := ensureMissingLogFile(path, err); ensureErr != nil {
			return fmt.Errorf("ensure missing log file: %w", ensureErr)
		}

		return nil
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

	if info.Mode().Perm() == LogFilePerm {
		return nil
	}

	if chmodErr := os.Chmod(path, LogFilePerm); chmodErr != nil {
		return fmt.Errorf("chmod log file failed: %w", chmodErr)
	}

	return nil
}

func ensureMissingLogFile(path string, statErr error) error {
	if !errors.Is(statErr, os.ErrNotExist) {
		return fmt.Errorf("stat log file failed: %w", statErr)
	}

	// #nosec G304 -- 로그 파일 경로는 애플리케이션 설정이며 외부 입력이 아니다.
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY|syscall.O_NOFOLLOW, LogFilePerm)
	if err != nil {
		return fmt.Errorf("create log file failed: %w", err)
	}

	if err := file.Chmod(LogFilePerm); err != nil {
		chmodErr := fmt.Errorf("chmod log file failed: %w", err)

		if closeErr := file.Close(); closeErr != nil {
			if err := errors.Join(chmodErr, fmt.Errorf("close log file failed: %w", closeErr)); err != nil {
				return fmt.Errorf("ensure missing log file: %w", err)
			}

			return nil
		}

		return chmodErr
	}

	if err := file.Close(); err != nil {
		return fmt.Errorf("close log file failed: %w", err)
	}

	return nil
}

func EnsureLogDirPerm(path string) error {
	if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%w: %s", ErrLogPathSymlink, path)
	}

	if err := os.MkdirAll(path, LogDirPerm); err != nil {
		return fmt.Errorf("create log dir failed: %w", err)
	}

	info, err := os.Lstat(path)
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

	if chmodErr := os.Chmod(path, LogDirPerm); chmodErr != nil {
		return fmt.Errorf("chmod log dir failed: %w", chmodErr)
	}

	return nil
}
