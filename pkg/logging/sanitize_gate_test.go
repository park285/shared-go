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

	h := newSanitizeHandler(slog.NewTextHandler(&buf, nil))
	slog.New(h).Info("m", slog.String(key, value))

	return buf.String()
}

func sanitizeMessage(t *testing.T, msg string) string {
	t.Helper()

	var buf bytes.Buffer

	h := newSanitizeHandler(slog.NewTextHandler(&buf, nil))
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

// I2 회귀: Unicode case-fold trap. (?i) 정규식·EqualFold는 ſ(U+017F)↔s,
// K(U+212A, Kelvin)↔k를 fold-equivalent로 보지만, 멀티바이트 룬은 토큰과
// 바이트 폭이 달라 고정 폭 게이트가 skip할 수 있다. 게이트 출력은 정규식 직접
// 호출 결과와 byte-identical이어야 한다 (superset 불변식).
func TestRedactSecrets_UnicodeFoldTrap(t *testing.T) {
	cases := []string{
		"?paſsword=LEAKEDPW",        // long-s ſ ↔ "password"의 s
		"?toKen=LEAKEDTOK",          // Kelvin K ↔ "token"의 k
		"?ſecret=LEAKEDSEC",         // long-s ſ ↔ "secret"의 s
		"?api_Key=LEAKEDAK",         // Kelvin K ↔ "api_key"의 k
		";paſswd=LEAKEDPW",          // "passwd" 내 long-s, semicolon 구분자
		"prefixſ ?password=PLAINPW", // non-ASCII 있으나 ASCII token도 매칭됨
	}
	for _, in := range cases {
		want := redactDiagnosticWithoutGates(in)
		if got := redactSecrets(in); got != want {
			t.Errorf("redactSecrets(%q) = %q, want %q (gate must be superset of regex)", in, got, want)
		}
	}
}

func FuzzRedactSecrets_GateIsSuperset(f *testing.F) {
	seeds := []string{
		"", "?token=v", "?paſsword=v", "?toKen=v", "Bearer x.y",
		"plain text", "ſſſ", "?api_Key=ſ", "key=ſ", "?secret=ſecret",
		"API_TOKEN=x", "password: raw", `{"api_key":"x"}`,
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, in string) {
		want := redactDiagnosticWithoutGates(in)
		if got := redactSecrets(in); got != want {
			t.Fatalf("gate diverged from regex: redactSecrets(%q) = %q, want %q", in, got, want)
		}
	})
}

func TestRedactSecrets_MatchesDirectRegex(t *testing.T) {
	in := "url=https://x.test?token=secret123&Bearer foo.bar"
	want := redactDiagnosticWithoutGates(in)

	if got := redactSecrets(in); got != want {
		t.Fatalf("redactSecrets(%q) = %q, want %q", in, got, want)
	}
}

func redactDiagnosticWithoutGates(in string) string {
	out := bearerTokenRegex.ReplaceAllString(in, "${1}***REDACTED***")

	out = querySecretRegex.ReplaceAllString(out, "${1}***REDACTED***")
	out = credentialURLRegex.ReplaceAllString(out, "${1}***REDACTED***@")

	return redactSecretAssignments(out)
}

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

func TestQuerySecret_BareKeyMasked(t *testing.T) {
	out := sanitizeValue(t, "url", "https://x.test?key=visible&api_key=secret")
	if strings.Contains(out, "api_key=secret") {
		t.Fatalf("api_key query value not masked, got: %s", out)
	}

	if strings.Contains(out, "key=visible") {
		t.Fatalf("bare key query value not masked, got: %s", out)
	}

	if !strings.Contains(out, "key=***REDACTED***") {
		t.Fatalf("expected bare key query value to be masked, got: %s", out)
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

func TestIsSensitiveKey_BroadKeysStillNotMasked(t *testing.T) {
	notMasked := []string{"session", "csrf", "auth", "jwt", "username"}
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
