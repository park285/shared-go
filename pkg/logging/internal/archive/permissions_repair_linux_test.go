package archive

import "os"

func inaccessibleRepairSupported() bool {
	_, err := os.Stat("/proc/self/fd")

	return err == nil || os.Geteuid() == 0
}
