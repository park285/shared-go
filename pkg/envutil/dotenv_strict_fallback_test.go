//go:build !unix

package envutil

import (
	"os"
	"path/filepath"
	"testing"
)

// no-follow open을 지원하지 않는 플랫폼에서 strict dotenv는 의도적으로 항상 실패한다.
func TestLoadDotenvFileStrictFailsClosedWithoutNoFollow(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fallback.env")
	if err := os.WriteFile(path, []byte("FALLBACK_STRICT_KEY=value\n"), 0o600); err != nil {
		t.Fatalf("write dotenv: %v", err)
	}
	t.Cleanup(func() { _ = os.Unsetenv("FALLBACK_STRICT_KEY") })

	if err := LoadDotenvFile(path, true, true); err == nil {
		t.Fatal("LoadDotenvFile(strict) error = nil, want fail-closed on this platform")
	}
	if got := os.Getenv("FALLBACK_STRICT_KEY"); got != "" {
		t.Fatalf("FALLBACK_STRICT_KEY = %q, want empty", got)
	}

	if err := LoadDotenvFile(path, true, false); err != nil {
		t.Fatalf("LoadDotenvFile(non-strict) error = %v, want local dotenv to keep working", err)
	}
	if got := os.Getenv("FALLBACK_STRICT_KEY"); got != "value" {
		t.Fatalf("FALLBACK_STRICT_KEY = %q, want value", got)
	}
}
