//go:build !linux && !darwin && !dragonfly && !freebsd && !netbsd && !openbsd

package archive

import (
	"errors"
	"os"
)

func chmodNoFollow(string, os.FileMode) error {
	return errors.ErrUnsupported
}
