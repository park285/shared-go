package dbmigrate

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

type recordedExec struct {
	query string
	args  []any
}

func recordingExec(calls *[]recordedExec, failOn string) Execer {
	return func(_ context.Context, query string, args ...any) error {
		*calls = append(*calls, recordedExec{query: query, args: args})
		if failOn != "" && strings.Contains(query, failOn) {
			return errors.New("boom")
		}
		return nil
	}
}

func TestSessionConfigConfigureAppliesTimeouts(t *testing.T) {
	t.Parallel()

	var calls []recordedExec
	cfg := SessionConfig{
		LockTimeout:      10 * time.Second,
		StatementTimeout: 4 * time.Minute,
	}
	if err := cfg.Configure(context.Background(), recordingExec(&calls, "")); err != nil {
		t.Fatalf("Configure() error = %v", err)
	}

	if len(calls) != 2 {
		t.Fatalf("exec calls = %d, want 2", len(calls))
	}
	if calls[0].query != querySetLockTimeout {
		t.Errorf("first query = %q, want set lock_timeout", calls[0].query)
	}
	if calls[1].query != querySetStatementTimeout {
		t.Errorf("second query = %q, want set statement_timeout", calls[1].query)
	}
	if want := []any{"10000ms"}; !slices.Equal(calls[0].args, want) {
		t.Errorf("lock_timeout args = %v, want %v", calls[0].args, want)
	}
	if want := []any{"240000ms"}; !slices.Equal(calls[1].args, want) {
		t.Errorf("statement_timeout args = %v, want %v", calls[1].args, want)
	}
}

func TestSessionConfigConfigureSkipsNonPositive(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		cfg  SessionConfig
		want int
	}{
		{name: "zero value", cfg: SessionConfig{}, want: 0},
		{name: "negative values", cfg: SessionConfig{LockTimeout: -time.Second, StatementTimeout: -time.Minute}, want: 0},
		{name: "lock only", cfg: SessionConfig{LockTimeout: time.Second}, want: 1},
		{name: "statement only", cfg: SessionConfig{StatementTimeout: time.Minute}, want: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var calls []recordedExec
			if err := tt.cfg.Configure(context.Background(), recordingExec(&calls, "")); err != nil {
				t.Fatalf("Configure() error = %v", err)
			}
			if len(calls) != tt.want {
				t.Fatalf("exec calls = %d, want %d", len(calls), tt.want)
			}
		})
	}
}

func TestSessionConfigConfigureErrors(t *testing.T) {
	t.Parallel()

	cfg := SessionConfig{LockTimeout: time.Second, StatementTimeout: time.Minute}
	if err := cfg.Configure(context.Background(), nil); err == nil || !strings.Contains(err.Error(), "exec is required") {
		t.Fatalf("Configure(nil exec) error = %v, want exec required", err)
	}

	var calls []recordedExec
	err := cfg.Configure(context.Background(), recordingExec(&calls, "lock_timeout"))
	if err == nil || !strings.Contains(err.Error(), "set lock_timeout") {
		t.Fatalf("Configure() error = %v, want wrapped set lock_timeout", err)
	}

	calls = nil
	err = cfg.Configure(context.Background(), recordingExec(&calls, "statement_timeout"))
	if err == nil || !strings.Contains(err.Error(), "set statement_timeout") {
		t.Fatalf("Configure() error = %v, want wrapped set statement_timeout", err)
	}
}

func TestTimeoutSettingRoundsUpToMillisecond(t *testing.T) {
	t.Parallel()

	tests := []struct {
		d    time.Duration
		want string
	}{
		{d: time.Millisecond, want: "1ms"},
		{d: 1500 * time.Microsecond, want: "2ms"},
		{d: 10 * time.Second, want: "10000ms"},
		{d: time.Duration(1<<63 - 1), want: "9223372036855ms"},
	}
	for _, tt := range tests {
		if got := timeoutSetting(tt.d); got != tt.want {
			t.Errorf("timeoutSetting(%v) = %q, want %q", tt.d, got, tt.want)
		}
	}
}

func TestApplyWithSessionConfiguresBeforeMigrations(t *testing.T) {
	t.Parallel()

	fsys := fstest.MapFS{
		ManifestName: {Data: []byte("001 first.sql\n")},
		"first.sql":  {Data: []byte("select 1")},
	}

	var calls []recordedExec
	err := Apply(context.Background(), fsys, recordingExec(&calls, ""), WithSession(SessionConfig{
		LockTimeout:      10 * time.Second,
		StatementTimeout: 4 * time.Minute,
	}))
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	got := make([]string, 0, len(calls))
	for _, call := range calls {
		got = append(got, call.query)
	}
	want := []string{querySetLockTimeout, querySetStatementTimeout, "select 1"}
	if !slices.Equal(got, want) {
		t.Fatalf("executed queries = %v, want %v", got, want)
	}
}

func TestSessionQueriesUseSessionScope(t *testing.T) {
	t.Parallel()

	for _, query := range []string{querySetLockTimeout, querySetStatementTimeout} {
		if !strings.Contains(query, "set_config(") || !strings.Contains(query, "$1, false)") {
			t.Errorf("session query %q must use set_config(..., $1, false)", query)
		}
	}
}
