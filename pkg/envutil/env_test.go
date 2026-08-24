package envutil

import (
	"os"
	"path/filepath"
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
		{"value exists", "TEST_STRING", testValue, testDefault, testValue},
		{testTrimApplied, "TEST_STRING", "  value  ", testDefault, testValue},
		{testEmptyReturnsDefault, "TEST_STRING", "", testDefault, testDefault},
		{"unset returns default", testUnsetKey, "", testDefault, testDefault},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.key == testUnsetKey {
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
		{"value exists", "TEST_STRING_RAW", testValue, testDefault, testValue},
		{"no trim applied", "TEST_STRING_RAW", "  value  ", testDefault, "  value  "},
		{testEmptyReturnsDefault, "TEST_STRING_RAW", "", testDefault, testDefault},
		{"unset returns default", testUnsetKey, "", testDefault, testDefault},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.key == testUnsetKey {
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
		{"valid int", testTestInt, "42", 0, 42},
		{testTrimApplied, testTestInt, "  42  ", 0, 42},
		{"negative int", testTestInt, "-10", 0, -10},
		{"invalid returns default", testTestInt, "invalid", 99, 99},
		{testEmptyReturnsDefault, testTestInt, "", 99, 99},
		{"unset returns default", testUnsetKey, "", 99, 99},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.key == testUnsetKey {
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

func TestBool(t *testing.T) {
	tests := []struct {
		name     string
		key      string
		value    string
		def      bool
		expected bool
	}{
		{"true", testTestBool, "true", false, true},
		{"1", testTestBool, "1", false, true},
		{"yes", testTestBool, "yes", false, true},
		{"y", testTestBool, "y", false, true},
		{"True uppercase", testTestBool, "True", false, true},
		{"YES uppercase", testTestBool, "YES", false, true},
		{testTrimApplied, testTestBool, "  true  ", false, true},
		{"on", testTestBool, "on", false, true},
		{"false", testTestBool, "false", true, false},
		{"0", testTestBool, "0", true, false},
		{"no", testTestBool, "no", true, false},
		{"n", testTestBool, "n", true, false},
		{"off", testTestBool, "off", true, false},
		{"OFF uppercase", testTestBool, "OFF", true, false},
		{"unrecognized returns default true", testTestBool, "maybe", true, true},
		{"unrecognized returns default false", testTestBool, "maybe", false, false},
		{testEmptyReturnsDefault, testTestBool, "", true, true},
		{"empty returns default false", testTestBool, "", false, false},
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

func TestFloat(t *testing.T) {
	tests := []struct {
		name     string
		key      string
		value    string
		def      float64
		expected float64
	}{
		{"valid float", testTestFloat, "3.14", 0.0, 3.14},
		{testTrimApplied, testTestFloat, "  3.14  ", 0.0, 3.14},
		{"negative float", testTestFloat, "-2.5", 0.0, -2.5},
		{"scientific notation", testTestFloat, "1.5e2", 0.0, 150.0},
		{"invalid returns default", testTestFloat, "invalid", 99.9, 99.9},
		{testEmptyReturnsDefault, testTestFloat, "", 99.9, 99.9},
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
		{"seconds", testTestDuration, "30s", 0, 30 * time.Second},
		{"minutes", testTestDuration, "5m", 0, 5 * time.Minute},
		{"hours", testTestDuration, "1h", 0, 1 * time.Hour},
		{"combined", testTestDuration, "1h30m", 0, 90 * time.Minute},
		{testTrimApplied, testTestDuration, "  30s  ", 0, 30 * time.Second},
		{"invalid returns default", testTestDuration, "invalid", 99 * time.Second, 99 * time.Second},
		{testEmptyReturnsDefault, testTestDuration, "", 99 * time.Second, 99 * time.Second},
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

func TestStringAny(t *testing.T) {
	tests := []struct {
		name     string
		setup    map[string]string
		keys     []string
		expected string
	}{
		{
			"first key exists",
			map[string]string{testKey1: "value1", testKey2: testValue2},
			[]string{testKey1, testKey2},
			"value1",
		},
		{
			"second key exists",
			map[string]string{testKey2: testValue2},
			[]string{testKey1, testKey2},
			testValue2,
		},
		{
			testTrimApplied,
			map[string]string{testKey1: "  value1  "},
			[]string{testKey1, testKey2},
			"value1",
		},
		{
			"skip empty first",
			map[string]string{testKey1: "", testKey2: testValue2},
			[]string{testKey1, testKey2},
			testValue2,
		},
		{
			"all empty returns empty",
			map[string]string{testKey1: "", testKey2: ""},
			[]string{testKey1, testKey2},
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

func TestList(t *testing.T) {
	tests := []struct {
		name     string
		key      string
		value    string
		set      bool
		expected []string
	}{
		{"mixed delimiters", testTestList, "a,b c\nd\te", true, []string{"a", "b", "c", "d", "e"}},
		{"trim per item", testTestList, "  a , b ", true, []string{"a", "b"}},
		{"dedup", testTestList, "a,a", true, []string{"a"}},
		{"dedup keeps first order", testTestList, "b,a,b", true, []string{"b", "a"}},
		{"empty value returns nil", testTestList, "", true, nil},
		{"whitespace only returns nil", testTestList, "   \n\t", true, nil},
		{"delimiters only returns empty slice", testTestList, ",,", true, []string{}},
		{"unset returns nil", testTestList, "", false, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.set {
				t.Setenv(tt.key, tt.value)
			} else {
				require.NoError(t, os.Unsetenv(tt.key))
			}

			result := List(tt.key)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestListFromFile(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "list.secret")
	require.NoError(t, os.WriteFile(filePath, []byte("x, y\nz"), 0o600))

	require.NoError(t, os.Unsetenv("TEST_LIST_FILESRC"))
	t.Setenv("TEST_LIST_FILESRC_FILE", filePath)

	require.Equal(t, []string{"x", "y", "z"}, List("TEST_LIST_FILESRC"))
}

func TestMap(t *testing.T) {
	tests := []struct {
		name     string
		key      string
		value    string
		set      bool
		expected map[string]string
	}{
		{"colon and equals", testTestMap, "k1:v1,k2=v2", true, map[string]string{"k1": "v1", "k2": "v2"}},
		{"value with spaces preserved", testTestMap, "k:v with space", true, map[string]string{"k": "v with space"}},
		{"missing value skipped", testTestMap, "k:,k2:v2", true, map[string]string{"k2": "v2"}},
		{"missing key skipped", testTestMap, "=v,k2:v2", true, map[string]string{"k2": "v2"}},
		{"newline tab delimiters", testTestMap, "k1:v1\nk2:v2\tk3:v3", true, map[string]string{"k1": "v1", "k2": "v2", "k3": "v3"}},
		{"empty value returns nil", testTestMap, "", true, nil},
		{"no valid pairs returns nil", testTestMap, "k:,=v", true, nil},
		{"unset returns nil", testTestMap, "", false, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.set {
				t.Setenv(tt.key, tt.value)
			} else {
				require.NoError(t, os.Unsetenv(tt.key))
			}

			result := Map(tt.key)
			require.Equal(t, tt.expected, result)
		})
	}
}
