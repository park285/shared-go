package logging

import (
	"errors"
	"fmt"
	"os"
)

func ensureLogFilePerm(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return ensureMissingLogFile(path, err)
	}

	if info.IsDir() {
		return fmt.Errorf("log path is directory: %s", path)
	}

	if info.Mode().Perm() == logFilePerm {
		return nil
	}

	if chmodErr := os.Chmod(path, logFilePerm); chmodErr != nil {
		return fmt.Errorf("chmod log file failed: %w", chmodErr)
	}
	return nil
}

func ensureMissingLogFile(path string, statErr error) error {
	if !errors.Is(statErr, os.ErrNotExist) {
		return fmt.Errorf("stat log file failed: %w", statErr)
	}

	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, logFilePerm)
	if err != nil {
		return fmt.Errorf("create log file failed: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close log file failed: %w", err)
	}
	return nil
}

func ensureLogDirPerm(path string) error {
	if err := os.MkdirAll(path, logDirPerm); err != nil {
		return fmt.Errorf("create log dir failed: %w", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat log dir failed: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("log dir path is not directory: %s", path)
	}
	if info.Mode().Perm() == logDirPerm {
		return nil
	}

	if chmodErr := os.Chmod(path, logDirPerm); chmodErr != nil {
		return fmt.Errorf("chmod log dir failed: %w", chmodErr)
	}
	return nil
}
