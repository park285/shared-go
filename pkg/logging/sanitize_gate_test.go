package logging

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func sanitizeValue(t *testing.T, key, value string) string {
	t.Helper()
	var buf bytes.Buffer
	h := NewSanitizeHandler(slog.NewTextHandler(&buf, nil))
	slog.New(h).Info("m", slog.String(key, value))
	return buf.String()
}

func sanitizeMessage(t *testing.T, msg string) string {
	t.Helper()
	var buf bytes.Buffer
	h := NewSanitizeHandler(slog.NewTextHandler(&buf, nil))
	slog.New(h).Info(msg)
	return buf.String()
}

// I2: 싼 pre-check 게이트가 혼합 대소문자 Bearer를 우회시키지 않아야 한다.
func TestRedactSecrets_MixedCaseBearerNotBypassed(t *testing.T) {
	cases := []string{
		"Bearer abc123.def",
		"bEaReR abc123.def",
		"BEARER abc123.def",
		"prefix bearer abc123.def suffix",
	}
	for _, in := range cases {
		out := sanitizeValue(t, "header", in)
		if strings.Contains(out, "abc123.def") {
			t.Errorf("bearer token not redacted for %q, got: %s", in, out)
		}
		if !strings.Contains(out, "***REDACTED***") {
			t.Errorf("expected redaction for %q, got: %s", in, out)
		}
	}
}

// I2: 게이트가 매치 없는 입력에 대해 출력을 변경하지 않아야 한다 (idempotent skip).
func TestRedactSecrets_NoMatchUnchanged(t *testing.T) {
	in := "plain message with no secrets and value=visible"
	if got := redactSecrets(in); got != in {
		t.Fatalf("redactSecrets(%q) = %q, want unchanged", in, got)
	}
}

// I2: 게이트 통과 후 정규식 결과가 직접 호출과 동일해야 한다.
func TestRedactSecrets_MatchesDirectRegex(t *testing.T) {
	in := "url=https://x.test?token=secret123&Bearer foo.bar"
	want := bearerTokenRegex.ReplaceAllString(in, "${1}***REDACTED***")
	want = querySecretRegex.ReplaceAllString(want, "${1}***REDACTED***")
	if got := redactSecrets(in); got != want {
		t.Fatalf("redactSecrets(%q) = %q, want %q", in, got, want)
	}
}

// I3: querySecretRegex 구분자 확장 — ;password=x (DSN 케이스) 마스킹.
func TestQuerySecret_SemicolonSeparator(t *testing.T) {
	out := sanitizeValue(t, "dsn", "host=db;password=p4ssw0rd;db=app")
	if strings.Contains(out, "p4ssw0rd") {
		t.Errorf("semicolon-separated password must be masked, got: %s", out)
	}
	if !strings.Contains(out, "***REDACTED***") {
		t.Errorf("expected redaction, got: %s", out)
	}
}

// I3: 신규 query 키 (pwd/passwd/private_key/secret_key) 마스킹.
func TestQuerySecret_NewKeys(t *testing.T) {
	cases := []struct{ key, secret string }{
		{"pwd", "pwdval"},
		{"passwd", "passwdval"},
		{"private_key", "pkval"},
		{"secret_key", "skval"},
	}
	for _, c := range cases {
		in := "https://x.test?" + c.key + "=" + c.secret + "&keep=ok"
		out := sanitizeValue(t, "url", in)
		if strings.Contains(out, c.secret) {
			t.Errorf("query key %q value not masked, got: %s", c.key, out)
		}
		if !strings.Contains(out, "keep=ok") {
			t.Errorf("non-secret query param dropped for %q, got: %s", c.key, out)
		}
	}
}

// I3: 신규 민감 키 (pwd/passwd/private_key/secret_key 및 _suffix 변형) 마스킹.
func TestIsSensitiveKey_NewKeys(t *testing.T) {
	masked := []string{
		"pwd", "passwd", "private_key", "secret_key",
		"db_pwd", "user_passwd", "rsa_private_key", "hmac_secret_key",
		"PWD", "Private_Key",
	}
	for _, k := range masked {
		if !isSensitiveKey(k) {
			t.Errorf("isSensitiveKey(%q) = false, want true", k)
		}
	}
}

// I3: 과광역 키 (session/csrf/auth/key bare)는 추가하지 않았는지 확인 (false-positive 보류).
func TestIsSensitiveKey_BroadKeysStillNotMasked(t *testing.T) {
	notMasked := []string{"session", "csrf", "auth", "key", "jwt", "username"}
	for _, k := range notMasked {
		if isSensitiveKey(k) {
			t.Errorf("isSensitiveKey(%q) = true, want false (broad key must stay unmasked)", k)
		}
	}
}

// I3: message lane에서도 ;password= DSN 케이스가 마스킹되는지.
func TestMessageMasking_SemicolonDSN(t *testing.T) {
	out := sanitizeMessage(t, "connecting host=db;password=topsecret;sslmode=require")
	if strings.Contains(out, "topsecret") {
		t.Errorf("DSN password in message must be masked, got: %s", out)
	}
}
