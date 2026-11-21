//go:build unix && !linux && !darwin && !dragonfly && !freebsd && !netbsd && !openbsd

package archive

import "os"

func inaccessibleRepairSupported() bool {
	return os.Geteuid() == 0
}
