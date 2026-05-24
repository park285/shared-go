package envutil

import (
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestString(t *testing.T) {
	tests := []struct {
		name     string
		key      string
		value    string
		def      string
		expected string
	}{
		{"value exists", "TEST_STRING", "value", "default", "value"},
		{"trim applied", "TEST_STRING", "  value  ", "default", "value"},
		{"empty returns default", "TEST_STRING", "", "default", "default"},
		{"unset returns default", "UNSET_KEY", "", "default", "default"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.key == "UNSET_KEY" {
				require.NoError(t, os.Unsetenv(tt.key))
			} else {
				t.Setenv(tt.key, tt.value)
			}
			result := String(tt.key, tt.def)
			if result != tt.expected {
				t.Errorf("String(%q, %q) = %q, want %q", tt.key, tt.def, result, tt.expected)
			}
		})
	}
}

func TestStringRaw(t *testing.T) {
	tests := []struct {
		name     string
		key      string
		value    string
		def      string
		expected string
	}{
		{"value exists", "TEST_STRING_RAW", "value", "default", "value"},
		{"no trim applied", "TEST_STRING_RAW", "  value  ", "default", "  value  "},
		{"empty returns default", "TEST_STRING_RAW", "", "default", "default"},
		{"unset returns default", "UNSET_KEY", "", "default", "default"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.key == "UNSET_KEY" {
				require.NoError(t, os.Unsetenv(tt.key))
			} else {
				t.Setenv(tt.key, tt.value)
			}
			result := StringRaw(tt.key, tt.def)
			if result != tt.expected {
				t.Errorf("StringRaw(%q, %q) = %q, want %q", tt.key, tt.def, result, tt.expected)
			}
		})
	}
}

func TestInt(t *testing.T) {
	tests := []struct {
		name     string
		key      string
		value    string
		def      int
		expected int
	}{
		{"valid int", "TEST_INT", "42", 0, 42},
		{"trim applied", "TEST_INT", "  42  ", 0, 42},
		{"negative int", "TEST_INT", "-10", 0, -10},
		{"invalid returns default", "TEST_INT", "invalid", 99, 99},
		{"empty returns default", "TEST_INT", "", 99, 99},
		{"unset returns default", "UNSET_KEY", "", 99, 99},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.key == "UNSET_KEY" {
				require.NoError(t, os.Unsetenv(tt.key))
			} else {
				t.Setenv(tt.key, tt.value)
			}
			result := Int(tt.key, tt.def)
			if result != tt.expected {
				t.Errorf("Int(%q, %d) = %d, want %d", tt.key, tt.def, result, tt.expected)
			}
		})
	}
}

func TestIntRaw(t *testing.T) {
	tests := []struct {
		name     string
		key      string
		value    string
		def      int
		expected int
	}{
		{"valid int", "TEST_INT_RAW", "42", 0, 42},
		{"no trim causes failure", "TEST_INT_RAW", "  42  ", 99, 99},
		{"invalid returns default", "TEST_INT_RAW", "invalid", 99, 99},
		{"empty returns default", "TEST_INT_RAW", "", 99, 99},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(tt.key, tt.value)
			result := IntRaw(tt.key, tt.def)
			if result != tt.expected {
				t.Errorf("IntRaw(%q, %d) = %d, want %d", tt.key, tt.def, result, tt.expected)
			}
		})
	}
}

func TestIntNonNegative(t *testing.T) {
	tests := []struct {
		name     string
		key      string
		value    string
		def      int
		expected int
	}{
		{"positive value", "TEST_INT_NONNEG", "42", 10, 42},
		{"zero value", "TEST_INT_NONNEG", "0", 10, 0},
		{"negative returns 0 NOT default", "TEST_INT_NONNEG", "-5", 10, 0},
		{"invalid returns default", "TEST_INT_NONNEG", "invalid", 10, 10},
		{"empty returns default", "TEST_INT_NONNEG", "", 10, 10},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(tt.key, tt.value)
			result := IntNonNegative(tt.key, tt.def)
			if result != tt.expected {
				t.Errorf("IntNonNegative(%q, %d) = %d, want %d", tt.key, tt.def, result, tt.expected)
			}
		})
	}
}

func TestInt64(t *testing.T) {
	tests := []struct {
		name     string
		key      string
		value    string
		def      int64
		expected int64
	}{
		{"valid int64", "TEST_INT64", "12345678901234", 0, 12345678901234},
		{"trim applied", "TEST_INT64", "  12345678901234  ", 0, 12345678901234},
		{"negative int64", "TEST_INT64", "-12345678901234", 0, -12345678901234},
		{"invalid returns default", "TEST_INT64", "invalid", 99, 99},
		{"empty returns default", "TEST_INT64", "", 99, 99},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(tt.key, tt.value)
			result := Int64(tt.key, tt.def)
			if result != tt.expected {
				t.Errorf("Int64(%q, %d) = %d, want %d", tt.key, tt.def, result, tt.expected)
			}
		})
	}
}

func TestBool(t *testing.T) {
	tests := []struct {
		name     string
		key      string
		value    string
		def      bool
		expected bool
	}{
		{"true", "TEST_BOOL", "true", false, true},
		{"1", "TEST_BOOL", "1", false, true},
		{"yes", "TEST_BOOL", "yes", false, true},
		{"y", "TEST_BOOL", "y", false, true},
		{"True uppercase", "TEST_BOOL", "True", false, true},
		{"YES uppercase", "TEST_BOOL", "YES", false, true},
		{"trim applied", "TEST_BOOL", "  true  ", false, true},
		{"false", "TEST_BOOL", "false", true, false},
		{"0", "TEST_BOOL", "0", true, false},
		{"no", "TEST_BOOL", "no", true, false},
		{"unknown returns false NOT default", "TEST_BOOL", "unknown", true, false},
		{"empty returns default", "TEST_BOOL", "", true, true},
		{"empty returns default false", "TEST_BOOL", "", false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(tt.key, tt.value)
			result := Bool(tt.key, tt.def)
			if result != tt.expected {
				t.Errorf("Bool(%q, %v) = %v, want %v", tt.key, tt.def, result, tt.expected)
			}
		})
	}
}

func TestBoolStrict(t *testing.T) {
	// Suppress slog warnings during tests
	oldLogger := slog.Default()
	defer slog.SetDefault(oldLogger)
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelError,
	})))

	tests := []struct {
		name     string
		key      string
		value    string
		def      bool
		expected bool
	}{
		{"true", "TEST_BOOL_STRICT", "true", false, true},
		{"True uppercase", "TEST_BOOL_STRICT", "True", false, true},
		{"false", "TEST_BOOL_STRICT", "false", true, false},
		{"False uppercase", "TEST_BOOL_STRICT", "False", true, false},
		{"trim applied", "TEST_BOOL_STRICT", "  true  ", false, true},
		{"yes returns default with warn", "TEST_BOOL_STRICT", "yes", false, false},
		{"1 returns default with warn", "TEST_BOOL_STRICT", "1", false, false},
		{"unknown returns default with warn", "TEST_BOOL_STRICT", "unknown", true, true},
		{"empty returns default", "TEST_BOOL_STRICT", "", true, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(tt.key, tt.value)
			result := BoolStrict(tt.key, tt.def)
			if result != tt.expected {
				t.Errorf("BoolStrict(%q, %v) = %v, want %v", tt.key, tt.def, result, tt.expected)
			}
		})
	}
}

func TestFloat(t *testing.T) {
	tests := []struct {
		name     string
		key      string
		value    string
		def      float64
		expected float64
	}{
		{"valid float", "TEST_FLOAT", "3.14", 0.0, 3.14},
		{"trim applied", "TEST_FLOAT", "  3.14  ", 0.0, 3.14},
		{"negative float", "TEST_FLOAT", "-2.5", 0.0, -2.5},
		{"scientific notation", "TEST_FLOAT", "1.5e2", 0.0, 150.0},
		{"invalid returns default", "TEST_FLOAT", "invalid", 99.9, 99.9},
		{"empty returns default", "TEST_FLOAT", "", 99.9, 99.9},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(tt.key, tt.value)
			result := Float(tt.key, tt.def)
			if result != tt.expected {
				t.Errorf("Float(%q, %f) = %f, want %f", tt.key, tt.def, result, tt.expected)
			}
		})
	}
}

func TestDuration(t *testing.T) {
	tests := []struct {
		name     string
		key      string
		value    string
		def      time.Duration
		expected time.Duration
	}{
		{"seconds", "TEST_DURATION", "30s", 0, 30 * time.Second},
		{"minutes", "TEST_DURATION", "5m", 0, 5 * time.Minute},
		{"hours", "TEST_DURATION", "1h", 0, 1 * time.Hour},
		{"combined", "TEST_DURATION", "1h30m", 0, 90 * time.Minute},
		{"trim applied", "TEST_DURATION", "  30s  ", 0, 30 * time.Second},
		{"invalid returns default", "TEST_DURATION", "invalid", 99 * time.Second, 99 * time.Second},
		{"empty returns default", "TEST_DURATION", "", 99 * time.Second, 99 * time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(tt.key, tt.value)
			result := Duration(tt.key, tt.def)
			if result != tt.expected {
				t.Errorf("Duration(%q, %v) = %v, want %v", tt.key, tt.def, result, tt.expected)
			}
		})
	}
}

func TestRequired(t *testing.T) {
	tests := []struct {
		name        string
		key         string
		value       string
		shouldPanic bool
	}{
		{"value exists", "TEST_REQUIRED", "value", false},
		{"trim applied", "TEST_REQUIRED", "  value  ", false},
		{"empty panics", "TEST_REQUIRED", "", true},
		{"unset panics", "UNSET_REQUIRED", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.key == "UNSET_REQUIRED" {
				require.NoError(t, os.Unsetenv(tt.key))
			} else {
				t.Setenv(tt.key, tt.value)
			}

			defer func() {
				r := recover()
				if (r != nil) != tt.shouldPanic {
					t.Errorf("Required(%q) panic = %v, want panic = %v", tt.key, r != nil, tt.shouldPanic)
				}
			}()

			result := Required(tt.key)
			if !tt.shouldPanic && result != "value" {
				t.Errorf("Required(%q) = %q, want %q", tt.key, result, "value")
			}
		})
	}
}

func TestStringAny(t *testing.T) {
	tests := []struct {
		name     string
		setup    map[string]string
		keys     []string
		expected string
	}{
		{
			"first key exists",
			map[string]string{"KEY1": "value1", "KEY2": "value2"},
			[]string{"KEY1", "KEY2"},
			"value1",
		},
		{
			"second key exists",
			map[string]string{"KEY2": "value2"},
			[]string{"KEY1", "KEY2"},
			"value2",
		},
		{
			"trim applied",
			map[string]string{"KEY1": "  value1  "},
			[]string{"KEY1", "KEY2"},
			"value1",
		},
		{
			"skip empty first",
			map[string]string{"KEY1": "", "KEY2": "value2"},
			[]string{"KEY1", "KEY2"},
			"value2",
		},
		{
			"all empty returns empty",
			map[string]string{"KEY1": "", "KEY2": ""},
			[]string{"KEY1", "KEY2"},
			"",
		},
		{
			"no keys returns empty",
			map[string]string{},
			[]string{},
			"",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for k, v := range tt.setup {
				t.Setenv(k, v)
			}
			result := StringAny(tt.keys...)
			if result != tt.expected {
				t.Errorf("StringAny(%v) = %q, want %q", tt.keys, result, tt.expected)
			}
		})
	}
}
