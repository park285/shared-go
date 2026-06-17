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
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/park285/shared-go/pkg/envutil"
)

const (
	defaultCodexBin            = "codex"
	defaultCodexModel          = "gpt-5.5"
	defaultCodexSandbox        = "read-only"
	defaultCodexMaxConcurrency = 1
	defaultCodexTimeout        = 30 * time.Second
	defaultCommandOutputLimit  = 8 * 1024
)

var ErrCodexAuthRequired = errors.New("codex oauth authentication required")

type CodexConfig struct {
	BinPath        string
	Home           string
	Model          string
	Profile        string
	WorkDir        string
	Sandbox        string
	AccessToken    string
	Timeout        time.Duration
	MaxConcurrency int
	LoginCheck     bool
}

type CodexJSONGenerator struct {
	binPath     string
	home        string
	model       string
	profile     string
	workDir     string
	sandbox     string
	accessToken string
	timeout     time.Duration
	loginCheck  bool

	sem chan struct{}

	loginMu  sync.Mutex
	loggedIn bool
}

func NewCodexJSONGenerator(cfg CodexConfig) (*CodexJSONGenerator, error) {
	binPath := strings.TrimSpace(cfg.BinPath)
	if binPath == "" {
		binPath = defaultCodexBin
	}

	model := strings.TrimSpace(cfg.Model)
	if model == "" {
		model = defaultCodexModel
	}

	sandbox := strings.TrimSpace(cfg.Sandbox)
	if sandbox == "" {
		sandbox = defaultCodexSandbox
	}
	switch sandbox {
	case "read-only", "workspace-write", "danger-full-access":
	default:
		return nil, fmt.Errorf("invalid codex sandbox %q", sandbox)
	}

	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultCodexTimeout
	}

	maxConcurrency := cfg.MaxConcurrency
	if maxConcurrency <= 0 {
		maxConcurrency = defaultCodexMaxConcurrency
	}

	workDir := strings.TrimSpace(cfg.WorkDir)
	if workDir == "" {
		workDir = os.TempDir()
	}
	if err := os.MkdirAll(workDir, 0o700); err != nil {
		return nil, fmt.Errorf("create codex work dir: %w", err)
	}

	home := strings.TrimSpace(cfg.Home)
	if home != "" {
		if err := os.MkdirAll(home, 0o700); err != nil {
			return nil, fmt.Errorf("create codex home: %w", err)
		}
	}

	return &CodexJSONGenerator{
		binPath:     binPath,
		home:        home,
		model:       model,
		profile:     strings.TrimSpace(cfg.Profile),
		workDir:     workDir,
		sandbox:     sandbox,
		accessToken: strings.TrimSpace(cfg.AccessToken),
		timeout:     timeout,
		loginCheck:  cfg.LoginCheck,
		sem:         make(chan struct{}, maxConcurrency),
	}, nil
}

func NewCodexJSONGeneratorFromEnv() (*CodexJSONGenerator, error) {
	maxConcurrency, err := codexMaxConcurrencyFromEnv()
	if err != nil {
		return nil, err
	}

	return NewCodexJSONGenerator(CodexConfig{
		BinPath:        strings.TrimSpace(os.Getenv("CODEX_BIN")),
		Home:           strings.TrimSpace(os.Getenv("CODEX_HOME")),
		Model:          codexModelFromEnv(),
		Profile:        strings.TrimSpace(os.Getenv("CODEX_PROFILE")),
		WorkDir:        strings.TrimSpace(os.Getenv("CODEX_WORK_DIR")),
		Sandbox:        strings.TrimSpace(os.Getenv("CODEX_SANDBOX")),
		AccessToken:    envutil.StringOrFile("CODEX_ACCESS_TOKEN", ""),
		MaxConcurrency: maxConcurrency,
		LoginCheck:     codexLoginCheckFromEnv(),
	})
}

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

func (c *CodexJSONGenerator) EnsureLogin(ctx context.Context) error {
	if c == nil {
		return ErrNilJSONGenerator
	}
	if !c.loginCheck {
		return nil
	}

	c.loginMu.Lock()
	defer c.loginMu.Unlock()

	if c.loggedIn {
		return nil
	}
	if err := c.loginStatus(ctx); err == nil {
		c.loggedIn = true
		return nil
	} else if c.accessToken == "" {
		return fmt.Errorf("%w: run `codex login --device-auth` in CODEX_HOME=%q or mount an existing auth.json", ErrCodexAuthRequired, c.effectiveHomeForMessage())
	}

	if err := c.loginWithAccessToken(ctx); err != nil {
		return fmt.Errorf("codex login with access token failed: %w", err)
	}
	if err := c.loginStatus(ctx); err != nil {
		return fmt.Errorf("%w: codex login status failed after token login: %w", ErrCodexAuthRequired, err)
	}
	c.loggedIn = true
	return nil
}

func (c *CodexJSONGenerator) loginStatus(ctx context.Context) error {
	ctx, cancel := c.commandContext(ctx, minDuration(c.timeout, 15*time.Second))
	defer cancel()

	_, _, err := c.runRaw(ctx, []string{"login", "status"}, "")
	return err
}

func (c *CodexJSONGenerator) loginWithAccessToken(ctx context.Context) error {
	if c.accessToken == "" {
		return ErrCodexAuthRequired
	}

	ctx, cancel := c.commandContext(ctx, minDuration(c.timeout, 30*time.Second))
	defer cancel()

	_, _, err := c.runRaw(ctx, []string{"login", "--with-access-token"}, c.accessToken+"\n")
	return err
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

func (c *CodexJSONGenerator) commandEnv() []string {
	base := os.Environ()
	env := make([]string, 0, len(base)+3)
	for _, kv := range base {
		idx := strings.IndexByte(kv, '=')
		if idx <= 0 {
			continue
		}
		key := kv[:idx]
		switch key {
		case "CODEX_ACCESS_TOKEN", "OPENAI_API_KEY", "CODEX_API_KEY":
			continue
		default:
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

type cappedBuffer struct {
	buf   bytes.Buffer
	limit int
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	if b == nil {
		return len(p), nil
	}
	if b.limit <= 0 {
		return len(p), nil
	}
	remaining := b.limit - b.buf.Len()
	if remaining > 0 {
		var err error
		if len(p) > remaining {
			_, err = b.buf.Write(p[:remaining])
		} else {
			_, err = b.buf.Write(p)
		}
		if err != nil {
			return 0, fmt.Errorf("write capped buffer: %w", err)
		}
	}
	return len(p), nil
}

func (b *cappedBuffer) String() string {
	if b == nil {
		return ""
	}
	return b.buf.String()
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

func codexModelFromEnv() string {
	model := strings.TrimSpace(os.Getenv("CODEX_MODEL"))
	if model != "" {
		return model
	}
	return strings.TrimSpace(os.Getenv("LLM_MODEL_DEFAULT"))
}

func codexMaxConcurrencyFromEnv() (int, error) {
	raw := strings.TrimSpace(os.Getenv("CODEX_MAX_CONCURRENCY"))
	if raw == "" {
		return defaultCodexMaxConcurrency, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("read CODEX_MAX_CONCURRENCY failed: %w", err)
	}
	if n <= 0 {
		return 0, fmt.Errorf("invalid CODEX_MAX_CONCURRENCY: %d", n)
	}
	return n, nil
}

func codexLoginCheckFromEnv() bool {
	raw := strings.TrimSpace(os.Getenv("CODEX_LOGIN_CHECK"))
	if raw == "" {
		return true
	}
	switch strings.ToLower(raw) {
	case "1", "true", "yes", "y", "on":
		return true
	case "0", "false", "no", "n", "off":
		return false
	default:
		return true
	}
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
