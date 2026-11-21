//go:build darwin || dragonfly || freebsd || netbsd || openbsd

package archive

func inaccessibleRepairSupported() bool {
	return true
}
