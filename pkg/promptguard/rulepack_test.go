package promptguard

import (
	"os"
	"path/filepath"
	"testing"
)

func writeRulepackFile(t *testing.T, content string) string {
	t.Helper()

	dir := t.TempDir()

	path := filepath.Join(dir, "bad.yml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	return dir
}

func assertLoadRulepacksFails(t *testing.T, content string) {
	t.Helper()

	dir := writeRulepackFile(t, content)
	if _, err := loadRulepacks(dir, nil); err == nil {
		t.Fatal("expected lint error")
	}
}

func TestFindRulepackFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	for _, name := range []string{"b.yaml", "a.yml", "c.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(""), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	files := findRulepackFiles(dir)
	if len(files) != 2 {
		t.Fatalf("expected 2 rulepack files, got %d", len(files))
	}

	if filepath.Base(files[0]) != "a.yml" || filepath.Base(files[1]) != "b.yaml" {
		t.Fatalf("files = %#v, want sorted yaml files", files)
	}
}

func TestReadRulepackFileRejectsPathOutsideBaseDir(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	outside := filepath.Join(t.TempDir(), "outside.yml")
	if err := os.WriteFile(outside, []byte("version: 2\nrules: []\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := readRulepackFile(dir, outside); err == nil {
		t.Fatal("expected path validation error for file outside base dir")
	}
}

func TestCompileRulepackErrors(t *testing.T) {
	t.Parallel()

	_, err := compileRulepack(&rawRulepack{Rules: []rawRule{{ID: "x", Type: "unknown", Weight: 1}}})
	if err == nil {
		t.Fatal("expected error for unknown rule type")
	}
}

func TestLoadRulepacksRejectsSuspiciousEscapes(t *testing.T) {
	t.Parallel()

	assertLoadRulepacksFails(t, `
version: 2
rules:
  - id: bad
    type: regex
    action: block
    pattern: '(시스템).{0,12}(따르지\\s*마)'
    weight: 1.0
`)
}

func TestLoadRulepacksRejectsGenericBlockedPhrase(t *testing.T) {
	t.Parallel()

	assertLoadRulepacksFails(t, `
version: 2
rules:
  - id: bad
    type: phrases
    action: block
    phrases:
      - roleplay
    weight: 1.0
`)
}
