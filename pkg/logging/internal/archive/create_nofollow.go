//go:build unix

package archive

import "syscall"

const (
	logFileCreateGuard = syscall.O_NOFOLLOW
	logModeOpenGuard   = syscall.O_NOFOLLOW | syscall.O_NONBLOCK
)
