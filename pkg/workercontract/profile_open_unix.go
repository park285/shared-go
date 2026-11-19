//go:build unix

package workercontract

import (
	"errors"
	"os"
	"syscall"
)

func openRegularProfile(path string) (*os.File, error) {
	//nolint:gosec // 운영자가 지정한 profile 경로를 no-follow로 열고 같은 descriptor를 검사한다.
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		if errors.Is(err, syscall.ELOOP) {
			return nil, profileFileError{code: ProfileFileTypeInvalid}
		}

		return nil, profileOpenError(err)
	}

	info, err := file.Stat()
	if err != nil {
		closeErr := file.Close()

		return nil, profileOpenError(errors.Join(err, closeErr))
	}

	if !info.Mode().IsRegular() {
		if closeErr := file.Close(); closeErr != nil {
			return nil, profileOpenError(closeErr)
		}

		return nil, profileFileError{code: ProfileFileTypeInvalid}
	}

	return file, nil
}
