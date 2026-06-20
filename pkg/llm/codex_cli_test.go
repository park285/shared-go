package llm

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestCodexJSONGeneratorRunsExecWithSchemaOutputAndScrubbedEnv(t *testing.T) {
	tmp := t.TempDir()
	argsPath := filepath.Join(tmp, "args.txt")
	envPath := filepath.Join(tmp, "env.txt")
	stdinPath := filepath.Join(tmp, "stdin.txt")
	schemaPath := filepath.Join(tmp, "schema.txt")
	script := writeFakeCodex(t, fmt.Sprintf(`#!/usr/bin/env bash
set -euo pipefail
printf '%%s\n' "$@" > %q
env | sort > %q
cat > %q
output=""
schema=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    --output-last-message) shift; output="$1" ;;
    --output-schema) shift; schema="$1" ;;
  esac
  shift || true
done
cp "$schema" %q
printf '{"answer":"yes"}' > "$output"
`, argsPath, envPath, stdinPath, schemaPath))
	t.Setenv("CODEX_ACCESS_TOKEN", "env-access-token")
	t.Setenv("OPENAI_API_KEY", "env-openai-key")
	t.Setenv("CODEX_API_KEY", "env-codex-key")

	generator, err := NewCodexJSONGenerator(CodexConfig{
		BinPath:        script,
		Home:           filepath.Join(tmp, "home"),
		WorkDir:        filepath.Join(tmp, "work"),
		Sandbox:        "workspace-write",
		Profile:        "task-profile",
		Timeout:        time.Second,
		MaxConcurrency: 1,
		LoginCheck:     false,
	})
	if err != nil {
		t.Fatalf("NewCodexJSONGenerator error = %v", err)
	}

	resp, err := generator.GenerateJSON(t.Context(), JSONRequest{
		TaskName:     "twentyq_answer_question",
		SystemPrompt: "system prompt",
		UserPrompt:   "user prompt",
		SchemaName:   "answer",
		Schema:       map[string]any{"type": "object", "properties": map[string]any{"answer": map[string]any{"type": "string"}}},
		Model:        "gpt-test",
	})
	if err != nil {
		t.Fatalf("GenerateJSON error = %v", err)
	}
	if resp.Text != `{"answer":"yes"}` {
		t.Fatalf("Text = %q, want JSON output", resp.Text)
	}
	if resp.Model != "gpt-test" {
		t.Fatalf("Model = %q, want gpt-test", resp.Model)
	}

	args := readFile(t, argsPath)
	for _, want := range []string{"exec", "--ephemeral", "--skip-git-repo-check", "--ignore-rules", "--color", "never", "--sandbox", "workspace-write", "--ask-for-approval", "never", "--model", "gpt-test", "--profile", "task-profile", "--output-schema", "--output-last-message", "--cd"} {
		if !strings.Contains(args, want) {
			t.Fatalf("args missing %q in:\n%s", want, args)
		}
	}
	if strings.Contains(args, "--ignore-user-config") {
		t.Fatalf("args unexpectedly ignored user config with profile set:\n%s", args)
	}

	stdin := readFile(t, stdinPath)
	if !strings.Contains(stdin, "system prompt") || !strings.Contains(stdin, "user prompt") {
		t.Fatalf("stdin = %q, want both prompts", stdin)
	}
	schema := readFile(t, schemaPath)
	if !strings.Contains(schema, `"answer"`) {
		t.Fatalf("schema = %q, want marshaled schema", schema)
	}
	env := readFile(t, envPath)
	for _, secret := range []string{"env-access-token", "env-openai-key", "env-codex-key", "CODEX_ACCESS_TOKEN=", "OPENAI_API_KEY=", "CODEX_API_KEY="} {
		if strings.Contains(env, secret) {
			t.Fatalf("env leaked %q in:\n%s", secret, env)
		}
	}
	if !strings.Contains(env, "CODEX_HOME="+filepath.Join(tmp, "home")) {
		t.Fatalf("env missing CODEX_HOME, got:\n%s", env)
	}
	if !strings.Contains(env, "NO_COLOR=1") {
		t.Fatalf("env missing NO_COLOR, got:\n%s", env)
	}
}

func TestCodexJSONGeneratorInvalidJSONRedactsToken(t *testing.T) {
	tmp := t.TempDir()
	script := writeFakeCodex(t, `#!/usr/bin/env bash
set -euo pipefail
output=""
while [ "$#" -gt 0 ]; do
  if [ "$1" = "--output-last-message" ]; then
    shift
    output="$1"
  fi
  shift || true
done
printf 'not-json login-token-secret' > "$output"
`)
	generator, err := NewCodexJSONGenerator(CodexConfig{
		BinPath:     script,
		WorkDir:     filepath.Join(tmp, "work"),
		AccessToken: "login-token-secret",
		Timeout:     time.Second,
		LoginCheck:  false,
	})
	if err != nil {
		t.Fatalf("NewCodexJSONGenerator error = %v", err)
	}

	_, err = generator.GenerateJSON(t.Context(), validCodexJSONRequest())
	if err == nil {
		t.Fatal("GenerateJSON invalid JSON error = nil, want error")
	}
	if strings.Contains(err.Error(), "login-token-secret") {
		t.Fatalf("GenerateJSON leaked access token in error: %v", err)
	}
	if !strings.Contains(err.Error(), "decode codex invalid_task json failed") {
		t.Fatalf("GenerateJSON error = %v, want decode task context", err)
	}
}

func TestCodexJSONGeneratorContextCancellation(t *testing.T) {
	tmp := t.TempDir()
	script := writeFakeCodex(t, `#!/usr/bin/env bash
sleep 5
`)
	generator, err := NewCodexJSONGenerator(CodexConfig{
		BinPath:    script,
		WorkDir:    filepath.Join(tmp, "work"),
		Timeout:    time.Second,
		LoginCheck: false,
	})
	if err != nil {
		t.Fatalf("NewCodexJSONGenerator error = %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()

	_, err = generator.GenerateJSON(ctx, validCodexJSONRequest())
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("GenerateJSON error = %v, want context deadline exceeded", err)
	}
}

func TestCodexJSONGeneratorAccessTokenLoginUsesStdinAndScrubsEnv(t *testing.T) {
	tmp := t.TempDir()
	tokenPath := filepath.Join(tmp, "login-token.txt")
	envPath := filepath.Join(tmp, "login-env.txt")
	markerPath := filepath.Join(tmp, "logged-in")
	script := writeFakeCodex(t, fmt.Sprintf(`#!/usr/bin/env bash
set -euo pipefail
if [ "$1" = "login" ] && [ "$2" = "status" ]; then
  if [ -f %[3]q ]; then
    exit 0
  fi
  exit 1
fi
if [ "$1" = "login" ] && [ "$2" = "--with-access-token" ]; then
  env | sort > %[2]q
  cat > %[1]q
  touch %[3]q
  exit 0
fi
output=""
while [ "$#" -gt 0 ]; do
  if [ "$1" = "--output-last-message" ]; then
    shift
    output="$1"
  fi
  shift || true
done
printf '{"answer":"yes"}' > "$output"
`, tokenPath, envPath, markerPath))
	t.Setenv("CODEX_ACCESS_TOKEN", "env-token-must-not-leak")

	generator, err := NewCodexJSONGenerator(CodexConfig{
		BinPath:     script,
		WorkDir:     filepath.Join(tmp, "work"),
		AccessToken: "login-token-secret",
		Timeout:     time.Second,
		LoginCheck:  true,
	})
	if err != nil {
		t.Fatalf("NewCodexJSONGenerator error = %v", err)
	}

	if _, err := generator.GenerateJSON(t.Context(), validCodexJSONRequest()); err != nil {
		t.Fatalf("GenerateJSON error = %v", err)
	}
	if got := strings.TrimSpace(readFile(t, tokenPath)); got != "login-token-secret" {
		t.Fatalf("login token stdin = %q, want access token", got)
	}
	env := readFile(t, envPath)
	if strings.Contains(env, "env-token-must-not-leak") || strings.Contains(env, "CODEX_ACCESS_TOKEN=") {
		t.Fatalf("login env leaked access token:\n%s", env)
	}
}

func TestNewCodexJSONGeneratorFromEnvReadsCodexSettings(t *testing.T) {
	tmp := t.TempDir()
	argsPath := filepath.Join(tmp, "args.txt")
	envPath := filepath.Join(tmp, "env.txt")
	tokenFile := filepath.Join(tmp, "token")
	if err := os.WriteFile(tokenFile, []byte("file-token-secret\n"), 0o600); err != nil {
		t.Fatalf("write token file: %v", err)
	}
	script := writeFakeCodex(t, fmt.Sprintf(`#!/usr/bin/env bash
set -euo pipefail
printf '%%s\n' "$@" > %q
env | sort > %q
output=""
while [ "$#" -gt 0 ]; do
  if [ "$1" = "--output-last-message" ]; then
    shift
    output="$1"
  fi
  shift || true
done
printf '{"answer":"yes"}' > "$output"
`, argsPath, envPath))
	t.Setenv("CODEX_BIN", script)
	t.Setenv("CODEX_HOME", filepath.Join(tmp, "home"))
	t.Setenv("CODEX_MODEL", "env-model")
	t.Setenv("CODEX_PROFILE", "env-profile")
	t.Setenv("CODEX_WORK_DIR", filepath.Join(tmp, "work"))
	t.Setenv("CODEX_SANDBOX", "workspace-write")
	t.Setenv("CODEX_ACCESS_TOKEN_FILE", tokenFile)
	t.Setenv("CODEX_MAX_CONCURRENCY", "1")
	t.Setenv("CODEX_LOGIN_CHECK", "false")

	generator, err := NewCodexJSONGeneratorFromEnv()
	if err != nil {
		t.Fatalf("NewCodexJSONGeneratorFromEnv error = %v", err)
	}
	if generator.Model() != "env-model" {
		t.Fatalf("Model = %q, want env-model", generator.Model())
	}

	req := validCodexJSONRequest()
	req.Model = ""
	if _, err := generator.GenerateJSON(t.Context(), req); err != nil {
		t.Fatalf("GenerateJSON error = %v", err)
	}

	args := readFile(t, argsPath)
	for _, want := range []string{"--model\nenv-model", "--profile\nenv-profile", "--sandbox\nworkspace-write"} {
		if !strings.Contains(args, want) {
			t.Fatalf("args missing %q in:\n%s", want, args)
		}
	}
	env := readFile(t, envPath)
	if !strings.Contains(env, "CODEX_HOME="+filepath.Join(tmp, "home")) {
		t.Fatalf("env missing CODEX_HOME:\n%s", env)
	}
	if strings.Contains(env, "file-token-secret") || strings.Contains(env, "CODEX_ACCESS_TOKEN=") {
		t.Fatalf("env leaked token file value:\n%s", env)
	}
}

func TestCodexJSONGeneratorConcurrencyLimit(t *testing.T) {
	tmp := t.TempDir()
	activePath := filepath.Join(tmp, "active")
	maxPath := filepath.Join(tmp, "max")
	lockDir := filepath.Join(tmp, "lock")
	script := writeFakeCodex(t, fmt.Sprintf(`#!/usr/bin/env bash
set -euo pipefail
output=""
while [ "$#" -gt 0 ]; do
  if [ "$1" = "--output-last-message" ]; then
    shift
    output="$1"
  fi
  shift || true
done
while ! mkdir %[3]q 2>/dev/null; do sleep 0.005; done
active=0
if [ -f %[1]q ]; then active=$(cat %[1]q); fi
active=$((active + 1))
echo "$active" > %[1]q
max=0
if [ -f %[2]q ]; then max=$(cat %[2]q); fi
if [ "$active" -gt "$max" ]; then echo "$active" > %[2]q; fi
rmdir %[3]q
sleep 0.08
printf '{"answer":"yes"}' > "$output"
while ! mkdir %[3]q 2>/dev/null; do sleep 0.005; done
active=$(cat %[1]q)
active=$((active - 1))
echo "$active" > %[1]q
rmdir %[3]q
`, activePath, maxPath, lockDir))

	generator, err := NewCodexJSONGenerator(CodexConfig{
		BinPath:        script,
		WorkDir:        filepath.Join(tmp, "work"),
		Timeout:        2 * time.Second,
		MaxConcurrency: 1,
		LoginCheck:     false,
	})
	if err != nil {
		t.Fatalf("NewCodexJSONGenerator error = %v", err)
	}

	var wg sync.WaitGroup
	errs := make(chan error, 3)
	for range 3 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := generator.GenerateJSON(context.Background(), validCodexJSONRequest())
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("GenerateJSON concurrent error = %v", err)
		}
	}
	if got := strings.TrimSpace(readFile(t, maxPath)); got != "1" {
		t.Fatalf("max concurrent processes = %q, want 1", got)
	}
}

func validCodexJSONRequest() JSONRequest {
	return JSONRequest{
		TaskName:     "invalid_task",
		SystemPrompt: "system",
		UserPrompt:   "user",
		SchemaName:   "answer",
		Schema:       map[string]any{"type": "object"},
		Model:        "gpt-test",
	}
}

func writeFakeCodex(t *testing.T, body string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "codex")
	if err := os.WriteFile(path, []byte(body), 0o700); err != nil {
		t.Fatalf("write fake codex: %v", err)
	}
	return path
}

func readFile(t *testing.T, path string) string {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}
