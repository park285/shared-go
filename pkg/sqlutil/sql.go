package sqlutil

import (
	"fmt"
	"io/fs"
	"strings"
)

func MustQuery(fsys fs.FS, name string) string {
	data, err := fs.ReadFile(fsys, name)
	if err != nil {
		panic(fmt.Sprintf("missing SQL asset %s: %v", name, err))
	}

	query := strings.TrimSpace(string(data))
	if query == "" {
		panic(fmt.Sprintf("empty SQL asset %s", name))
	}

	return query
}
