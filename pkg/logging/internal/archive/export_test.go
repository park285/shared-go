package archive

import "os"

func setReadDirFn(fn func(string) ([]os.DirEntry, error)) func() {
	prev := readDirFn
	readDirFn = fn
	return func() { readDirFn = prev }
}
