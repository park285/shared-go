package llm

import (
	"strings"
	"testing"
)

func codexEnvKeys(env []string) map[string]string {
	out := make(map[string]string, len(env))
	for _, kv := range env {
		idx := strings.IndexByte(kv, '=')
		if idx <= 0 {
			continue
		}
		out[kv[:idx]] = kv[idx+1:]
	}
	return out
}

func TestSG02CodexCommandEnvUsesAllowlist_496ed9f2(t *testing.T) {
	t.Setenv("PATH", "/usr/bin:/bin")
	t.Setenv("HOME", "/home/probe")
	t.Setenv("LANG", "en_US.UTF-8")
	t.Setenv("TZ", "UTC")
	t.Setenv("RANDOM_NON_SECRET_VAR", "should-not-pass")
	t.Setenv("SOME_BUSINESS_CONFIG", "also-dropped")

	gen := &CodexJSONGenerator{home: "/codex/home"}
	env := codexEnvKeys(gen.commandEnv())

	for key := range env {
		switch key {
		case "PATH", "HOME", "LANG", "LC_ALL", "LC_CTYPE", "TZ", "TMPDIR", "CODEX_HOME", "NO_COLOR":
		default:
			t.Fatalf("commandEnv() leaked non-allowlisted key %q (value=%q)", key, env[key])
		}
	}

	if _, ok := env["RANDOM_NON_SECRET_VAR"]; ok {
		t.Fatal("commandEnv() forwarded RANDOM_NON_SECRET_VAR, allowlist must drop it")
	}
	if _, ok := env["SOME_BUSINESS_CONFIG"]; ok {
		t.Fatal("commandEnv() forwarded SOME_BUSINESS_CONFIG, allowlist must drop it")
	}
	if env["PATH"] != "/usr/bin:/bin" {
		t.Fatalf("commandEnv() PATH = %q, want passthrough of allowlisted value", env["PATH"])
	}
}

func TestSG02CodexCommandEnvDropsCommonSecrets_496ed9f2(t *testing.T) {
	secrets := map[string]string{
		"CODEX_ACCESS_TOKEN":    "tok",
		"OPENAI_API_KEY":        "sk-openai",
		"CODEX_API_KEY":         "sk-codex",
		"GITHUB_TOKEN":          "ghp_x",
		"AWS_SECRET_ACCESS_KEY": "aws-secret",
		"AWS_ACCESS_KEY_ID":     "aws-id",
		"DATABASE_URL":          "postgres://u:p@h/db",
		"REDIS_URL":             "redis://h",
		"VALKEY_URL":            "valkey://h",
		"SLACK_TOKEN":           "xoxb-1",
		"GOOGLE_API_KEY":        "g-key",
		"MOONSHOT_API_KEY":      "ms-key",
		"KIMI_API_KEY":          "kimi-key",
		"ANTHROPIC_API_KEY":     "ant-key",
		"SERVICE_SECRET":        "svc-secret",
		"SOME_PASSWORD":         "pw",
		"PRIVATE_KEY":           "----",
		"SESSION_TOKEN":         "sess",
		"BOT_TOKEN":             "bot",
		"HMAC_SECRET":           "hmac",
	}
	if len(secrets) < 20 {
		t.Fatalf("test setup: want >=20 secret keys, have %d", len(secrets))
	}
	for k, v := range secrets {
		t.Setenv(k, v)
	}

	gen := &CodexJSONGenerator{home: "/codex/home"}
	env := codexEnvKeys(gen.commandEnv())

	for k, v := range secrets {
		if _, ok := env[k]; ok {
			t.Fatalf("commandEnv() leaked secret key %q into child env", k)
		}
		for ck, cv := range env {
			if cv == v {
				t.Fatalf("commandEnv() leaked secret value of %q via child key %q", k, ck)
			}
		}
	}
}

func TestSG02CodexCommandEnvSetsIsolatedHome_496ed9f2(t *testing.T) {
	t.Setenv("CODEX_HOME", "/parent/leaked/home")

	gen := &CodexJSONGenerator{home: "/isolated/codex/home"}
	env := codexEnvKeys(gen.commandEnv())

	if env["CODEX_HOME"] != "/isolated/codex/home" {
		t.Fatalf("commandEnv() CODEX_HOME = %q, want isolated %q (parent CODEX_HOME must not pass through)", env["CODEX_HOME"], "/isolated/codex/home")
	}
	if env["NO_COLOR"] != "1" {
		t.Fatalf("commandEnv() NO_COLOR = %q, want 1", env["NO_COLOR"])
	}
}
