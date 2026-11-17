package logging

import (
	"bytes"
	"encoding/json/jsontext"
	"errors"
	"log/slog"
	"reflect"
	"strings"
	"testing"
	"time"
)

const (
	boundarySentinel   = "synthetic-private-content-7391"
	boundaryRequestID  = "request-7391"
	boundaryErrorCode  = "provider_rejected"
	boundaryTraceIDKey = "trace_id"
	boundarySenderKey  = "sender"
	boundaryErrorKey   = "error"
)

type boundaryPayload struct {
	Content string
	Next    *boundaryPayload
}

type boundaryNamedMap map[string]any

type boundaryNamedSlice []string

type boundaryNamedString string

type boundaryValuer struct {
	calls      *int
	value      slog.Value
	panicValue any
}

func (v *boundaryValuer) LogValue() slog.Value {
	*v.calls++
	if v.panicValue != nil {
		panic(v.panicValue)
	}

	return v.value
}

type boundaryStringer struct{ calls *int }

func (v boundaryStringer) String() string {
	*v.calls++
	return boundarySentinel
}

type boundaryError struct{ calls *int }

func (v boundaryError) Error() string {
	*v.calls++
	return boundarySentinel
}

type boundaryJSONMarshaler struct {
	calls *int
	mode  string
}

func (v boundaryJSONMarshaler) MarshalJSON() ([]byte, error) {
	*v.calls++
	switch v.mode {
	case "panic":
		panic(boundarySentinel)
	case boundaryErrorKey:
		return nil, errors.New(boundarySentinel)
	default:
		return []byte(`"` + boundarySentinel + `"`), nil
	}
}

type boundaryTextMarshaler struct{ calls *int }

func (v boundaryTextMarshaler) MarshalText() ([]byte, error) {
	*v.calls++
	return []byte(boundarySentinel), nil
}

type boundaryJSONToMarshaler struct{ calls *int }

func (v boundaryJSONToMarshaler) MarshalJSONTo(enc *jsontext.Encoder) error {
	*v.calls++
	return enc.WriteToken(jsontext.String(boundarySentinel))
}

type boundaryDiagnosticMarshalerError struct {
	calls *int
}

func (v boundaryDiagnosticMarshalerError) Error() string {
	return "provider failed token=" + boundarySentinel
}

func (v boundaryDiagnosticMarshalerError) MarshalJSON() ([]byte, error) {
	*v.calls++
	return []byte(`"` + boundarySentinel + `"`), nil
}

func TestSanitizeBoundaryJSONUnsupportedValues(t *testing.T) {
	calls := 0
	pointer := &boundaryPayload{Content: boundarySentinel}

	pointer.Next = pointer

	cycle := map[string]any{"content": boundarySentinel}

	cycle["self"] = cycle

	sliceCycle := []any{nil, boundarySentinel}

	sliceCycle[0] = sliceCycle

	cases := map[string]any{
		"struct":           boundaryPayload{Content: boundarySentinel},
		"pointer_cycle":    pointer,
		"typed_nil":        (*boundaryPayload)(nil),
		"map_cycle":        cycle,
		"slice_cycle":      sliceCycle,
		"named_map":        boundaryNamedMap{"content": boundarySentinel},
		"named_slice":      boundaryNamedSlice{boundarySentinel},
		"named_string":     boundaryNamedString(boundarySentinel),
		"array":            [1]string{boundarySentinel},
		"bytes":            []byte(boundarySentinel),
		"stringer":         boundaryStringer{calls: &calls},
		"json":             boundaryJSONMarshaler{calls: &calls},
		"json_error":       boundaryJSONMarshaler{calls: &calls, mode: boundaryErrorKey},
		"json_panic":       boundaryJSONMarshaler{calls: &calls, mode: "panic"},
		"json_to":          boundaryJSONToMarshaler{calls: &calls},
		"text":             boundaryTextMarshaler{calls: &calls},
		"nested_error":     map[string]any{boundaryErrorKey: boundaryError{calls: &calls}},
		"nested_marshaler": []any{boundaryJSONMarshaler{calls: &calls}},
	}
	for name, value := range cases {
		t.Run(name, func(t *testing.T) {
			var output bytes.Buffer

			logger := slog.New(newFormatHandler(slog.LevelInfo, &output, nil))
			logger.LogAttrs(t.Context(), slog.LevelError, "provider_response_failed",
				slog.String(stdoutTestRequestIDKey, boundaryRequestID),
				slog.String("error_code", boundaryErrorCode),
				slog.Any("response", value))

			record := decodeLogLine(t, output.String())
			if strings.Contains(output.String(), boundarySentinel) || record["response"] != blockedUnsupported {
				t.Fatalf("미지원 값의 출력 차단 실패: %s", output.String())
			}

			if record[stdoutTestRequestIDKey] != boundaryRequestID || record["error_code"] != boundaryErrorCode {
				t.Fatalf("진단 식별자 손실: %v", record)
			}

			if calls != 0 {
				t.Fatalf("미지원 사용자 메서드 호출 수 = %d", calls)
			}
		})
	}

	if pointer.Content != boundarySentinel || pointer.Next != pointer || cycle["content"] != boundarySentinel || sliceCycle[1] != boundarySentinel {
		t.Fatal("호출자 입력 변경")
	}
}

func TestSanitizeBoundaryLogValuerResults(t *testing.T) {
	panicCalls := 0
	cases := []struct {
		name  string
		value slog.Value
		panic any
		want  any
	}{
		{name: "scalar", value: slog.StringValue("visible"), want: "visible"},
		{name: "nil", value: slog.AnyValue(nil), want: nil},
		{name: "any", value: slog.AnyValue(boundaryPayload{Content: boundarySentinel}), want: blockedUnsupported},
		{name: "group", value: slog.GroupValue(slog.String("authorization", boundarySentinel), slog.String(stdoutTestRequestIDKey, boundaryRequestID)), want: map[string]any{"authorization": redactedValue, stdoutTestRequestIDKey: boundaryRequestID}},
		{name: "group_any", value: slog.GroupValue(slog.Any("response", boundaryPayload{Content: boundarySentinel})), want: map[string]any{"response": blockedUnsupported}},
		{name: boundaryErrorKey, value: slog.AnyValue(errors.New("token=" + boundarySentinel)), want: "token=" + redactedValue},
		{name: "panic", panic: boundaryError{calls: &panicCalls}, want: blockedResolution},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			valuer := &boundaryValuer{calls: &calls, value: tc.value, panicValue: tc.panic}

			var output bytes.Buffer

			logger := slog.New(newFormatHandler(slog.LevelInfo, &output, nil))
			logger.LogAttrs(t.Context(), slog.LevelInfo, "resolved_response", slog.Any("response", valuer))

			record := decodeLogLine(t, output.String())
			if !reflect.DeepEqual(record["response"], tc.want) || strings.Contains(output.String(), boundarySentinel) {
				t.Fatalf("해소 결과 = %v, 기대값 %v", record, tc.want)
			}

			if calls != 1 || panicCalls != 0 {
				t.Fatalf("LogValue 호출 = %d, panic Error 호출 = %d", calls, panicCalls)
			}
		})
	}

	t.Run("cycle", func(t *testing.T) {
		calls := 0
		valuer := &boundaryValuer{calls: &calls}

		valuer.value = slog.AnyValue(valuer)

		var output bytes.Buffer

		slog.New(newFormatHandler(slog.LevelInfo, &output, nil)).LogAttrs(t.Context(), slog.LevelInfo, "resolve_cycle", slog.Any("response", valuer))

		if decodeLogLine(t, output.String())["response"] != blockedResolution || calls == 0 || calls > 100 {
			t.Fatalf("해소 순환 차단 실패: 호출=%d 출력=%s", calls, output.String())
		}
	})
}

func TestSanitizeBoundarySensitiveValuesNeverExecute(t *testing.T) {
	for _, key := range []string{"authorization", boundarySenderKey} {
		for _, placement := range []string{"record", "with_attrs", "group", "with_group"} {
			t.Run(key+"/"+placement, func(t *testing.T) {
				calls := 0
				valuer := &boundaryValuer{calls: &calls, panicValue: boundarySentinel}

				var output bytes.Buffer

				logger := slog.New(newFormatHandler(slog.LevelInfo, &output, testPrivacy))
				attrs := []slog.Attr{slog.Any(key, valuer)}

				switch placement {
				case "with_attrs":
					logger = slog.New(logger.Handler().WithAttrs(attrs))
					attrs = nil
				case "group":
					attrs = []slog.Attr{slog.Group(key, slog.Any("content", valuer), slog.Any(boundaryErrorKey, boundaryError{calls: &calls}))}
				case "with_group":
					logger = logger.WithGroup(key).With(slog.Any("bound", valuer))
					attrs = []slog.Attr{slog.Any("content", valuer), slog.Any(boundaryErrorKey, boundaryError{calls: &calls})}
				}

				logger.LogAttrs(t.Context(), slog.LevelInfo, "sensitive_response", attrs...)

				if calls != 0 || strings.Contains(output.String(), boundarySentinel) || !strings.Contains(output.String(), redactedValue) {
					t.Fatalf("민감 값 메서드 실행 또는 원문 노출: 호출=%d 출력=%s", calls, output.String())
				}
			})
		}
	}
}

func boundaryNestedGroup(depth int, leaf slog.Attr) slog.Attr {
	for range depth {
		leaf = slog.Attr{Key: "scope", Value: slog.GroupValue(leaf)}
	}

	return leaf
}

func TestSanitizeBoundaryGroupDepth(t *testing.T) {
	for _, depth := range []int{8, 9} {
		for _, open := range []int{0, 4, 8} {
			for _, bound := range []bool{false, true} {
				var output bytes.Buffer

				logger := slog.New(newFormatHandler(slog.LevelInfo, &output, nil)).With(slog.String(stdoutTestRequestIDKey, boundaryRequestID))

				for range open {
					logger = logger.WithGroup("scope").WithGroup("")
				}

				attrs := []slog.Attr{boundaryNestedGroup(depth-open, slog.String("value", "depth-leaf"))}

				if bound {
					logger = slog.New(logger.Handler().WithAttrs(attrs))
					attrs = nil
				}

				logger.LogAttrs(t.Context(), slog.LevelInfo, "depth_probe", attrs...)

				if strings.Contains(output.String(), "depth-leaf") != (depth == 8) || strings.Contains(output.String(), blockedDepth) != (depth == 9) {
					t.Fatalf("group 깊이=%d WithGroup=%d WithAttrs=%t 경계 실패: %s", depth, open, bound, output.String())
				}

				if decodeLogLine(t, output.String())[stdoutTestRequestIDKey] != boundaryRequestID {
					t.Fatal("깊이 차단이 바깥 식별자를 제거함")
				}
			}
		}
	}
}

func TestSanitizeBoundaryWithAttrsAndGroupIsolation(t *testing.T) {
	var output bytes.Buffer

	base := slog.New(newFormatHandler(slog.LevelInfo, &output, nil))
	calls := 0
	valuer := &boundaryValuer{calls: &calls, value: slog.StringValue("visible")}
	attrs := []slog.Attr{
		slog.Any("payload", boundaryPayload{Content: boundarySentinel}),
		slog.String(stdoutTestRequestIDKey, boundaryRequestID),
		slog.Any("resolved", valuer),
	}
	bound := slog.New(base.Handler().WithAttrs(attrs)).WithGroup("operation")
	bound.LogAttrs(t.Context(), slog.LevelInfo, "bound", slog.String("error_code", boundaryErrorCode))
	bound.LogAttrs(t.Context(), slog.LevelInfo, "bound_again")
	base.LogAttrs(t.Context(), slog.LevelInfo, "base", slog.String("visible", "yes"))

	if attrs[0].Value.Kind() != slog.KindAny || attrs[1].Value.String() != boundaryRequestID || attrs[2].Value.Kind() != slog.KindLogValuer {
		t.Fatal("WithAttrs가 호출자 속성을 변경함")
	}

	if calls != 1 {
		t.Fatalf("WithAttrs 값의 중복 해소: 호출=%d", calls)
	}

	boundRecord := findJSONLogRecord(t, output.String(), "bound", 0)
	baseRecord := findJSONLogRecord(t, output.String(), "base", 0)

	if boundRecord["payload"] != blockedUnsupported || boundRecord[stdoutTestRequestIDKey] != boundaryRequestID || baseRecord["payload"] != nil || baseRecord["operation"] != nil {
		t.Fatalf("파생 로거 격리 실패: bound=%v base=%v", boundRecord, baseRecord)
	}

	operation, ok := boundRecord["operation"].(map[string]any)
	if !ok || operation["error_code"] != boundaryErrorCode {
		t.Fatalf("WithAttrs 이후 WithGroup 범위 손실: %v", boundRecord)
	}

	if boundRecord["resolved"] != "visible" {
		t.Fatal("WithAttrs LogValuer 결과 손실")
	}
}

func TestSanitizeBoundaryOverdepthDoesNotExecute(t *testing.T) {
	calls := 0
	valuer := &boundaryValuer{calls: &calls, panicValue: boundarySentinel}

	var output bytes.Buffer

	logger := slog.New(newFormatHandler(slog.LevelInfo, &output, nil))

	for range 9 {
		logger = logger.WithGroup("scope")
	}

	logger.With(slog.Any("bound", valuer)).WithGroup("ignored").LogAttrs(t.Context(), slog.LevelInfo, "with_group_depth", slog.Any("value", valuer))

	if calls != 0 || !strings.Contains(output.String(), blockedDepth) {
		t.Fatalf("WithGroup 깊이 초과 실행: 호출=%d 출력=%s", calls, output.String())
	}

	output.Reset()
	slog.New(newFormatHandler(slog.LevelInfo, &output, nil)).LogAttrs(t.Context(), slog.LevelInfo, "inline_depth", boundaryNestedGroup(9, slog.Any("value", valuer)))

	if calls != 0 || !strings.Contains(output.String(), blockedDepth) {
		t.Fatalf("inline group 깊이 초과 실행: 호출=%d 출력=%s", calls, output.String())
	}
}

func TestSanitizeBoundaryGroupCycleAndUnnamedDepth(t *testing.T) {
	attrs := []slog.Attr{slog.String("seed", boundarySentinel)}
	cycle := slog.Attr{Key: "scope", Value: slog.GroupValue(attrs...)}

	attrs[0] = cycle

	var output bytes.Buffer

	logger := slog.New(newFormatHandler(slog.LevelInfo, &output, nil))
	logger.LogAttrs(t.Context(), slog.LevelInfo, "cycle", cycle)

	if !strings.Contains(output.String(), blockedDepth) || attrs[0].Value.Kind() != slog.KindGroup {
		t.Fatal("순환 group의 깊이 차단 또는 입력 보존 실패")
	}

	leaf := slog.String("value", boundarySentinel)

	for range 9 {
		leaf = slog.Attr{Value: slog.GroupValue(leaf)}
	}

	output.Reset()
	logger.LogAttrs(t.Context(), slog.LevelInfo, "unnamed", leaf)

	if strings.Contains(output.String(), boundarySentinel) || !strings.Contains(output.String(), blockedDepth) {
		t.Fatalf("이름 없는 group의 깊이 차단 실패: %s", output.String())
	}
}

func TestSanitizeBoundaryNativeScalars(t *testing.T) {
	var output bytes.Buffer

	logger := slog.New(newFormatHandler(slog.LevelInfo, &output, nil))
	logger.LogAttrs(t.Context(), slog.LevelInfo, "scalars", slog.Any("nil", nil), slog.Bool("bool", true),
		slog.Int64("int", -3), slog.Uint64("uint", 4), slog.Float64("float", 1.5),
		slog.Duration("elapsed", time.Second), slog.Time("at", testTime()))

	record := decodeLogLine(t, output.String())
	if record["nil"] != nil || record["bool"] != true || record["int"] != float64(-3) || record["uint"] != float64(4) ||
		record["float"] != 1.5 || record["elapsed"] != float64(time.Second) || record["at"] != testTime().Format(time.RFC3339Nano) {
		t.Fatalf("native scalar 보존 실패: %v", record)
	}
}

func TestSanitizeBoundaryExplicitErrorSkipsMarshaler(t *testing.T) {
	calls := 0

	var output bytes.Buffer

	logger := slog.New(newFormatHandler(slog.LevelInfo, &output, nil))
	logger.LogAttrs(t.Context(), slog.LevelError, "diagnostic",
		slog.Any(boundaryErrorKey, boundaryDiagnosticMarshalerError{calls: &calls}),
		slog.Any("nil_error", (*boundaryError)(nil)))

	record := decodeLogLine(t, output.String())
	if calls != 0 || strings.Contains(output.String(), boundarySentinel) ||
		record[boundaryErrorKey] != "provider failed token="+redactedValue || record["nil_error"] != blockedResolution {
		t.Fatalf("직접 error 정제 또는 marshaler 차단 실패: %s", output.String())
	}
}
