//go:build !unix

package envutil

import (
	"errors"
	"os"
)

var errSecretFileNoFollowUnsupported = errors.New("secret file no-follow open is unsupported on this platform")

func openSecretFileNoFollow(filePath string) (*os.File, error) {
	return nil, errSecretFileNoFollowUnsupported
}
