package guardtext

import "testing"

func TestConfusablesGeneratedMetadata(t *testing.T) {
	t.Parallel()

	if confusablesUnicodeVersion != "17.0.0" {
		t.Fatalf("confusablesUnicodeVersion = %q, want %q", confusablesUnicodeVersion, "17.0.0")
	}

	if confusablesSourceSHA256 != "091c7f82fc39ef208faf8f94d29c244de99254675e09de163160c810d13ef22a" {
		t.Fatalf("confusablesSourceSHA256 = %q", confusablesSourceSHA256)
	}

	if got := len(confusablesMap); got != 6565 {
		t.Fatalf("len(confusablesMap) = %d, want 6565", got)
	}

	if unicodeDataBaselineSHA256 != "806e9aed65037197f1ec85e12be6e8cd870fc5608b4de0fffd990f689f376a73" {
		t.Fatalf("unicodeDataBaselineSHA256 = %q", unicodeDataBaselineSHA256)
	}

	if unicodeDataSourceSHA256 != "2e1efc1dcb59c575eedf5ccae60f95229f706ee6d031835247d843c11d96470c" {
		t.Fatalf("unicodeDataSourceSHA256 = %q", unicodeDataSourceSHA256)
	}

	if got := len(unicode17CanonicalDecompositionDelta); got != 20 {
		t.Fatalf("len(unicode17CanonicalDecompositionDelta) = %d, want 20", got)
	}

	if got := len(unicode17CanonicalCombiningClassDelta); got != 46 {
		t.Fatalf("len(unicode17CanonicalCombiningClassDelta) = %d, want 46", got)
	}
}

func TestUnicode17NFDUsesCanonicalDecompositionDelta(t *testing.T) {
	t.Parallel()

	if got, want := unicode17NFD("\U000105C9"), "\U000105D2\u0307"; got != want {
		t.Fatalf("unicode17NFD() = %U, want %U", []rune(got), []rune(want))
	}
}

func TestUnicode17NFDReordersNewCombiningClass(t *testing.T) {
	t.Parallel()

	if got, want := unicode17NFD("a\u1AE7\u0323"), "a\u0323\u1AE7"; got != want {
		t.Fatalf("unicode17NFD() = %U, want %U", []rune(got), []rune(want))
	}
}
