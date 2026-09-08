package kakaoformat

import (
	"strings"
	"testing"
)

func TestRenderPreservesInvalidInput(t *testing.T) {
	t.Parallel()

	for _, input := range []string{"\xff", " \xff ", "**\xff**", "\x00CODE0\x00"} {
		if got := Render(input); got != input {
			t.Fatalf("invalid input changed: got %q, want %q", got, input)
		}
	}
}

func TestRenderPreservesNestedFenceSurroundings(t *testing.T) {
	t.Parallel()

	const input = "````text\nbefore\n```go\nx\n```\nafter\n\n\n````"

	const body = "before\n```go\nx\n```\nafter\n\n\n"

	got := Render(input)
	if !strings.Contains(got, body) || !strings.HasPrefix(got, "┏━━━━━ text ━━━━━┓") {
		t.Fatalf("fence contents were changed: %q", got)
	}
}
