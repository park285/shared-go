package envutil

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func captureWarn(t *testing.T, fn func()) []map[string]any {
	t.Helper()

	var buf bytes.Buffer
	old := slog.Default()
	t.Cleanup(func() { slog.SetDefault(old) })
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))

	fn()

	var records []map[string]any
	for line := range strings.SplitSeq(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		rec := map[string]any{}
		require.NoError(t, json.Unmarshal([]byte(line), &rec))
		records = append(records, rec)
	}
	return records
}

func TestInt_ParseFailureWarns(t *testing.T) {
	t.Setenv("TEST_INT_WARN", "30sx")
	var got int
	records := captureWarn(t, func() { got = Int("TEST_INT_WARN", 7) })

	require.Equal(t, 7, got)
	require.Len(t, records, 1)
	require.Equal(t, "WARN", records[0]["level"])
	require.Equal(t, "TEST_INT_WARN", records[0]["key"])
	require.Equal(t, true, records[0]["value_present"])
	require.NotContains(t, records[0], "value")
	require.NotContains(t, records[0]["error"], "30sx")
}

func TestInt_ValidDoesNotWarn(t *testing.T) {
	t.Setenv("TEST_INT_WARN", "42")
	records := captureWarn(t, func() { Int("TEST_INT_WARN", 7) })
	require.Empty(t, records)
}

func TestFloat_ParseFailureWarns(t *testing.T) {
	t.Setenv("TEST_FLOAT_WARN", "3.1x")
	var got float64
	records := captureWarn(t, func() { got = Float("TEST_FLOAT_WARN", 2.5) })
	require.Equal(t, 2.5, got)
	require.Len(t, records, 1)
	require.Equal(t, "TEST_FLOAT_WARN", records[0]["key"])
	require.Equal(t, true, records[0]["value_present"])
	require.NotContains(t, records[0], "value")
	require.NotContains(t, records[0]["error"], "3.1x")
}

func TestDuration_ParseFailureWarns(t *testing.T) {
	t.Setenv("TEST_DURATION_WARN", "30sx")
	var got time.Duration
	records := captureWarn(t, func() { got = Duration("TEST_DURATION_WARN", 9*time.Second) })
	require.Equal(t, 9*time.Second, got)
	require.Len(t, records, 1)
	require.Equal(t, "TEST_DURATION_WARN", records[0]["key"])
	require.Equal(t, true, records[0]["value_present"])
	require.NotContains(t, records[0], "value")
	require.NotContains(t, records[0]["error"], "30sx")
}

func TestBool_UnrecognizedWarns(t *testing.T) {
	t.Setenv("TEST_BOOL_WARN", "maybe")
	var got bool
	records := captureWarn(t, func() { got = Bool("TEST_BOOL_WARN", true) })
	require.True(t, got)
	require.Len(t, records, 1)
	require.Equal(t, "TEST_BOOL_WARN", records[0]["key"])
	require.Equal(t, true, records[0]["value_present"])
	require.NotContains(t, records[0], "value")
}

func TestBool_RecognizedDoesNotWarn(t *testing.T) {
	t.Setenv("TEST_BOOL_WARN", "yes")
	records := captureWarn(t, func() { Bool("TEST_BOOL_WARN", false) })
	require.Empty(t, records)
}
