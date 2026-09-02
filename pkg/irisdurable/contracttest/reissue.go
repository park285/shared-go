package contracttest

import (
	"context"
	"errors"
	"testing"

	"github.com/park285/shared-go/v2/pkg/irisdurable"
)

// ReissueFixture는 구현의 reissue ladder와 그 구현이 재발급 트리거로 쓰는 오류 판정이다.
type ReissueFixture struct {
	Ladder irisdurable.ReissueLadder
	// PreHandoffConflict는 CLIENT_REQUEST_ID_FAILED 409에서만 참이어야 하는 판정이다.
	PreHandoffConflict func(error) bool
	// NewPreHandoffConflict는 판정이 참이 되는 오류를 만든다.
	NewPreHandoffConflict func() error
	// NewTerminalConflict는 판정이 거짓이어야 하는 오류(outcome unknown, payload mismatch 등)를 만든다.
	NewTerminalConflict func() error
}

func (f ReissueFixture) complete() bool {
	return f.Ladder.MaxGenerations > 0 && f.Ladder.Derive != nil &&
		f.PreHandoffConflict != nil && f.NewPreHandoffConflict != nil && f.NewTerminalConflict != nil
}

func runReissue(t *testing.T, fixture ReissueFixture) {
	t.Helper()

	if !fixture.complete() {
		t.Fatal("ReissueFixture is incomplete")
	}

	t.Run("ConflictPredicateSeparatesPreHandoffFromTerminal", func(t *testing.T) { testReissuePredicate(t, fixture) })
	t.Run("GenerationsAreBounded", func(t *testing.T) { testReissueBounded(t, fixture.Ladder) })
	t.Run("ReissuedBaseIsRejected", func(t *testing.T) { testReissueNestedBase(t, fixture.Ladder) })
	t.Run("GenerationOfRecoversStoredID", func(t *testing.T) { testReissueGenerationOf(t, fixture.Ladder) })
	t.Run("RunReissuesOnlyOnPreHandoffConflict", func(t *testing.T) { testReissueRunSucceedsAfterConflicts(t, fixture) })
	t.Run("RunPreservesTerminalConflict", func(t *testing.T) { testReissueRunPreservesTerminal(t, fixture) })
	t.Run("RunExhaustsAfterMaxGenerations", func(t *testing.T) { testReissueRunExhausts(t, fixture) })
}

func testReissuePredicate(t *testing.T, fixture ReissueFixture) {
	t.Helper()

	if !fixture.PreHandoffConflict(fixture.NewPreHandoffConflict()) {
		t.Fatal("pre-handoff conflict must satisfy the reissue predicate")
	}

	if fixture.PreHandoffConflict(fixture.NewTerminalConflict()) {
		t.Fatal("terminal conflict must not satisfy the reissue predicate")
	}
}

func testReissueBounded(t *testing.T, ladder irisdurable.ReissueLadder) {
	t.Helper()

	base := uniqueID("contract:v1")
	seen := make(map[string]struct{}, ladder.MaxGenerations+1)

	for generation := range ladder.MaxGenerations + 1 {
		id, err := ladder.ClientRequestID(base, generation)
		if err != nil {
			t.Fatalf("generation %d: %v", generation, err)
		}

		if _, dup := seen[id]; dup {
			t.Fatalf("generation %d reused id %q", generation, id)
		}

		seen[id] = struct{}{}
	}

	if _, err := ladder.ClientRequestID(base, ladder.MaxGenerations+1); !errors.Is(err, irisdurable.ErrReissueGenerationOutOfRange) {
		t.Fatalf("generation beyond max error = %v; want ErrReissueGenerationOutOfRange", err)
	}
}

func testReissueNestedBase(t *testing.T, ladder irisdurable.ReissueLadder) {
	t.Helper()

	first, err := ladder.ClientRequestID(uniqueID("contract:v1"), 1)
	if err != nil {
		t.Fatalf("generation 1: %v", err)
	}

	if _, err := ladder.Derive(first, 1); err == nil {
		t.Fatalf("deriving from reissued base %q must fail", first)
	}
}

func testReissueGenerationOf(t *testing.T, ladder irisdurable.ReissueLadder) {
	t.Helper()

	base := uniqueID("contract:v1")

	for want := range ladder.MaxGenerations + 1 {
		stored, err := ladder.ClientRequestID(base, want)
		if err != nil {
			t.Fatalf("generation %d: %v", want, err)
		}

		got, ok := ladder.GenerationOf(base, stored)
		if !ok || got != want {
			t.Fatalf("GenerationOf(%q) = %d, %v; want %d, true", stored, got, ok, want)
		}
	}

	if _, ok := ladder.GenerationOf(base, uniqueID("contract:v1")); ok {
		t.Fatal("unrelated id must not resolve to a generation")
	}
}

func testReissueRunSucceedsAfterConflicts(t *testing.T, fixture ReissueFixture) {
	t.Helper()

	ladder := fixture.Ladder
	calls := 0

	result, err := ladder.Run(t.Context(), uniqueID("contract:v1"), 0, func(context.Context, string, int) error {
		calls++
		if calls <= ladder.MaxGenerations {
			return fixture.NewPreHandoffConflict()
		}

		return nil
	}, fixture.PreHandoffConflict)
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	if result.Generation != ladder.MaxGenerations || calls != ladder.MaxGenerations+1 {
		t.Fatalf("result=%+v calls=%d; want success on generation %d", result, calls, ladder.MaxGenerations)
	}
}

func testReissueRunPreservesTerminal(t *testing.T, fixture ReissueFixture) {
	t.Helper()

	terminal := fixture.NewTerminalConflict()
	calls := 0

	_, err := fixture.Ladder.Run(t.Context(), uniqueID("contract:v1"), 0, func(context.Context, string, int) error {
		calls++

		return terminal
	}, fixture.PreHandoffConflict)
	if !errors.Is(err, terminal) || calls != 1 {
		t.Fatalf("err=%v calls=%d; want the terminal conflict returned after one send", err, calls)
	}
}

func testReissueRunExhausts(t *testing.T, fixture ReissueFixture) {
	t.Helper()

	calls := 0

	_, err := fixture.Ladder.Run(t.Context(), uniqueID("contract:v1"), 0, func(context.Context, string, int) error {
		calls++

		return fixture.NewPreHandoffConflict()
	}, fixture.PreHandoffConflict)
	if !errors.Is(err, irisdurable.ErrReissueExhausted) || calls != fixture.Ladder.MaxGenerations+1 {
		t.Fatalf("err=%v calls=%d; want exhaustion after %d sends", err, calls, fixture.Ladder.MaxGenerations+1)
	}
}
