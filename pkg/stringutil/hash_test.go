package stringutil

import "testing"

func TestTruncatedHash(t *testing.T) {
	t.Parallel()

	if got, want := TruncatedHash("abc"), "ba7816bf8f01cfea414140de5dae2223"; got != want {
		t.Fatalf("TruncatedHash(abc) = %q, want %q", got, want)
	}
	if got, want := TruncatedHash(""), "e3b0c44298fc1c149afbf4c8996fb924"; got != want {
		t.Fatalf("TruncatedHash(empty) = %q, want %q", got, want)
	}
	if TruncatedHash(" abc ") == TruncatedHash("abc") {
		t.Fatal("TruncatedHash(padded) must differ from TruncatedHash(trimmed)")
	}
}

func TestTruncatedLogHash(t *testing.T) {
	t.Parallel()

	if got := TruncatedLogHash(""); got != "" {
		t.Fatalf("TruncatedLogHash(empty) = %q, want empty", got)
	}
	if got := TruncatedLogHash("   "); got != "" {
		t.Fatalf("TruncatedLogHash(blank) = %q, want empty", got)
	}
	if got, want := TruncatedLogHash(" a  b "), "c8687a08aa5d6ed2044328fa6a697ab8"; got != want {
		t.Fatalf("TruncatedLogHash(padded) = %q, want %q", got, want)
	}
	if got, want := TruncatedLogHash("a b"), TruncatedLogHash(" a  b "); got != want {
		t.Fatalf("TruncatedLogHash normalization mismatch: %q != %q", got, want)
	}
	if got := len(TruncatedLogHash("payload")); got != 32 {
		t.Fatalf("TruncatedLogHash length = %d, want 32", got)
	}
}
