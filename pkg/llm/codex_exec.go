package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func (c *CodexJSONGenerator) GenerateJSON(ctx context.Context, req JSONRequest) (JSONResponse, error) {
	if c == nil {
		return JSONResponse{}, ErrNilJSONGenerator
	}
	if ctx == nil {
		return JSONResponse{}, ErrNilContext
	}

	req.Model = c.resolvedModel(req.Model)
	if err := ValidateJSONRequest(req); err != nil {
		return JSONResponse{}, err
	}
	if strings.TrimSpace(req.UserPrompt) == "" && strings.TrimSpace(req.SystemPrompt) == "" {
		return JSONResponse{}, fmt.Errorf("%w: prompt is empty", ErrInvalidJSONRequest)
	}

	if err := c.EnsureLogin(ctx); err != nil {
		return JSONResponse{}, err
	}
	if err := c.acquire(ctx); err != nil {
		return JSONResponse{}, err
	}
	defer c.release()

	ctx, cancel := c.commandContext(ctx, c.timeout)
	defer cancel()

	runDir, err := os.MkdirTemp(c.workDir, "llm-codex-*")
	if err != nil {
		return JSONResponse{}, fmt.Errorf("create codex run dir: %w", err)
	}
	defer removeAllBestEffort(runDir)

	schemaPath := filepath.Join(runDir, "schema.json")
	schema, err := json.Marshal(req.Schema)
	if err != nil {
		return JSONResponse{}, fmt.Errorf("marshal codex schema: %w", err)
	}
	if writeErr := os.WriteFile(schemaPath, schema, 0o600); writeErr != nil {
		return JSONResponse{}, fmt.Errorf("write codex schema: %w", writeErr)
	}

	outputPath := filepath.Join(runDir, "output.json")
	stdout, stderr, err := c.runRaw(ctx, c.execArgs(req, schemaPath, outputPath, runDir), codexPrompt(req))
	if err != nil {
		return JSONResponse{}, fmt.Errorf("codex exec %s failed: %w%s", taskName(req), err, c.formatCommandOutput(stdout, stderr))
	}

	payload, readErr := os.ReadFile(outputPath) //nolint:gosec // outputPath is created under this private per-run temp directory.
	if readErr != nil || len(bytes.TrimSpace(payload)) == 0 {
		payload = []byte(stdout)
	}
	payload = bytes.TrimSpace(payload)
	if len(payload) == 0 {
		return JSONResponse{}, fmt.Errorf("codex exec %s returned empty output%s", taskName(req), c.formatCommandOutput(stdout, stderr))
	}

	var decoded any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return JSONResponse{}, fmt.Errorf("decode codex %s json failed: %w; output=%s", taskName(req), err, c.safeSnippet(string(payload), 2048))
	}

	return JSONResponse{
		Text:  string(payload),
		Model: req.Model,
	}, nil
}

func (c *CodexJSONGenerator) Model() string {
	if c == nil {
		return ""
	}
	return c.model
}

func (c *CodexJSONGenerator) execArgs(req JSONRequest, schemaPath, outputPath, runDir string) []string {
	args := []string{
		"exec",
		"--ephemeral",
		"--skip-git-repo-check",
		"--ignore-rules",
	}
	if c.profile == "" {
		args = append(args, "--ignore-user-config")
	}
	args = append(args,
		"--color", "never",
		"--sandbox", c.sandbox,
		"--ask-for-approval", "never",
		"--model", req.Model,
		"--output-schema", schemaPath,
		"--output-last-message", outputPath,
		"--cd", runDir,
	)
	if c.profile != "" {
		args = append(args, "--profile", c.profile)
	}
	return append(args, "-")
}

func (c *CodexJSONGenerator) acquire(ctx context.Context) error {
	select {
	case c.sem <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *CodexJSONGenerator) release() {
	select {
	case <-c.sem:
	default:
	}
}

func (c *CodexJSONGenerator) commandContext(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	if timeout <= 0 {
		return ctx, func() {}
	}
	if _, ok := ctx.Deadline(); ok {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, timeout)
}

func (c *CodexJSONGenerator) runRaw(ctx context.Context, args []string, stdin string) (string, string, error) {
	cmd := exec.CommandContext(ctx, c.binPath, args...) //nolint:gosec // binPath is explicit operator/test configuration for the Codex CLI adapter.
	cmd.Env = c.commandEnv()
	if c.workDir != "" {
		cmd.Dir = c.workDir
	}
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}

	var stdout cappedBuffer
	var stderr cappedBuffer
	stdout.limit = defaultCommandOutputLimit
	stderr.limit = defaultCommandOutputLimit
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	if err != nil {
		var execErr *exec.Error
		if errors.As(err, &execErr) {
			return stdout.String(), stderr.String(), fmt.Errorf("codex binary %q not executable or not found: %w", c.binPath, err)
		}
		return stdout.String(), stderr.String(), err
	}
	return stdout.String(), stderr.String(), nil
}

var codexEnvAllowlist = map[string]struct{}{
	"PATH":     {},
	"HOME":     {},
	"LANG":     {},
	"LC_ALL":   {},
	"LC_CTYPE": {},
	"TZ":       {},
	"TMPDIR":   {},
}

func (c *CodexJSONGenerator) commandEnv() []string {
	base := os.Environ()
	env := make([]string, 0, len(codexEnvAllowlist)+2)
	for _, kv := range base {
		idx := strings.IndexByte(kv, '=')
		if idx <= 0 {
			continue
		}
		key := kv[:idx]
		if key == "CODEX_HOME" {
			continue
		}
		if _, ok := codexEnvAllowlist[key]; ok {
			env = append(env, kv)
		}
	}
	if c.home != "" {
		env = append(env, "CODEX_HOME="+c.home)
	}
	return append(env, "NO_COLOR=1")
}

func (c *CodexJSONGenerator) formatCommandOutput(stdout, stderr string) string {
	stdout = c.safeSnippet(stdout, 2048)
	stderr = c.safeSnippet(stderr, 2048)
	if stdout == "" && stderr == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString("; command output:")
	if stdout != "" {
		b.WriteString(" stdout=")
		b.WriteString(stdout)
	}
	if stderr != "" {
		b.WriteString(" stderr=")
		b.WriteString(stderr)
	}
	return b.String()
}

func (c *CodexJSONGenerator) safeSnippet(s string, limit int) string {
	s = strings.TrimSpace(s)
	if c != nil && c.accessToken != "" {
		s = strings.ReplaceAll(s, c.accessToken, "***REDACTED***")
	}
	return RedactDiagnostic(s, limit)
}

func (c *CodexJSONGenerator) effectiveHomeForMessage() string {
	if c == nil || strings.TrimSpace(c.home) == "" {
		return "~/.codex"
	}
	return c.home
}

func (c *CodexJSONGenerator) resolvedModel(model string) string {
	model = strings.TrimSpace(model)
	if model != "" {
		return model
	}
	if c == nil {
		return ""
	}
	return c.model
}

func codexPrompt(req JSONRequest) string {
	systemPrompt := strings.TrimSpace(req.SystemPrompt)
	userPrompt := strings.TrimSpace(req.UserPrompt)
	if systemPrompt == "" {
		return userPrompt
	}
	if userPrompt == "" {
		return systemPrompt
	}
	return systemPrompt + "\n\n" + userPrompt
}

func taskName(req JSONRequest) string {
	task := strings.TrimSpace(req.TaskName)
	if task == "" {
		return strings.TrimSpace(req.SchemaName)
	}
	return task
}

func minDuration(a, b time.Duration) time.Duration {
	if a <= 0 {
		return b
	}
	if b <= 0 {
		return a
	}
	if a < b {
		return a
	}
	return b
}

func removeAllBestEffort(path string) {
	if err := os.RemoveAll(path); err != nil {
		return
	}
}
