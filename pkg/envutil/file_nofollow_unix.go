//go:build unix

package envutil

import (
	"os"
	"syscall"
)

func openSecretFileNoFollow(filePath string) (*os.File, error) {
	// O_NOFOLLOW는 Lstat 검사 후 경로가 symlink로 바뀌는 TOCTOU 경쟁을 막는 유일한 가드라 제거하면 안 됩니다.
	//nolint:gosec // *_FILE env vars are intentional operator-supplied secret file paths.
	return os.OpenFile(filePath, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
}
