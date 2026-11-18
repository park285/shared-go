//go:build unix

package envutil

import (
	"fmt"
	"os"
	"syscall"
)

func openSecretFileNoFollow(filePath string) (*os.File, error) {
	//nolint:gosec // *_FILE 환경 변수는 운영자가 지정하는 비밀 파일 경로다.
	out, err := os.OpenFile(filePath, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, fmt.Errorf("open file: %w", err)
	}

	return out, nil
}
