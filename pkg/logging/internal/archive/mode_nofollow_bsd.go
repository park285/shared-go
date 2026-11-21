//go:build darwin || dragonfly || freebsd || netbsd || openbsd

package archive

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

func chmodNoFollow(path string, want os.FileMode) error {
	mode := uint32(want.Perm())
	if want&os.ModeSetuid != 0 {
		mode |= unix.S_ISUID
	}

	if want&os.ModeSetgid != 0 {
		mode |= unix.S_ISGID
	}

	if want&os.ModeSticky != 0 {
		mode |= unix.S_ISVTX
	}

	if err := unix.Fchmodat(unix.AT_FDCWD, path, mode, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return fmt.Errorf("fchmodat %s: %w", path, err)
	}

	return nil
}
