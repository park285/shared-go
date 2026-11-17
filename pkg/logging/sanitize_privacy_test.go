package logging

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

const kakaoSentinelID = "sentinel-kakao-identifier-8842"

func privacyOutput(t *testing.T, attrs ...slog.Attr) string {
	t.Helper()

	var buf bytes.Buffer

	logger := slog.New(newPrivacySanitizeHandler(slog.NewTextHandler(&buf, nil)))
	logger.LogAttrs(t.Context(), slog.LevelInfo, "privacy", attrs...)

	return buf.String()
}

func TestSanitizeHandler_PrivacyKeysMaskedAcrossKinds(t *testing.T) {
	keys := []string{
		"room", "room_name", "chat_id",
		tokenUserName, "thread_id", "session_thread_id", "sender",
		"USER_NAME", "session.thread.id",
	}

	for _, key := range keys {
		t.Run(key, func(t *testing.T) {
			kinds := map[string]slog.Attr{
				"string": slog.String(key, kakaoSentinelID),
				"int":    slog.Int64(key, 8842),
				"uint":   slog.Uint64(key, 8842),
				"float":  slog.Float64(key, 8842.5),
				"bool":   slog.Bool(key, true),
				"any":    slog.Any(key, map[string]string{testNested: kakaoSentinelID}),
				"error":  slog.Any(key, errors.New(kakaoSentinelID)),
				"valuer": slog.Any(key, fixedValuer{v: kakaoSentinelID}),
			}

			for kind, attr := range kinds {
				output := privacyOutput(t, attr)
				if strings.Contains(output, kakaoSentinelID) || strings.Contains(output, "8842") {
					t.Errorf("%s value of kind %s leaked: %s", key, kind, output)
				}

				if !strings.Contains(output, "***REDACTED***") {
					t.Errorf("%s of kind %s not masked: %s", key, kind, output)
				}
			}
		})
	}
}

func TestSanitizeHandler_OperationalIDsPreservedInsideGroups(t *testing.T) {
	output := privacyOutput(t,
		slog.Group("context",
			slog.String(testUserID, kakaoSentinelID),
			slog.Group("chat",
				slog.Int64("room_id", 8842),
				slog.String("channel_id", "UC1234567890"),
			),
		),
	)

	if !strings.Contains(output, "context.user_id="+kakaoSentinelID) {
		t.Fatalf("group user_id not preserved: %s", output)
	}

	if !strings.Contains(output, "context.chat.room_id=8842") {
		t.Fatalf("nested group room_id not preserved: %s", output)
	}

	if !strings.Contains(output, "context.chat.channel_id=UC1234567890") {
		t.Fatalf("public content id inside group not preserved: %s", output)
	}
}

func TestSanitizeHandler_PrivacyGroupValueMaskedWhole(t *testing.T) {
	output := privacyOutput(t,
		slog.Group("sender",
			slog.String("name", kakaoSentinelID),
			slog.Int64("id", 8842),
		),
	)

	if strings.Contains(output, kakaoSentinelID) || strings.Contains(output, "8842") {
		t.Fatalf("privacy-keyed group leaked members: %s", output)
	}

	if !strings.Contains(output, "sender=***REDACTED***") {
		t.Fatalf("privacy-keyed group not masked: %s", output)
	}
}

func TestSanitizeHandler_PublicContentIDsPreserved(t *testing.T) {
	cases := map[string]string{
		"room_id":      "8842",
		testUserID:     kakaoSentinelID,
		"Room_ID":      "8843",
		"user-id":      "user-8843",
		"channel_id":   "UC1234567890",
		testVideoID:    "dQw4w9WgXcQ",
		"content_id":   "content-42",
		"request_id":   "req-42",
		"message_type": "text",
		"username":     "alice",
	}

	for key, value := range cases {
		t.Run(key, func(t *testing.T) {
			output := privacyOutput(t, slog.String(key, value))
			if strings.Contains(output, "***REDACTED***") {
				t.Fatalf("public key %q was masked: %s", key, output)
			}

			if !strings.Contains(output, key+"="+value) {
				t.Fatalf("public key %q value not preserved: %s", key, output)
			}
		})
	}
}

func TestSanitizeHandler_OperationalIDsPreservedViaWithAttrsAndWithGroup(t *testing.T) {
	var buf bytes.Buffer

	logger := slog.New(newPrivacySanitizeHandler(slog.NewTextHandler(&buf, nil))).
		With(slog.String(testUserID, kakaoSentinelID)).
		WithGroup("request")
	logger.LogAttrs(t.Context(), slog.LevelInfo, "privacy", slog.Int64("room_id", 8842), slog.String(testVideoID, testVid1))

	output := buf.String()

	if !strings.Contains(output, "user_id="+kakaoSentinelID) {
		t.Fatalf("With user_id not preserved: %s", output)
	}

	if !strings.Contains(output, "request.room_id=8842") {
		t.Fatalf("WithGroup room_id not preserved: %s", output)
	}

	if !strings.Contains(output, "request.video_id=vid-1") {
		t.Fatalf("public id under WithGroup not preserved: %s", output)
	}
}

func TestLogWarn_OperationalRoomIDPreserved(t *testing.T) {
	var buf bytes.Buffer

	logger := slog.New(newPrivacySanitizeHandler(slog.NewTextHandler(&buf, nil)))

	attrs := append(ErrorAttrs(errors.New("boom")), slog.String("channel_id", "UC123"), slog.Int64("room_id", 8842))
	Log(t.Context(), logger, slog.LevelWarn, "sync.poll.failed", "sync poll failed", attrs...)

	output := buf.String()

	if !strings.Contains(output, "room_id=8842") {
		t.Fatalf("room_id not preserved: %s", output)
	}

	if !strings.Contains(output, "channel_id=UC123") {
		t.Fatalf("channel_id not preserved: %s", output)
	}
}

func TestSanitizeHandler_WithGroupNamedPrivacyKeyMasksMembers(t *testing.T) {
	var buf bytes.Buffer

	logger := slog.New(newPrivacySanitizeHandler(slog.NewTextHandler(&buf, nil))).WithGroup("sender")
	logger.LogAttrs(t.Context(), slog.LevelInfo, "privacy",
		slog.String("name", kakaoSentinelID),
		slog.Int64("id", 8842),
	)

	output := buf.String()

	if strings.Contains(output, kakaoSentinelID) || strings.Contains(output, "8842") {
		t.Fatalf("WithGroup(\"sender\") member leaked: %s", output)
	}

	if !strings.Contains(output, "sender.name=***REDACTED***") {
		t.Fatalf("sender.name not masked: %s", output)
	}

	if !strings.Contains(output, "sender.id=***REDACTED***") {
		t.Fatalf("sender.id not masked: %s", output)
	}
}

func TestSanitizeHandler_WithGroupNamedPrivacyKeyMasksWithAttrs(t *testing.T) {
	var buf bytes.Buffer

	logger := slog.New(newPrivacySanitizeHandler(slog.NewTextHandler(&buf, nil))).
		WithGroup("room").
		With(slog.String("title", kakaoSentinelID))
	logger.LogAttrs(t.Context(), slog.LevelInfo, "privacy", slog.String("member", kakaoSentinelID))

	output := buf.String()

	if strings.Contains(output, kakaoSentinelID) {
		t.Fatalf("privacy group member leaked through With: %s", output)
	}
}

func TestSanitizeHandler_NestedGroupUnderPrivacyGroupMasked(t *testing.T) {
	var buf bytes.Buffer

	logger := slog.New(newPrivacySanitizeHandler(slog.NewTextHandler(&buf, nil))).
		WithGroup("sender").
		WithGroup("profile")
	logger.LogAttrs(t.Context(), slog.LevelInfo, "privacy", slog.String("nickname", kakaoSentinelID))

	output := buf.String()

	if strings.Contains(output, kakaoSentinelID) {
		t.Fatalf("nested group under privacy group leaked: %s", output)
	}
}

func TestSanitizeHandler_NonPrivacyGroupUnaffected(t *testing.T) {
	var buf bytes.Buffer

	logger := slog.New(newPrivacySanitizeHandler(slog.NewTextHandler(&buf, nil))).WithGroup("request")
	logger.LogAttrs(t.Context(), slog.LevelInfo, "privacy",
		slog.String("path", "/api/users"),
		slog.String(testVideoID, testVid1),
	)

	output := buf.String()

	if strings.Contains(output, "***REDACTED***") {
		t.Fatalf("non-privacy group was masked: %s", output)
	}

	if !strings.Contains(output, "request.path=/api/users") || !strings.Contains(output, "request.video_id=vid-1") {
		t.Fatalf("non-privacy group values not preserved: %s", output)
	}
}

func TestSanitizeHandler_AnyMapsBlockedWithoutMutation(t *testing.T) {
	nested := map[string]any{testUserID: kakaoSentinelID, testVideoID: testVid1}
	cycle := map[string]any{tokenUserName: kakaoSentinelID}

	cycle["self"] = cycle

	deep := map[string]any{tokenUserName: kakaoSentinelID}

	for range 9 {
		deep = map[string]any{testNested: deep}
	}

	cases := map[string]any{
		"operational": nested,
		"clean":       map[string]any{testVideoID: testVid1, testCount: 3},
		"nested":      map[string]any{testNested: nested},
		"cycle":       cycle,
		"depth_nine":  deep,
		"string_map":  map[string]string{testUserID: kakaoSentinelID},
	}
	for name, payload := range cases {
		t.Run(name, func(t *testing.T) {
			output := privacyOutput(t, slog.Any("payload", payload))
			if strings.Contains(output, kakaoSentinelID) || strings.Contains(output, testVid1) {
				t.Fatalf("미지원 map 원문 노출: %s", output)
			}

			if !strings.Contains(output, blockedUnsupported) {
				t.Fatalf("map 차단 사유 누락: %s", output)
			}
		})
	}

	if nested[testUserID] != kakaoSentinelID || nested[testVideoID] != testVid1 ||
		cycle[tokenUserName] != kakaoSentinelID || len(cycle) != 2 {
		t.Fatal("호출자 map 변경")
	}
}

func TestSanitizeAttr_PrivacyKeysRejectBlanketIDSuffix(t *testing.T) {
	blanket := []string{"room_id", testUserID, "Room_ID", "user-id", "channel_id", testVideoID, "content_id", "request_id", "trace_id", "id", "correlation_id"}
	for _, key := range blanket {
		if out := testPrivacy.sanitizeAttr(slog.String(key, kakaoSentinelID)); out.Value.String() != kakaoSentinelID {
			t.Errorf("%q value = %q, want preserved (no blanket *_id masking)", key, out.Value.String())
		}
	}

	masked := []string{"room", "chat_id", tokenUserName, "room_name", "thread_id", "session_thread_id", "sender"}
	for _, key := range masked {
		if out := testPrivacy.sanitizeAttr(slog.String(key, kakaoSentinelID)); out.Value.String() != redactedValue {
			t.Errorf("%q value = %q, want redacted privacy key", key, out.Value.String())
		}
	}
}
