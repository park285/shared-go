package sqlutil

import (
	"fmt"
	"strings"
	"testing"
	"testing/fstest"
)

func TestMustQuery(t *testing.T) {
	fsys := fstest.MapFS{
		"query.sql": &fstest.MapFile{Data: []byte("  SELECT 1;\n")},
	}
	if got := MustQuery(fsys, "query.sql"); got != "SELECT 1;" {
		t.Fatalf("MustQuery() = %q, want trimmed query", got)
	}
}

func TestMustQueryPanicsForMissingAsset(t *testing.T) {
	assertPanicContains(t, "missing SQL asset missing.sql", func() {
		MustQuery(fstest.MapFS{}, "missing.sql")
	})
}

func TestMustQueryPanicsForEmptyAsset(t *testing.T) {
	fsys := fstest.MapFS{"empty.sql": &fstest.MapFile{Data: []byte(" \n\t")}}

	assertPanicContains(t, "empty SQL asset empty.sql", func() {
		MustQuery(fsys, "empty.sql")
	})
}

func assertPanicContains(t *testing.T, want string, fn func()) {
	t.Helper()

	defer func() {
		value := recover()
		if value == nil {
			t.Fatal("panic = nil")
		}

		if got := fmt.Sprint(value); !strings.HasPrefix(got, want) {
			t.Fatalf("panic = %q, want prefix %q", got, want)
		}
	}()

	fn()
}
