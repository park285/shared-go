package envutil

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadDotenvFile(t *testing.T) {
	tests := []struct {
		name      string
		content   string
		setup     func(*testing.T, string)
		required  bool
		strict    bool
		wantEnv   map[string]string
		wantErr   string
		pathEmpty bool
	}{
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
				t.Setenv("EXISTING_KEY", "keep")
			},
			wantEnv: map[string]string{
				"FROM_DOT_ENV":   "value",
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
				if err := os.Chmod(path, 0o644); err != nil {
					t.Fatalf("chmod dotenv: %v", err)
				}
			},
			strict:  true,
			wantErr: "world-accessible",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, ".env")
			if !tt.pathEmpty {
				if err := os.WriteFile(path, []byte(tt.content), 0o600); err != nil {
					t.Fatalf("write dotenv: %v", err)
				}
			} else {
				path = ""
			}
			if tt.setup != nil {
				tt.setup(t, path)
			}
			for key := range tt.wantEnv {
				t.Cleanup(func() { _ = os.Unsetenv(key) })
			}

			err := LoadDotenvFile(path, tt.required, tt.strict)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("LoadDotenvFile() error = %v, want substring %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("LoadDotenvFile() error = %v", err)
			}
			for key, want := range tt.wantEnv {
				if got := os.Getenv(key); got != want {
					t.Fatalf("%s = %q, want %q", key, got, want)
				}
			}
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

func TestLoadDotenv(t *testing.T) {
	tests := []struct {
		name    string
		opts    DotenvOptions
		setup   func(*testing.T, string)
		wantKey string
		wantVal string
		wantErr string
	}{
		{
			name: "chatbotgo local opt in",
			opts: DotenvOptions{
				LocalEnableKey: "CHATBOTGO_LOAD_DOTENV",
				LocalPathKey:   "CHATBOTGO_DOTENV_PATH",
			},
			setup: func(t *testing.T, dir string) {
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
				t.Setenv("TWENTYQ_ENV_FILE", filepath.Join(dir, "missing.env"))
				t.Setenv("TWENTYQ_REQUIRE_OPENBAO", "true")
			},
			wantErr: "stat dotenv file failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Chdir(dir)
			if tt.setup != nil {
				tt.setup(t, dir)
			}

			err := LoadDotenv(tt.opts)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("LoadDotenv() error = %v, want substring %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("LoadDotenv() error = %v", err)
			}
			if got := os.Getenv(tt.wantKey); got != tt.wantVal {
				t.Fatalf("%s = %q, want %q", tt.wantKey, got, tt.wantVal)
			}
		})
	}
}
