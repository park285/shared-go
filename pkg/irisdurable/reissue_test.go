package irisdurable_test

import (
	"context"
	"errors"
	"regexp"
	"strconv"
	"testing"

	"github.com/park285/shared-go/v2/pkg/irisdurable"
)

var (
	errConflict    = errors.New("pre-handoff conflict")
	errTerminal    = errors.New("outcome unknown")
	suffixPattern  = regexp.MustCompile(`:r\d+$`)
	errNestedBase  = errors.New("nested base")
	testMaxLadders = 2
)

func testDerive(base string, generation int) (string, error) {
	if suffixPattern.MatchString(base) {
		return "", errNestedBase
	}

	return base + ":r" + strconv.Itoa(generation), nil
}

func testLadder() irisdurable.ReissueLadder {
	return irisdurable.ReissueLadder{MaxGenerations: testMaxLadders, Derive: testDerive}
}

func TestReissueLadderClientRequestIDBounds(t *testing.T) {
	t.Parallel()

	ladder := testLadder()

	base, err := ladder.ClientRequestID("base", 0)
	if err != nil || base != "base" {
		t.Fatalf("generation 0 = %q, %v; want base, nil", base, err)
	}

	second, err := ladder.ClientRequestID("base", 2)
	if err != nil || second != "base:r2" {
		t.Fatalf("generation 2 = %q, %v; want base:r2, nil", second, err)
	}

	for _, generation := range []int{-1, 3} {
		if _, err := ladder.ClientRequestID("base", generation); !errors.Is(err, irisdurable.ErrReissueGenerationOutOfRange) {
			t.Fatalf("generation %d error = %v; want out of range", generation, err)
		}
	}

	if _, err := (irisdurable.ReissueLadder{}).ClientRequestID("base", 0); !errors.Is(err, irisdurable.ErrReissueLadderInvalid) {
		t.Fatalf("empty ladder error = %v; want invalid", err)
	}
}

func TestReissueLadderGenerationOf(t *testing.T) {
	t.Parallel()

	ladder := testLadder()

	for want := range testMaxLadders + 1 {
		stored, err := ladder.ClientRequestID("base", want)
		if err != nil {
			t.Fatalf("derive generation %d: %v", want, err)
		}

		got, ok := ladder.GenerationOf("base", stored)
		if !ok || got != want {
			t.Fatalf("GenerationOf(%q) = %d, %v; want %d, true", stored, got, ok, want)
		}
	}

	if _, ok := ladder.GenerationOf("base", "other"); ok {
		t.Fatal("unrelated id must not resolve to a generation")
	}

	if _, ok := ladder.GenerationOf("base:r1", "base:r1:r1"); ok {
		t.Fatal("nested base must not resolve to a generation")
	}
}

func TestReissueLadderRunReissuesOnlyOnPreHandoffConflict(t *testing.T) {
	t.Parallel()

	ladder := testLadder()
	isConflict := func(err error) bool { return errors.Is(err, errConflict) }

	var sent []string

	result, err := ladder.Run(t.Context(), "base", 0, func(_ context.Context, id string, _ int) error {
		sent = append(sent, id)
		if len(sent) < 3 {
			return errConflict
		}

		return nil
	}, isConflict)
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	if result.Generation != 2 || result.ClientRequestID != "base:r2" {
		t.Fatalf("result = %+v; want generation 2", result)
	}

	if len(sent) != 3 {
		t.Fatalf("sent %d times; want 3", len(sent))
	}
}

func TestReissueLadderRunStopsOnTerminalConflict(t *testing.T) {
	t.Parallel()

	ladder := testLadder()
	isConflict := func(err error) bool { return errors.Is(err, errConflict) }
	calls := 0

	result, err := ladder.Run(t.Context(), "base", 0, func(context.Context, string, int) error {
		calls++

		return errTerminal
	}, isConflict)
	if !errors.Is(err, errTerminal) {
		t.Fatalf("error = %v; want terminal conflict passthrough", err)
	}

	if calls != 1 || result.Generation != 0 {
		t.Fatalf("calls=%d result=%+v; want a single generation-0 send", calls, result)
	}
}

func TestReissueLadderRunExhaustsAfterMaxGenerations(t *testing.T) {
	t.Parallel()

	ladder := testLadder()
	isConflict := func(err error) bool { return errors.Is(err, errConflict) }
	calls := 0

	result, err := ladder.Run(t.Context(), "base", 1, func(context.Context, string, int) error {
		calls++

		return errConflict
	}, isConflict)
	if !errors.Is(err, irisdurable.ErrReissueExhausted) || !errors.Is(err, errConflict) {
		t.Fatalf("error = %v; want exhausted wrapping the conflict", err)
	}

	if calls != testMaxLadders || result.Generation != testMaxLadders {
		t.Fatalf("calls=%d result=%+v; want resume from generation 1 through %d", calls, result, testMaxLadders)
	}
}

func TestReissueLadderRunRejectsInvalidStartAndArguments(t *testing.T) {
	t.Parallel()

	ladder := testLadder()
	isConflict := func(error) bool { return false }
	noSend := func(context.Context, string, int) error { return nil }

	if _, err := ladder.Run(t.Context(), "base", testMaxLadders+1, noSend, isConflict); !errors.Is(err, irisdurable.ErrReissueGenerationOutOfRange) {
		t.Fatalf("start beyond max error = %v; want out of range", err)
	}

	if _, err := ladder.Run(t.Context(), "base", 0, nil, isConflict); !errors.Is(err, irisdurable.ErrReissueLadderInvalid) {
		t.Fatalf("nil send error = %v; want invalid", err)
	}
}
