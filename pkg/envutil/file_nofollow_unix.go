//go:build unix

package envutil

import (
	"os"
	"syscall"
)

func openSecretFileNoFollow(filePath string) (*os.File, error) {
	// O_NOFOLLOW는 Lstat 검사 후 경로가 symlink로 바뀌는 TOCTOU 경쟁을 막는 유일한 가드라 제거하면 안 됩니다.
	// O_NONBLOCK은 같은 경쟁에서 경로가 FIFO로 바뀌었을 때 open이 writer를 무기한 기다리는 것을 막습니다.
	// 두 호출자 모두 open 직후 fstat으로 regular file을 확인하므로 이 flag가 read 의미를 바꾸지 않습니다.
	//nolint:gosec // *_FILE env vars are intentional operator-supplied secret file paths.
	return os.OpenFile(filePath, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
}
