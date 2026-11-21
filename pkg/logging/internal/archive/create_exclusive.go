//go:build plan9

package archive

import "os"

const logFileCreateGuard = os.O_EXCL
