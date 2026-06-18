package llm

import (
	"errors"
	"fmt"
	"os"
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
