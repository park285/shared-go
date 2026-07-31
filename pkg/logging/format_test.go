package logging

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
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
	for _, leaked := range []string{probeUserID, probeToken, probeQuerySec} {
		if strings.Contains(out, leaked) {
			t.Fatalf("%s: unsanitized value %q reached the writer: %s", label, leaked, out)
		}
	}
	if !strings.Contains(out, redactedValue) {
		t.Fatalf("%s: no redaction marker in output: %s", label, out)
	}
}

func probeJSONRecord(t *testing.T, label, out string) map[string]any {
	t.Helper()
	var probe map[string]any
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
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
	for _, key := range []string{"user_id", "authorization"} {
		if got := record[key]; got != redactedValue {
			t.Fatalf("%s: JSON %s = %v, want %q", label, key, got, redactedValue)
		}
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
	valid := map[string]logFormat{
		"":         formatText,
		"text":     formatText,
		"TEXT":     formatText,
		"  text  ": formatText,
		"json":     formatJSON,
		"JSON":     formatJSON,
		" json ":   formatJSON,
	}
	for in, want := range valid {
		got, err := parseLogFormat(in)
		if err != nil {
			t.Fatalf("parseLogFormat(%q) error = %v, want nil", in, err)
		}
		if got != want {
			t.Fatalf("parseLogFormat(%q) = %v, want %v", in, got, want)
		}
	}

	for _, in := range []string{"yaml", "logfmt", "tex", "json5", "text json"} {
		if _, err := parseLogFormat(in); err == nil {
			t.Errorf("parseLogFormat(%q) error = nil, want rejection", in)
		}
	}
}

func TestNewLogger_SanitizesAndKeepsTextFormat(t *testing.T) {
	out := captureStdout(t, func() { logFormatProbe(NewLogger()) })

	assertProbeSanitized(t, "NewLogger", out)
	if json.Valid([]byte(strings.TrimSpace(out))) {
		t.Fatalf("NewLogger must stay tint text, got JSON-parsable output: %s", out)
	}
	if !strings.Contains(out, "user_id="+redactedValue) {
		t.Fatalf("NewLogger output is not in tint key=value form: %s", out)
	}
}

func TestConsoleHandler_SanitizesInEveryFormat(t *testing.T) {
	for _, format := range []string{FormatText, FormatJSON} {
		t.Run(format, func(t *testing.T) {
			var stdout bytes.Buffer
			logger, closer, err := enableFileLoggingWithStdout(
				&stdout,
				Config{Level: "info", Format: format},
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

			label := "console/" + format
			assertProbeSanitized(t, label, stdout.String())
			if format == FormatJSON {
				assertJSONProbe(t, label, stdout.String())
			}
		})
	}
}

func TestFileHandler_SanitizesOnBothLanesInEveryFormat(t *testing.T) {
	for _, format := range []string{FormatText, FormatJSON} {
		t.Run(format, func(t *testing.T) {
			logDir := t.TempDir()
			var stdout bytes.Buffer
			logger, closer, err := enableFileLoggingWithStdout(
				&stdout,
				Config{
					Level:      "info",
					Format:     format,
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
				label := "file/" + format + "/" + lane
				assertProbeSanitized(t, label, out)
				if format == FormatJSON {
					assertJSONProbe(t, label, out)
				}
			}
		})
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

func stripLeadingTimestamps(out string) string {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	stripped := make([]string, 0, len(lines))
	for _, line := range lines {
		_, rest, found := strings.Cut(line, " ")
		if !found {
			rest = line
		}
		stripped = append(stripped, rest)
	}
	return strings.Join(stripped, "\n")
}

func TestFormatZeroValue_MatchesExplicitText(t *testing.T) {
	zero := formatProbeOutput(t, Config{Level: "info"})
	explicit := formatProbeOutput(t, Config{Level: "info", Format: FormatText})

	if stripLeadingTimestamps(zero) != stripLeadingTimestamps(explicit) {
		t.Fatalf("zero-value Format diverged from %q\nzero: %s\ntext: %s", FormatText, zero, explicit)
	}
	if !strings.Contains(zero, "user_id="+redactedValue) {
		t.Fatalf("zero-value Format is not tint text: %s", zero)
	}
	if json.Valid([]byte(strings.TrimSpace(zero))) {
		t.Fatalf("zero-value Format produced JSON, want tint text: %s", zero)
	}

	jsonOut := formatProbeOutput(t, Config{Level: "info", Format: FormatJSON})
	if stripLeadingTimestamps(jsonOut) == stripLeadingTimestamps(zero) {
		t.Fatalf("json format produced the same output as text: %s", jsonOut)
	}
}

func TestTextFormat_TintOptionsPinned(t *testing.T) {
	const debugProbe = "text_debug_probe_must_be_filtered"

	var stdout bytes.Buffer
	logger, closer, err := enableFileLoggingWithStdout(
		&stdout,
		Config{Level: "info", Format: FormatText},
		"probe.log",
		Options{},
	)
	if err != nil {
		t.Fatalf("enableFileLoggingWithStdout() error = %v", err)
	}
	if closer != nil {
		t.Cleanup(func() { _ = closer.Close() })
	}

	logger.Debug(debugProbe)
	logFormatProbe(logger)

	out := stdout.String()

	if strings.Contains(out, debugProbe) {
		t.Errorf("text handler ignored Config.Level: debug record emitted at info level: %s", out)
	}

	line, _, found := strings.Cut(strings.TrimSpace(out), "\n")
	if !found {
		line = strings.TrimSpace(out)
	}
	stamp, rest, found := strings.Cut(line, " ")
	if !found {
		t.Fatalf("text line has no timestamp field: %q", line)
	}
	if _, err := time.Parse(time.RFC3339, stamp); err != nil {
		t.Errorf("text TimeFormat is not RFC3339: %q (%v)", stamp, err)
	}
	if !tintSourceRef.MatchString(rest) {
		t.Errorf("text handler dropped AddSource: %q does not carry a logging/<file>:<line> reference", rest)
	}
}

var tintSourceRef = regexp.MustCompile(`\blogging/[A-Za-z0-9_.-]+\.go:\d+`)

func TestFormatHandlers_MaskSensitiveKeysRegardlessOfValueKind(t *testing.T) {
	cases := []struct {
		name string
		attr slog.Attr
		leak string
	}{
		{"privacy_int64", slog.Int64("user_id", 4821), "4821"},
		{"privacy_bool", slog.Bool("sender", true), "true"},
		{"credential_int64", slog.Int64("token", 987654321), "987654321"},
		{"credential_int", slog.Int("api_key", 555111), "555111"},
		{"credential_suffix_int", slog.Int64("bot_token", 424242), "424242"},
		{"credential_any_bytes", slog.Any("secret", []byte("RAWSECRETBYTES")), "RAWSECRETBYTES"},
		{"credential_any_slice", slog.Any("authorization", []string{"RAWAUTHVAL"}), "RAWAUTHVAL"},
	}

	for _, format := range []struct {
		label string
		value logFormat
	}{{"text", formatText}, {"json", formatJSON}} {
		for _, tc := range cases {
			t.Run(format.label+"/"+tc.name, func(t *testing.T) {
				var buf bytes.Buffer
				slog.New(newFormatHandler(format.value, slog.LevelInfo, &buf, true)).Info("probe", tc.attr)

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
	slog.New(newFormatHandler(formatJSON, slog.LevelInfo, &buf, true)).Info("format_probe_source")

	record := probeJSONRecord(t, "json/source", buf.String())
	source, ok := record[slog.SourceKey].(map[string]any)
	if !ok {
		t.Fatalf("source is not an object: %v", record[slog.SourceKey])
	}
	file, _ := source["file"].(string)
	if filepath.IsAbs(file) {
		t.Fatalf("source.file is an absolute build path: %q", file)
	}
	if want := "logging/format_test.go"; file != want {
		t.Fatalf("source.file = %q, want %q", file, want)
	}
	if _, ok := source["line"]; !ok {
		t.Fatalf("source.line dropped: %v", source)
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
