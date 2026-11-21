//go:build windows || wasip1 || js

package archive

import (
	"errors"
	"fmt"
)

var errFileLoggingUnsupported = fmt.Errorf("file logging on this platform cannot enforce 0640 file and 0750 directory permissions: %w", errors.ErrUnsupported)

func EnsureLogFilePerm(string) error {
	return errFileLoggingUnsupported
}

func EnsureLogDirPerm(string) error {
	return errFileLoggingUnsupported
}
