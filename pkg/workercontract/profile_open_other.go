//go:build !unix && !windows

package workercontract

import (
	"errors"
	"os"
)

// 이 플랫폼에는 공통 no-follow open API가 없어 기존 inode 검사를 유지한다.
func openRegularProfile(path string) (*os.File, error) {
	before, err := os.Lstat(path)
	if err != nil {
		return nil, profileOpenError(err)
	}

	if !before.Mode().IsRegular() {
		return nil, profileFileError{code: ProfileFileTypeInvalid}
	}

	//nolint:gosec // 운영자가 지정한 profile 경로이며 열린 descriptor의 identity를 검사한다.
	file, err := os.Open(path)
	if err != nil {
		return nil, profileOpenError(err)
	}

	after, lstatErr := os.Lstat(path)
	opened, statErr := file.Stat()
	if lstatErr != nil || statErr != nil || !after.Mode().IsRegular() || !opened.Mode().IsRegular() ||
		!os.SameFile(before, after) || !os.SameFile(after, opened) {
		return nil, profileOpenError(errors.Join(profileFileError{code: ProfileFileTypeInvalid}, file.Close()))
	}

	return file, nil
}
