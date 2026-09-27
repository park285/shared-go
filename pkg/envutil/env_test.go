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

// 비엄격 Int/Bool/Float/Duration은 제거되었고 잘못된 값은 기본값 대신 에러로 드러난다.
func TestIntEValues(t *testing.T) {
	tests := []struct {
		name     string
		key      string
		value    string
		def      int
		expected int
		wantErr  bool
	}{
		{"valid int", testTestInt, "42", 0, 42, false},
		{testTrimApplied, testTestInt, "  42  ", 0, 42, false},
		{"negative int", testTestInt, "-10", 0, -10, false},
		{"invalid returns error", testTestInt, "invalid", 99, 0, true},
		{testEmptyReturnsDefault, testTestInt, "", 99, 99, false},
		{"unset returns default", testUnsetKey, "", 99, 99, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.key == testUnsetKey {
				require.NoError(t, os.Unsetenv(tt.key))
			} else {
				t.Setenv(tt.key, tt.value)
			}

			result, err := IntE(tt.key, tt.def)
			if (err != nil) != tt.wantErr || result != tt.expected {
				t.Errorf("IntE(%q, %d) = (%d, %v), want (%d, error=%t)", tt.key, tt.def, result, err, tt.expected, tt.wantErr)
			}
		})
	}
}

func TestBoolEValues(t *testing.T) {
	tests := []struct {
		name     string
		key      string
		value    string
		def      bool
		expected bool
		wantErr  bool
	}{
		{"true", testTestBool, "true", false, true, false},
		{"True uppercase", testTestBool, "True", false, true, false},
		{"YES uppercase", testTestBool, "YES", false, true, false},
		{testTrimApplied, testTestBool, "  true  ", false, true, false},
		{"OFF uppercase", testTestBool, "OFF", true, false, false},
		{"unrecognized returns error", testTestBool, "maybe", true, false, true},
		{testEmptyReturnsDefault, testTestBool, "", true, true, false},
		{"empty returns default false", testTestBool, "", false, false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(tt.key, tt.value)

			result, err := BoolE(tt.key, tt.def)
			if (err != nil) != tt.wantErr || result != tt.expected {
				t.Errorf("BoolE(%q, %v) = (%v, %v), want (%v, error=%t)", tt.key, tt.def, result, err, tt.expected, tt.wantErr)
			}
		})
	}
}

func TestFloatEValues(t *testing.T) {
	tests := []struct {
		name     string
		key      string
		value    string
		def      float64
		expected float64
		wantErr  bool
	}{
		{"valid float", testTestFloat, "3.14", 0.0, 3.14, false},
		{testTrimApplied, testTestFloat, "  3.14  ", 0.0, 3.14, false},
		{"negative float", testTestFloat, "-2.5", 0.0, -2.5, false},
		{"scientific notation", testTestFloat, "1.5e2", 0.0, 150.0, false},
		{"invalid returns error", testTestFloat, "invalid", 99.9, 0, true},
		{testEmptyReturnsDefault, testTestFloat, "", 99.9, 99.9, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(tt.key, tt.value)

			result, err := FloatE(tt.key, tt.def)
			if (err != nil) != tt.wantErr || result != tt.expected {
				t.Errorf("FloatE(%q, %f) = (%f, %v), want (%f, error=%t)", tt.key, tt.def, result, err, tt.expected, tt.wantErr)
			}
		})
	}
}

func TestDurationEValues(t *testing.T) {
	tests := []struct {
		name     string
		key      string
		value    string
		def      time.Duration
		expected time.Duration
		wantErr  bool
	}{
		{"seconds", testTestDuration, "30s", 0, 30 * time.Second, false},
		{"minutes", testTestDuration, "5m", 0, 5 * time.Minute, false},
		{"hours", testTestDuration, "1h", 0, 1 * time.Hour, false},
		{"combined", testTestDuration, "1h30m", 0, 90 * time.Minute, false},
		{testTrimApplied, testTestDuration, "  30s  ", 0, 30 * time.Second, false},
		{"invalid returns error", testTestDuration, "invalid", 99 * time.Second, 0, true},
		{testEmptyReturnsDefault, testTestDuration, "", 99 * time.Second, 99 * time.Second, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(tt.key, tt.value)

			result, err := DurationE(tt.key, tt.def)
			if (err != nil) != tt.wantErr || result != tt.expected {
				t.Errorf("DurationE(%q, %v) = (%v, %v), want (%v, error=%t)", tt.key, tt.def, result, err, tt.expected, tt.wantErr)
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

			result, err := List(tt.key)
			require.NoError(t, err)
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

	got, err := List("TEST_LIST_FILESRC")
	require.NoError(t, err)
	require.Equal(t, []string{"x", "y", "z"}, got)
}

func TestListFromFileError(t *testing.T) {
	require.NoError(t, os.Unsetenv("TEST_LIST_FILE_ERROR"))
	t.Setenv("TEST_LIST_FILE_ERROR_FILE", filepath.Join(t.TempDir(), "missing"))

	got, err := List("TEST_LIST_FILE_ERROR")
	require.Error(t, err)
	require.Nil(t, got)
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
		{"newline tab delimiters", testTestMap, "k1:v1\nk2:v2\tk3:v3", true, map[string]string{"k1": "v1", "k2": "v2", "k3": "v3"}},
		{"empty value returns nil", testTestMap, "", true, nil},
		{"unset returns nil", testTestMap, "", false, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.set {
				t.Setenv(tt.key, tt.value)
			} else {
				require.NoError(t, os.Unsetenv(tt.key))
			}

			result, err := Map(tt.key)
			require.NoError(t, err)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestMapRejectsMalformedPresentValue(t *testing.T) {
	t.Setenv(testTestMap, "valid:value,missing-separator")

	got, err := Map(testTestMap)
	require.Error(t, err)
	require.Nil(t, got)
	require.NotContains(t, err.Error(), "missing-separator")
}
