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
		{"on", "TEST_BOOL", "on", false, true},
		{"false", "TEST_BOOL", "false", true, false},
		{"0", "TEST_BOOL", "0", true, false},
		{"no", "TEST_BOOL", "no", true, false},
		{"n", "TEST_BOOL", "n", true, false},
		{"off", "TEST_BOOL", "off", true, false},
		{"OFF uppercase", "TEST_BOOL", "OFF", true, false},
		{"unrecognized returns default true", "TEST_BOOL", "maybe", true, true},
		{"unrecognized returns default false", "TEST_BOOL", "maybe", false, false},
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

func TestList(t *testing.T) {
	tests := []struct {
		name     string
		key      string
		value    string
		set      bool
		expected []string
	}{
		{"mixed delimiters", "TEST_LIST", "a,b c\nd\te", true, []string{"a", "b", "c", "d", "e"}},
		{"trim per item", "TEST_LIST", "  a , b ", true, []string{"a", "b"}},
		{"dedup", "TEST_LIST", "a,a", true, []string{"a"}},
		{"dedup keeps first order", "TEST_LIST", "b,a,b", true, []string{"b", "a"}},
		{"empty value returns nil", "TEST_LIST", "", true, nil},
		{"whitespace only returns nil", "TEST_LIST", "   \n\t", true, nil},
		{"delimiters only returns empty slice", "TEST_LIST", ",,", true, []string{}},
		{"unset returns nil", "TEST_LIST", "", false, nil},
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

func TestListWithFallback(t *testing.T) {
	tests := []struct {
		name     string
		key      string
		value    string
		set      bool
		fallback string
		expected []string
	}{
		{"key set splits its value", "TEST_LIST_FB", "a,b", true, "x,y", []string{"a", "b"}},
		{"key unset splits fallback", "TEST_LIST_FB", "", false, "x,y", []string{"x", "y"}},
		{"key empty splits fallback", "TEST_LIST_FB", "", true, "x,y", []string{"x", "y"}},
		{"both empty returns nil", "TEST_LIST_FB", "", false, "", nil},
		{"fallback deduped", "TEST_LIST_FB", "", false, "x,x", []string{"x"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.set {
				t.Setenv(tt.key, tt.value)
			} else {
				require.NoError(t, os.Unsetenv(tt.key))
			}
			result := ListWithFallback(tt.key, tt.fallback)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestMap(t *testing.T) {
	tests := []struct {
		name     string
		key      string
		value    string
		set      bool
		expected map[string]string
	}{
		{"colon and equals", "TEST_MAP", "k1:v1,k2=v2", true, map[string]string{"k1": "v1", "k2": "v2"}},
		{"value with spaces preserved", "TEST_MAP", "k:v with space", true, map[string]string{"k": "v with space"}},
		{"missing value skipped", "TEST_MAP", "k:,k2:v2", true, map[string]string{"k2": "v2"}},
		{"missing key skipped", "TEST_MAP", "=v,k2:v2", true, map[string]string{"k2": "v2"}},
		{"newline tab delimiters", "TEST_MAP", "k1:v1\nk2:v2\tk3:v3", true, map[string]string{"k1": "v1", "k2": "v2", "k3": "v3"}},
		{"empty value returns nil", "TEST_MAP", "", true, nil},
		{"no valid pairs returns nil", "TEST_MAP", "k:,=v", true, nil},
		{"unset returns nil", "TEST_MAP", "", false, nil},
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
