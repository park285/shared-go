package logging

import (
	"bytes"
	"context"
	jsonv2 "encoding/json/v2"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

const (
	probeUserID   = "kakao-user-4821"
	probeToken    = "ghp_FAKEnotarealgithubtoken000"
	probeQuerySec = "leakedqueryvalue"
	probeMessage  = "format_probe connecting https://x.test?api_key=" + probeQuerySec
)

func logFormatProbe(logger *slog.Logger) {
	logger.Info(probeMessage,
		slog.String("user_id", probeUserID),
		slog.String("authorization", "Bearer "+probeToken),
	)
}

func assertProbeSanitized(t *testing.T, label, out string) {
	t.Helper()
	for _, leaked := range []string{probeToken, probeQuerySec} {
		if strings.Contains(out, leaked) {
			t.Fatalf("%s: unsanitized value %q reached the writer: %s", label, leaked, out)
		}
	}
	if !strings.Contains(out, probeUserID) {
		t.Fatalf("%s: operational user_id was not preserved: %s", label, out)
	}
	if !strings.Contains(out, redactedValue) {
		t.Fatalf("%s: no redaction marker in output: %s", label, out)
	}
}

func probeJSONRecord(t *testing.T, label, out string) map[string]any {
	t.Helper()
	var probe map[string]any
	for line := range strings.SplitSeq(strings.TrimSpace(out), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var record map[string]any
		if err := jsonv2.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("%s: line is not valid JSON: %q (%v)", label, line, err)
		}
		if msg, ok := record[slog.MessageKey].(string); ok && strings.HasPrefix(msg, "format_probe") {
			probe = record
		}
	}
	if probe == nil {
		t.Fatalf("%s: probe record missing from output: %s", label, out)
	}
	return probe
}

func assertJSONProbe(t *testing.T, label, out string) {
	t.Helper()
	record := probeJSONRecord(t, label, out)
	for _, key := range []string{slog.TimeKey, slog.LevelKey, slog.MessageKey, slog.SourceKey} {
		if _, ok := record[key]; !ok {
			t.Fatalf("%s: JSON record missing slog key %q: %v", label, key, record)
		}
	}
	if got := record["user_id"]; got != probeUserID {
		t.Fatalf("%s: JSON user_id = %v, want %q", label, got, probeUserID)
	}
	if got := record["authorization"]; got != redactedValue {
		t.Fatalf("%s: JSON authorization = %v, want %q", label, got, redactedValue)
	}
	if msg, _ := record[slog.MessageKey].(string); strings.Contains(msg, probeQuerySec) {
		t.Fatalf("%s: JSON msg kept query secret: %q", label, msg)
	}
}

// 프로세스 전역 os.Stdout을 갈아끼우므로 이 helper를 쓰는 테스트는 t.Parallel을 쓸 수 없다
// (sanitize_sg05_test.go의 os.Stdout 교체와도 서로 배타적이다). 파이프는 fn 실행과 동시에
// 비워야 한다 — 출력이 파이프 버퍼(리눅스 64 KiB)를 넘으면 fn이 쓰기에서 교착한다.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stdout = w

	// fn 안의 t.Fatalf는 runtime.Goexit이므로 복구·close는 defer로만 보장된다.
	defer func() {
		if err := r.Close(); err != nil {
			t.Errorf("close pipe reader: %v", err)
		}
	}()

	drained := make(chan []byte, 1)
	go func() {
		out, _ := io.ReadAll(r)
		drained <- out
	}()

	func() {
		defer func() {
			os.Stdout = orig
			_ = w.Close()
		}()
		fn()
	}()

	return string(<-drained)
}

func TestParseLogFormat(t *testing.T) {
	for _, in := range []string{"", "json"} {
		if err := parseLogFormat(in); err != nil {
			t.Fatalf("parseLogFormat(%q) error = %v, want nil", in, err)
		}
	}

	for _, in := range []string{"JSON", " json ", "text", "TEXT", "  text  ", "yaml", "logfmt", "tex", "json5", "text json"} {
		if err := parseLogFormat(in); err == nil {
			t.Errorf("parseLogFormat(%q) error = nil, want rejection", in)
		} else if !strings.Contains(err.Error(), `only "json" is supported`) {
			t.Errorf("parseLogFormat(%q) error = %q, want json-only contract", in, err)
		}
	}
}

func TestNewLogger_SanitizesAndKeepsJSONFormat(t *testing.T) {
	out := captureStdout(t, func() { logFormatProbe(NewLogger()) })

	assertProbeSanitized(t, "NewLogger", out)
	assertJSONProbe(t, "NewLogger", out)
}

func TestConsoleHandler_SanitizesJSON(t *testing.T) {
	var stdout bytes.Buffer
	logger, closer, err := enableFileLoggingWithStdout(
		&stdout,
		Config{Level: "info", Format: FormatJSON},
		"console.log",
		Options{},
	)
	if err != nil {
		t.Fatalf("enableFileLoggingWithStdout() error = %v", err)
	}
	if closer != nil {
		t.Fatalf("console-only config returned a closer, want nil")
	}

	logFormatProbe(logger)

	assertProbeSanitized(t, "console/json", stdout.String())
	assertJSONProbe(t, "console/json", stdout.String())
}

func TestFileHandler_SanitizesJSONOnBothLanes(t *testing.T) {
	logDir := t.TempDir()
	var stdout bytes.Buffer
	logger, closer, err := enableFileLoggingWithStdout(
		&stdout,
		Config{
			Level:      "info",
			Format:     FormatJSON,
			Dir:        logDir,
			MaxSizeMB:  10,
			MaxBackups: 5,
			MaxAgeDays: 7,
		},
		"service.log",
		Options{},
	)
	if err != nil {
		t.Fatalf("enableFileLoggingWithStdout() error = %v", err)
	}
	t.Cleanup(func() {
		if closer != nil {
			_ = closer.Close()
		}
	})

	logFormatProbe(logger)

	fileBytes, err := os.ReadFile(filepath.Join(logDir, "service.log"))
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}

	lanes := map[string]string{"stdout": stdout.String(), "file": string(fileBytes)}
	for lane, out := range lanes {
		label := "file/json/" + lane
		assertProbeSanitized(t, label, out)
		assertJSONProbe(t, label, out)
	}
}

func formatProbeOutput(t *testing.T, config Config) string {
	t.Helper()
	var stdout bytes.Buffer
	logger, closer, err := enableFileLoggingWithStdout(&stdout, config, "probe.log", Options{})
	if err != nil {
		t.Fatalf("enableFileLoggingWithStdout() error = %v", err)
	}
	if closer != nil {
		t.Cleanup(func() { _ = closer.Close() })
	}
	logFormatProbe(logger)
	return stdout.String()
}

func TestFormatZeroValue_MatchesExplicitJSON(t *testing.T) {
	zero := formatProbeOutput(t, Config{Level: "info"})
	explicit := formatProbeOutput(t, Config{Level: "info", Format: FormatJSON})

	zeroRecord := probeJSONRecord(t, "zero-value Format", zero)
	explicitRecord := probeJSONRecord(t, "explicit json Format", explicit)
	delete(zeroRecord, slog.TimeKey)
	delete(explicitRecord, slog.TimeKey)
	if !reflect.DeepEqual(zeroRecord, explicitRecord) {
		t.Fatalf("zero-value Format diverged from %q\nzero: %s\njson: %s", FormatJSON, zero, explicit)
	}
}

func TestEnableFileLogging_RejectsTextFormat(t *testing.T) {
	config := Config{Level: "info", Format: "text"}
	logger, closer, err := EnableFileLoggingWithOptions(config, "service.log", Options{})
	if err == nil {
		t.Fatal("EnableFileLoggingWithOptions() error = nil, want text rejection")
	}
	if logger != nil || closer != nil {
		t.Fatalf("EnableFileLoggingWithOptions() = (%v, %v), want (nil, nil) on text rejection", logger, closer)
	}
	if !strings.Contains(err.Error(), `only "json" is supported`) {
		t.Fatalf("error = %q, want json-only contract", err)
	}
}

func TestFormatHandlers_MaskSensitiveKeysRegardlessOfValueKind(t *testing.T) {
	cases := []struct {
		name string
		attr slog.Attr
		leak string
	}{
		{"privacy_int64", slog.Int64("room_name", 4821), "4821"},
		{"privacy_bool", slog.Bool("sender", true), "true"},
		{"credential_int64", slog.Int64("token", 987654321), "987654321"},
		{"credential_int", slog.Int("api_key", 555111), "555111"},
		{"credential_suffix_int", slog.Int64("bot_token", 424242), "424242"},
		{"credential_any_bytes", slog.Any("secret", []byte("RAWSECRETBYTES")), "RAWSECRETBYTES"},
		{"credential_any_slice", slog.Any("authorization", []string{"RAWAUTHVAL"}), "RAWAUTHVAL"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			slog.New(newFormatHandler(slog.LevelInfo, &buf)).Info("probe", tc.attr)

			out := buf.String()
			if strings.Contains(out, tc.leak) {
				t.Fatalf("%s value survived masking: %s", tc.name, strings.TrimSpace(out))
			}
			if !strings.Contains(out, redactedValue) {
				t.Fatalf("%s produced no redaction marker: %s", tc.name, strings.TrimSpace(out))
			}
		})
	}
}

// json은 프로덕션 출하 lane이다. Level 배선이 풀리면 수집기에 DEBUG가 그대로 쏟아진다.
// console 분기와 file 분기는 서로 다른 handler 생성 지점이라, Dir이 빈 구성만 밟으면
// 프로덕션이 실제로 쓰는 file 분기의 배선은 어떤 단언도 지나지 않는다.
func TestJSONFormat_LevelPinned(t *testing.T) {
	const debugProbe = "json_debug_probe_must_be_filtered"

	fileLaneDir := t.TempDir()
	lanes := map[string]Config{
		"console": {Level: "info", Format: FormatJSON},
		"file": {
			Level:      "info",
			Format:     FormatJSON,
			Dir:        fileLaneDir,
			MaxSizeMB:  10,
			MaxBackups: 5,
			MaxAgeDays: 7,
		},
	}

	for lane, config := range lanes {
		t.Run(lane, func(t *testing.T) {
			var stdout bytes.Buffer
			logger, closer, err := enableFileLoggingWithStdout(&stdout, config, "probe.log", Options{})
			if err != nil {
				t.Fatalf("enableFileLoggingWithStdout() error = %v", err)
			}
			if closer != nil {
				t.Cleanup(func() { _ = closer.Close() })
			}

			logger.Debug(debugProbe)
			logFormatProbe(logger)

			assertJSONLevelPinned(t, "json/"+lane+"/stdout", stdout.String(), debugProbe)
		})
	}

	// 이 읽기는 루프 밖에 있어야 한다. 안으로 옮기면 lanes에서 file lane이 사라진 순간
	// 조용히 건너뛰어져, 프로덕션 file 분기의 level 배선이 다시 무단언 상태가 된다.
	fileBytes, err := os.ReadFile(filepath.Join(fileLaneDir, "probe.log"))
	if err != nil {
		t.Fatalf("file lane wrote no log file under %s: %v", fileLaneDir, err)
	}
	assertJSONLevelPinned(t, "json/file/file", string(fileBytes), debugProbe)
}

func assertJSONLevelPinned(t *testing.T, label, out, debugProbe string) {
	t.Helper()

	if strings.Contains(out, debugProbe) {
		t.Errorf("%s: handler ignored Config.Level, debug record emitted at info level: %s", label, out)
	}
	if record := probeJSONRecord(t, label, out); record[slog.LevelKey] != "INFO" {
		t.Errorf("%s: level = %v, want %q", label, record[slog.LevelKey], "INFO")
	}
}

func TestNewLogger_LevelPinned(t *testing.T) {
	const (
		debugProbe = "new_logger_debug_probe_must_be_filtered"
		infoProbe  = "new_logger_info_probe_must_survive"
	)

	out := captureStdout(t, func() {
		logger := NewLogger()
		logger.Debug(debugProbe)
		logger.Info(infoProbe)
	})

	if strings.Contains(out, debugProbe) {
		t.Errorf("NewLogger emitted a debug record at its pinned info level: %s", out)
	}
	if !strings.Contains(out, infoProbe) {
		t.Fatalf("NewLogger dropped the info record: %s", out)
	}
}

// JSON source.file이 빌드 머신 절대 경로면 디렉터리 구조가 모든 record에 실린다.
func TestJSONFormat_ShortensSourcePath(t *testing.T) {
	var buf bytes.Buffer
	slog.New(newFormatHandler(slog.LevelInfo, &buf)).Info("format_probe_source")

	record := probeJSONRecord(t, "json/source", buf.String())
	source, ok := record[slog.SourceKey].(string)
	if !ok {
		t.Fatalf("source is not a string: %v", record[slog.SourceKey])
	}
	file, line, found := strings.Cut(source, ":")
	if !found {
		t.Fatalf("source = %q, want \"file:line\"", source)
	}
	if filepath.IsAbs(file) {
		t.Fatalf("source file is an absolute build path: %q", file)
	}
	if want := "logging/format_test.go"; file != want {
		t.Fatalf("source file = %q, want %q", file, want)
	}
	if line == "" || line == "0" {
		t.Fatalf("source line dropped: %q", source)
	}
}

func TestJSONFormat_OmitsSourceForZeroPC(t *testing.T) {
	var buf bytes.Buffer
	handler := newFormatHandler(slog.LevelInfo, &buf)
	if err := handler.Handle(context.Background(), slog.NewRecord(time.Now(), slog.LevelInfo, "format_probe_zero_pc", 0)); err != nil {
		t.Fatalf("handle zero-PC record: %v", err)
	}

	record := probeJSONRecord(t, "json/zero-pc", buf.String())
	if value, ok := record[slog.SourceKey]; ok {
		t.Fatalf("PC 0 record carries a source attr: %v", value)
	}
}

func TestEnableFileLogging_RejectsUnknownFormat(t *testing.T) {
	configs := map[string]Config{
		"console": {Level: "info", Format: "yaml"},
		"file": {
			Level:      "info",
			Format:     "yaml",
			Dir:        t.TempDir(),
			MaxSizeMB:  10,
			MaxBackups: 5,
			MaxAgeDays: 7,
		},
	}

	for name, config := range configs {
		t.Run(name, func(t *testing.T) {
			logger, closer, err := EnableFileLoggingWithOptions(config, "service.log", Options{})
			if err == nil {
				t.Fatalf("EnableFileLoggingWithOptions() error = nil, want rejection")
			}
			if logger != nil || closer != nil {
				t.Fatalf("EnableFileLoggingWithOptions() = (%v, %v), want (nil, nil) on rejection", logger, closer)
			}
			if !strings.Contains(err.Error(), `"yaml"`) {
				t.Fatalf("error %q does not name the offending format", err)
			}

			if _, err := EnableFileLogging(config, "service.log"); err == nil {
				t.Fatalf("EnableFileLogging() error = nil, want rejection")
			}
		})
	}
}
