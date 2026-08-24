package envutil

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/park285/shared-go/v2/pkg/internal/testsupport"
)

type loadDotenvFileCase struct {
	name      string
	content   string
	setup     func(*testing.T, string)
	required  bool
	strict    bool
	wantEnv   map[string]string
	wantErr   string
	pathEmpty bool
}

func (tc loadDotenvFileCase) run(t *testing.T) {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, ".env")

	if !tc.pathEmpty {
		if err := os.WriteFile(path, []byte(tc.content), 0o600); err != nil {
			t.Fatalf("write dotenv: %v", err)
		}
	} else {
		path = ""
	}

	if tc.setup != nil {
		tc.setup(t, path)
	}

	for key := range tc.wantEnv {
		testsupport.UnsetEnvOnCleanup(t, key)
	}

	err := LoadDotenvFile(path, tc.required, tc.strict)
	if tc.wantErr != "" {
		if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
			t.Fatalf("LoadDotenvFile() error = %v, want substring %q", err, tc.wantErr)
		}

		return
	}

	if err != nil {
		t.Fatalf("LoadDotenvFile() error = %v", err)
	}

	for key, want := range tc.wantEnv {
		if got := os.Getenv(key); got != want {
			t.Fatalf("%s = %q, want %q", key, got, want)
		}
	}
}

func TestLoadDotenvFile(t *testing.T) {
	tests := []loadDotenvFileCase{
		{
			name: "loads supported lines without overriding",
			content: strings.Join([]string{
				"",
				"# comment",
				"FROM_DOT_ENV=value",
				"QUOTED_DOUBLE=\"quoted\"",
				"QUOTED_SINGLE='single-quoted'",
				"export EXPORTED_KEY=exported",
				"EXISTING_KEY=override-attempt",
				"INVALID_LINE",
			}, "\n"),
			setup: func(t *testing.T, _ string) {
				t.Helper()

				t.Setenv("EXISTING_KEY", "keep")
			},
			wantEnv: map[string]string{
				"FROM_DOT_ENV":   testValue,
				"QUOTED_DOUBLE":  "quoted",
				"QUOTED_SINGLE":  "single-quoted",
				"EXPORTED_KEY":   "exported",
				"EXISTING_KEY":   "keep",
				"UNDEFINED_LINE": "",
			},
		},
		{
			name:      "empty required path",
			required:  true,
			wantErr:   "dotenv path is empty",
			pathEmpty: true,
		},
		{
			name:    "strict world accessible rejected",
			content: "STRICT_KEY=value\n",
			setup: func(t *testing.T, path string) {
				t.Helper()

				if err := os.Chmod(path, 0o644); err != nil { //nolint:gosec // 허용적인 권한을 감지하는 동작을 검증하려고 일부러 그 권한을 만든다.
					t.Fatalf("chmod dotenv: %v", err)
				}
			},
			strict:  true,
			wantErr: "world-accessible",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.run(t)
		})
	}
}

func TestLoadDotenvFileMissingOptional(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "missing.env")
	if err := LoadDotenvFile(path, false, false); err != nil {
		t.Fatalf("LoadDotenvFile(optional missing) error = %v", err)
	}
}

func TestServiceDotenvPath(t *testing.T) {
	t.Parallel()

	if got := ServiceDotenvPath("twentyq"); got != "/run/twentyq/twentyq.env" {
		t.Fatalf("ServiceDotenvPath() = %q, want /run/twentyq/twentyq.env", got)
	}
}

type loadDotenvCase struct {
	name    string
	opts    DotenvOptions
	setup   func(*testing.T, string)
	wantKey string
	wantVal string
	wantErr string
}

func (tc loadDotenvCase) run(t *testing.T) {
	t.Helper()

	dir := t.TempDir()
	t.Chdir(dir)

	if tc.setup != nil {
		tc.setup(t, dir)
	}

	err := LoadDotenv(tc.opts)
	if tc.wantErr != "" {
		if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
			t.Fatalf("LoadDotenv() error = %v, want substring %q", err, tc.wantErr)
		}

		return
	}

	if err != nil {
		t.Fatalf("LoadDotenv() error = %v", err)
	}

	if got := os.Getenv(tc.wantKey); got != tc.wantVal {
		t.Fatalf("%s = %q, want %q", tc.wantKey, got, tc.wantVal)
	}
}

func TestLoadDotenv(t *testing.T) {
	tests := []loadDotenvCase{
		{
			name: "chatbotgo local opt in",
			opts: DotenvOptions{
				LocalEnableKey: "CHATBOTGO_LOAD_DOTENV",
				LocalPathKey:   "CHATBOTGO_DOTENV_PATH",
			},
			setup: func(t *testing.T, dir string) {
				t.Helper()

				path := filepath.Join(dir, "local.env")
				if err := os.WriteFile(path, []byte("LOCAL_ONLY=debug\n"), 0o600); err != nil {
					t.Fatalf("write local dotenv: %v", err)
				}

				t.Setenv("CHATBOTGO_LOAD_DOTENV", "true")
				t.Setenv("CHATBOTGO_DOTENV_PATH", path)
			},
			wantKey: "LOCAL_ONLY",
			wantVal: "debug",
		},
		{
			name: "local disabled",
			opts: DotenvOptions{
				LocalEnableKey: "CHATBOTGO_LOAD_DOTENV",
				LocalPaths:     []string{".env"},
			},
			setup: func(t *testing.T, dir string) {
				t.Helper()

				if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("DISABLED_LOCAL=1\n"), 0o600); err != nil {
					t.Fatalf("write local dotenv: %v", err)
				}

				t.Setenv("CHATBOTGO_LOAD_DOTENV", "false")
			},
			wantKey: "DISABLED_LOCAL",
			wantVal: "",
		},
		{
			name: "service explicit env file",
			opts: DotenvOptions{ServiceName: "twentyq"},
			setup: func(t *testing.T, dir string) {
				t.Helper()

				path := filepath.Join(dir, "twentyq.env")
				if err := os.WriteFile(path, []byte("SERVICE_KEY=ok\n"), 0o600); err != nil {
					t.Fatalf("write service dotenv: %v", err)
				}

				t.Setenv("TWENTYQ_ENV_FILE", path)
			},
			wantKey: "SERVICE_KEY",
			wantVal: "ok",
		},
		{
			name: "service required missing",
			opts: DotenvOptions{ServiceName: "twentyq"},
			setup: func(t *testing.T, dir string) {
				t.Helper()

				t.Setenv("TWENTYQ_ENV_FILE", filepath.Join(dir, "missing.env"))
				t.Setenv("TWENTYQ_REQUIRE_STATIC_SECRETS", "true")
			},
			wantErr: "stat dotenv file failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.run(t)
		})
	}
}
