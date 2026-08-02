package envutil

import (
	"errors"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestIntE(t *testing.T) {
	t.Setenv("TEST_INT_E", " 42 ")
	got, err := IntE("TEST_INT_E", 7)
	if err != nil || got != 42 {
		t.Fatalf("IntE() = (%d, %v), want (42, nil)", got, err)
	}

	t.Setenv("TEST_INT_E", "invalid")
	got, err = IntE("TEST_INT_E", 7)
	if err == nil || got != 0 {
		t.Fatalf("IntE(invalid) = (%d, %v), want (0, error)", got, err)
	}
	if !strings.Contains(err.Error(), `invalid int env TEST_INT_E (invalid syntax)`) {
		t.Fatalf("IntE(invalid) error = %q", err)
	}
	if !errors.Is(err, strconv.ErrSyntax) {
		t.Fatalf("IntE(invalid) error = %v, want strconv.ErrSyntax", err)
	}

	t.Setenv("TEST_INT_E", " ")
	got, err = IntE("TEST_INT_E", 7)
	if err != nil || got != 7 {
		t.Fatalf("IntE(blank) = (%d, %v), want (7, nil)", got, err)
	}
}

func TestStrictParseErrorsDoNotContainRawValues(t *testing.T) {
	const canary = "sk_test_FAKESecret1234567890"
	tests := []struct {
		name string
		key  string
		call func(string) error
	}{
		{name: "int", key: "TEST_SECRET_INT", call: func(key string) error { _, err := IntE(key, 0); return err }},
		{name: "int64 any", key: "TEST_SECRET_INT64", call: func(key string) error { _, err := Int64AnyE([]string{key}, 0); return err }},
		{name: "float", key: "TEST_SECRET_FLOAT", call: func(key string) error { _, err := FloatE(key, 0); return err }},
		{name: "bool any", key: "TEST_SECRET_BOOL", call: func(key string) error { _, err := BoolAnyE([]string{key}, false); return err }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(tt.key, canary)
			err := tt.call(tt.key)
			if err == nil {
				t.Fatal("strict parser error = nil")
			}
			if strings.Contains(err.Error(), canary) {
				t.Fatalf("strict parser leaked raw value: %q", err)
			}
			if !strings.Contains(err.Error(), tt.key) {
				t.Fatalf("strict parser error = %q, want key", err)
			}
			if !errors.Is(err, strconv.ErrSyntax) {
				t.Fatalf("strict parser error = %v, want syntax classification", err)
			}
		})
	}
}

func TestStrictParseErrorsPreserveRangeClassification(t *testing.T) {
	t.Setenv("TEST_INT_RANGE", "999999999999999999999999999999")
	_, err := IntE("TEST_INT_RANGE", 0)
	if err == nil || !errors.Is(err, strconv.ErrRange) {
		t.Fatalf("IntE(range) error = %v, want strconv.ErrRange", err)
	}
	if strings.Contains(err.Error(), "999999999999999999999999999999") {
		t.Fatalf("IntE(range) leaked raw value: %q", err)
	}
}

func TestInt64E(t *testing.T) {
	t.Setenv("TEST_INT64_E", "9223372036854775807")
	got, err := Int64E("TEST_INT64_E", 7)
	if err != nil || got != 9223372036854775807 {
		t.Fatalf("Int64E() = (%d, %v)", got, err)
	}

	t.Setenv("TEST_INT64_E", "invalid")
	got, err = Int64E("TEST_INT64_E", 7)
	if err == nil || got != 0 {
		t.Fatalf("Int64E(invalid) = (%d, %v), want (0, error)", got, err)
	}
}

func TestFloatE(t *testing.T) {
	t.Setenv("TEST_FLOAT_E", " 3.5 ")
	got, err := FloatE("TEST_FLOAT_E", 1.5)
	if err != nil || got != 3.5 {
		t.Fatalf("FloatE() = (%v, %v), want (3.5, nil)", got, err)
	}

	t.Setenv("TEST_FLOAT_E", "invalid")
	got, err = FloatE("TEST_FLOAT_E", 1.5)
	if err == nil || got != 0 {
		t.Fatalf("FloatE(invalid) = (%v, %v), want (0, error)", got, err)
	}
}

func TestBoolE(t *testing.T) {
	for _, tt := range []struct {
		value string
		want  bool
	}{
		{value: "true", want: true},
		{value: "1", want: true},
		{value: "yes", want: true},
		{value: "y", want: true},
		{value: "false", want: false},
		{value: "0", want: false},
		{value: "no", want: false},
		{value: "n", want: false},
		{value: "on", want: true},
		{value: "off", want: false},
		{value: "ON", want: true},
		{value: " off ", want: false},
	} {
		t.Run(tt.value, func(t *testing.T) {
			t.Setenv("TEST_BOOL_E", tt.value)
			got, err := BoolE("TEST_BOOL_E", !tt.want)
			if err != nil || got != tt.want {
				t.Fatalf("BoolE(%q) = (%v, %v), want (%v, nil)", tt.value, got, err, tt.want)
			}
		})
	}

	t.Setenv("TEST_BOOL_E", "maybe")
	got, err := BoolE("TEST_BOOL_E", true)
	if err == nil || got {
		t.Fatalf("BoolE(maybe) = (%v, %v), want (false, error)", got, err)
	}
}

func TestBoolAcceptanceSetIsShared(t *testing.T) {
	accepted := map[string]bool{
		"1": true, "true": true, "TRUE": true, "yes": true, "y": true, "on": true, "ON": true,
		"0": false, "false": false, "False": false, "no": false, "n": false, "off": false, "OFF": false,
	}

	for value, want := range accepted {
		t.Run("accept_"+value, func(t *testing.T) {
			t.Setenv("TEST_BOOL_TABLE", value)

			if got := Bool("TEST_BOOL_TABLE", !want); got != want {
				t.Errorf("Bool(%q) = %v, want %v", value, got, want)
			}
			got, err := BoolE("TEST_BOOL_TABLE", !want)
			if err != nil || got != want {
				t.Errorf("BoolE(%q) = (%v, %v), want (%v, nil)", value, got, err, want)
			}
			gotAny, err := BoolAnyE([]string{"TEST_BOOL_TABLE"}, !want)
			if err != nil || gotAny != want {
				t.Errorf("BoolAnyE(%q) = (%v, %v), want (%v, nil)", value, gotAny, err, want)
			}
			gotExplicit, explicit, err := BoolExplicit("TEST_BOOL_TABLE")
			if err != nil || !explicit || gotExplicit != want {
				t.Errorf("BoolExplicit(%q) = (%v, %v, %v), want (%v, true, nil)", value, gotExplicit, explicit, err, want)
			}
			if got := dotenvBool("TEST_BOOL_TABLE", !want); got != want {
				t.Errorf("dotenvBool(%q) = %v, want %v", value, got, want)
			}
		})
	}

	for _, value := range []string{"maybe", "2", "t", "f", "enabled"} {
		t.Run("reject_"+value, func(t *testing.T) {
			t.Setenv("TEST_BOOL_TABLE", value)

			if got := Bool("TEST_BOOL_TABLE", true); !got {
				t.Errorf("Bool(%q) = %v, want default true", value, got)
			}
			if _, err := BoolE("TEST_BOOL_TABLE", true); err == nil {
				t.Errorf("BoolE(%q) error = nil, want error", value)
			}
			if _, err := BoolAnyE([]string{"TEST_BOOL_TABLE"}, true); err == nil {
				t.Errorf("BoolAnyE(%q) error = nil, want error", value)
			}
			if _, _, err := BoolExplicit("TEST_BOOL_TABLE"); err == nil {
				t.Errorf("BoolExplicit(%q) error = nil, want error", value)
			}
			if got := dotenvBool("TEST_BOOL_TABLE", true); !got {
				t.Errorf("dotenvBool(%q) = %v, want default true", value, got)
			}
		})
	}
}

func TestBoolExplicit(t *testing.T) {
	t.Run("unset", func(t *testing.T) {
		if err := os.Unsetenv("TEST_BOOL_EXPLICIT"); err != nil {
			t.Fatalf("Unsetenv() error = %v", err)
		}
		value, explicit, err := BoolExplicit("TEST_BOOL_EXPLICIT")
		if value || explicit || err != nil {
			t.Fatalf("BoolExplicit(unset) = (%v, %v, %v), want (false, false, nil)", value, explicit, err)
		}
	})

	t.Run("blank counts as unset", func(t *testing.T) {
		t.Setenv("TEST_BOOL_EXPLICIT", "   ")
		value, explicit, err := BoolExplicit("TEST_BOOL_EXPLICIT")
		if value || explicit || err != nil {
			t.Fatalf("BoolExplicit(blank) = (%v, %v, %v), want (false, false, nil)", value, explicit, err)
		}
	})

	t.Run("explicit false", func(t *testing.T) {
		t.Setenv("TEST_BOOL_EXPLICIT", "false")
		value, explicit, err := BoolExplicit("TEST_BOOL_EXPLICIT")
		if value || !explicit || err != nil {
			t.Fatalf("BoolExplicit(false) = (%v, %v, %v), want (false, true, nil)", value, explicit, err)
		}
	})

	t.Run("invalid does not leak value", func(t *testing.T) {
		const canary = "sk_test_FAKESecret1234567890"
		t.Setenv("TEST_BOOL_EXPLICIT", canary)
		_, explicit, err := BoolExplicit("TEST_BOOL_EXPLICIT")
		if err == nil {
			t.Fatal("BoolExplicit(invalid) error = nil, want error")
		}
		if !explicit {
			t.Error("BoolExplicit(invalid) explicit = false, want true")
		}
		if strings.Contains(err.Error(), canary) {
			t.Fatalf("BoolExplicit(invalid) leaked raw value: %q", err)
		}
		if !errors.Is(err, strconv.ErrSyntax) {
			t.Fatalf("BoolExplicit(invalid) error = %v, want strconv.ErrSyntax", err)
		}
	})
}

func TestDurationE(t *testing.T) {
	t.Setenv("TEST_DURATION_E", " 1h30m ")
	got, err := DurationE("TEST_DURATION_E", time.Second)
	if err != nil || got != 90*time.Minute {
		t.Fatalf("DurationE() = (%v, %v), want (90m, nil)", got, err)
	}

	t.Setenv("TEST_DURATION_E", " ")
	got, err = DurationE("TEST_DURATION_E", 5*time.Second)
	if err != nil || got != 5*time.Second {
		t.Fatalf("DurationE(blank) = (%v, %v), want (5s, nil)", got, err)
	}

	const canary = "sk_test_FAKESecret1234567890"
	t.Setenv("TEST_DURATION_E", canary)
	got, err = DurationE("TEST_DURATION_E", 5*time.Second)
	if err == nil || got != 0 {
		t.Fatalf("DurationE(invalid) = (%v, %v), want (0, error)", got, err)
	}
	if strings.Contains(err.Error(), canary) {
		t.Fatalf("DurationE(invalid) leaked raw value: %q", err)
	}
	if !strings.Contains(err.Error(), "TEST_DURATION_E") {
		t.Fatalf("DurationE(invalid) error = %q, want key", err)
	}
	if !errors.Is(err, strconv.ErrSyntax) {
		t.Fatalf("DurationE(invalid) error = %v, want strconv.ErrSyntax", err)
	}
}

func TestAnyEUsesFirstNonEmptyValue(t *testing.T) {
	t.Setenv("TEST_ANY_FIRST", " ")
	t.Setenv("TEST_ANY_SECOND", "23")

	gotInt, err := IntAnyE([]string{"TEST_ANY_FIRST", "TEST_ANY_SECOND"}, 7)
	if err != nil || gotInt != 23 {
		t.Fatalf("IntAnyE() = (%d, %v), want (23, nil)", gotInt, err)
	}

	gotInt64, err := Int64AnyE([]string{"TEST_ANY_FIRST", "TEST_ANY_SECOND"}, 7)
	if err != nil || gotInt64 != 23 {
		t.Fatalf("Int64AnyE() = (%d, %v), want (23, nil)", gotInt64, err)
	}

	t.Setenv("TEST_ANY_SECOND", "yes")
	gotBool, err := BoolAnyE([]string{"TEST_ANY_FIRST", "TEST_ANY_SECOND"}, false)
	if err != nil || !gotBool {
		t.Fatalf("BoolAnyE() = (%v, %v), want (true, nil)", gotBool, err)
	}
}

func TestAnyEStopsAtFirstInvalidValue(t *testing.T) {
	t.Setenv("TEST_ANY_INVALID", "invalid")
	t.Setenv("TEST_ANY_VALID", "42")

	got, err := IntAnyE([]string{"TEST_ANY_INVALID", "TEST_ANY_VALID"}, 7)
	if err == nil || got != 0 {
		t.Fatalf("IntAnyE() = (%d, %v), want (0, error)", got, err)
	}
	if !strings.Contains(err.Error(), "TEST_ANY_INVALID") {
		t.Fatalf("IntAnyE() error = %q, want first key", err)
	}
}

func TestAnyEReturnsDefaultWhenAllValuesAreEmpty(t *testing.T) {
	t.Setenv("TEST_ANY_EMPTY", " ")

	gotInt, err := IntAnyE([]string{"TEST_ANY_UNSET", "TEST_ANY_EMPTY"}, 7)
	if err != nil || gotInt != 7 {
		t.Fatalf("IntAnyE() = (%d, %v), want (7, nil)", gotInt, err)
	}

	gotInt64, err := Int64AnyE(nil, 8)
	if err != nil || gotInt64 != 8 {
		t.Fatalf("Int64AnyE() = (%d, %v), want (8, nil)", gotInt64, err)
	}

	gotBool, err := BoolAnyE(nil, true)
	if err != nil || !gotBool {
		t.Fatalf("BoolAnyE() = (%v, %v), want (true, nil)", gotBool, err)
	}
}
