package logging

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

var testPrivacy = mustTestPrivacyKeys("room", "room_name", "chat_id", tokenUserName, "thread_id", "session_thread_id", "sender")

func mustTestPrivacyKeys(keys ...string) privacyKeys {
	privacy, err := newPrivacyKeys(keys)
	if err != nil {
		panic(err)
	}

	return privacy
}

func newPrivacySanitizeHandler(inner slog.Handler) *sanitizeHandler {
	handler := newSanitizeHandler(inner)

	handler.privacy = testPrivacy

	return handler
}

func TestPrivacyKeysComeOnlyFromService(t *testing.T) {
	var buf bytes.Buffer

	logger, closer, err := enableFileLoggingWithStdout(&buf, Config{}, "", Options{PrivacyKeys: []string{"Room", "session-thread-id"}})
	if err != nil || closer != nil {
		t.Fatalf("enable console logging = %v, %v", closer, err)
	}

	logger.Info("privacy_probe",
		slog.String("room", kakaoSentinelID+"-room"),
		slog.String("session_thread_id", kakaoSentinelID+"-thread"),
		slog.String("sender", kakaoSentinelID+"-sender"),
		slog.String("game_key", kakaoSentinelID+"-game"),
		slog.String("password", kakaoSentinelID+"-password"),
	)

	output := buf.String()

	for _, masked := range []string{"-room", "-thread", "-password"} {
		if strings.Contains(output, kakaoSentinelID+masked) {
			t.Errorf("%s leaked: %s", masked, output)
		}
	}

	for _, kept := range []string{"-sender", "-game"} {
		if !strings.Contains(output, kakaoSentinelID+kept) {
			t.Errorf("%s was masked without a service privacy key: %s", kept, output)
		}
	}
}

func TestPrivacyKeysRejectEmptyKey(t *testing.T) {
	if _, _, err := enableFileLoggingWithStdout(&bytes.Buffer{}, Config{}, "", Options{PrivacyKeys: []string{"room", " "}}); err == nil {
		t.Fatal("enable logging with an empty privacy key error = nil")
	}
}
