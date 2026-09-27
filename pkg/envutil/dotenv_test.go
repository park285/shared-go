package envutil

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
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
	path := filepath.Join(dir, testDotenvFileName)

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

	if got := ServiceDotenvPath(testServiceTwentyq); got != "/run/twentyq/twentyq.env" {
		t.Fatalf("ServiceDotenvPath() = %q, want /run/twentyq/twentyq.env", got)
	}
}

type loadDotenvCase struct {
	name      string
	opts      DotenvOptions
	setup     func(*testing.T, string)
	wantKey   string
	wantVal   string
	wantErr   string
	wantErrIs error
}

func (tc loadDotenvCase) run(t *testing.T) {
	t.Helper()

	dir := t.TempDir()
	t.Chdir(dir)

	if tc.wantKey != "" {
		testsupport.UnsetEnvOnCleanup(t, tc.wantKey)
	}

	if tc.setup != nil {
		tc.setup(t, dir)
	}

	err := LoadDotenv(tc.opts)
	if tc.wantErr != "" {
		tc.assertFailure(t, err)

		return
	}

	if err != nil {
		t.Fatalf("LoadDotenv() error = %v", err)
	}

	if got := os.Getenv(tc.wantKey); got != tc.wantVal {
		t.Fatalf("%s = %q, want %q", tc.wantKey, got, tc.wantVal)
	}
}

func (tc loadDotenvCase) assertFailure(t *testing.T, err error) {
	t.Helper()

	if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
		t.Fatalf("LoadDotenv() error = %v, want substring %q", err, tc.wantErr)
	}

	if tc.wantErrIs != nil && !errors.Is(err, tc.wantErrIs) {
		t.Fatalf("LoadDotenv() error = %v, want errors.Is %v", err, tc.wantErrIs)
	}

	if tc.wantKey == "" {
		return
	}

	// 설정 오류로 멈춘 경우 어떤 dotenv 파일도 적용되지 않아야 한다.
	if got, ok := os.LookupEnv(tc.wantKey); ok {
		t.Fatalf("%s = %q after LoadDotenv() error, want unset", tc.wantKey, got)
	}
}

func TestLoadDotenv(t *testing.T) {
	tests := []loadDotenvCase{
		{
			name: "chatbotgo local opt in",
			opts: DotenvOptions{
				LocalEnableKey: testChatbotgoLoadDotenv,
				LocalPathKey:   "CHATBOTGO_DOTENV_PATH",
			},
			setup: func(t *testing.T, dir string) {
				t.Helper()

				path := filepath.Join(dir, "local.env")
				if err := os.WriteFile(path, []byte("LOCAL_ONLY=debug\n"), 0o600); err != nil {
					t.Fatalf("write local dotenv: %v", err)
				}

				t.Setenv(testChatbotgoLoadDotenv, "true")
				t.Setenv("CHATBOTGO_DOTENV_PATH", path)
			},
			wantKey: "LOCAL_ONLY",
			wantVal: "debug",
		},
		{
			name: "local disabled",
			opts: DotenvOptions{
				LocalEnableKey: testChatbotgoLoadDotenv,
				LocalPaths:     []string{testDotenvFileName},
			},
			setup: func(t *testing.T, dir string) {
				t.Helper()

				if err := os.WriteFile(filepath.Join(dir, testDotenvFileName), []byte("DISABLED_LOCAL=1\n"), 0o600); err != nil {
					t.Fatalf("write local dotenv: %v", err)
				}

				t.Setenv(testChatbotgoLoadDotenv, "false")
			},
			wantKey: "DISABLED_LOCAL",
			wantVal: "",
		},
		{
			name: "service explicit env file",
			opts: DotenvOptions{ServiceName: testServiceTwentyq},
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
			opts: DotenvOptions{ServiceName: testServiceTwentyq},
			setup: func(t *testing.T, dir string) {
				t.Helper()

				t.Setenv("TWENTYQ_ENV_FILE", filepath.Join(dir, "missing.env"))
				t.Setenv(testRequireStaticSecrets, "true")
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

func writeDotenvFile(t *testing.T, path, content string, perm os.FileMode) {
	t.Helper()

	if err := os.WriteFile(path, []byte(content), perm); err != nil {
		t.Fatalf("write dotenv %s: %v", path, err)
	}

	// umask와 관계없이 의도한 권한을 고정한다.
	if err := os.Chmod(path, perm); err != nil {
		t.Fatalf("chmod dotenv %s: %v", path, err)
	}
}

// LocalEnableKey는 BoolE와 같은 규칙으로 판정하고, 받아들일 수 없는 값은 기본값(false)으로 접지 않고
// 설정 오류로 돌려준다(DEC-20260927-shared-go-dotenv-bool-strict).
func TestLoadDotenvLocalEnableFlag(t *testing.T) {
	localOpts := DotenvOptions{
		LocalEnableKey: testChatbotgoLoadDotenv,
		LocalPaths:     []string{testDotenvFileName},
	}

	tests := []loadDotenvCase{
		{
			name: "unacceptable value fails",
			opts: localOpts,
			setup: func(t *testing.T, dir string) {
				t.Helper()

				writeDotenvFile(t, filepath.Join(dir, testDotenvFileName), "LOCAL_FLAG_INVALID=loaded\n", 0o600)
				t.Setenv(testChatbotgoLoadDotenv, "enabled")
			},
			wantKey:   "LOCAL_FLAG_INVALID",
			wantErr:   "invalid bool env " + testChatbotgoLoadDotenv,
			wantErrIs: strconv.ErrSyntax,
		},
		{
			name: "accepts BoolE true spelling",
			opts: localOpts,
			setup: func(t *testing.T, dir string) {
				t.Helper()

				writeDotenvFile(t, filepath.Join(dir, testDotenvFileName), "LOCAL_FLAG_ON=loaded\n", 0o600)
				t.Setenv(testChatbotgoLoadDotenv, " On ")
			},
			wantKey: "LOCAL_FLAG_ON",
			wantVal: "loaded",
		},
		{
			name: "whitespace only stays unset",
			opts: localOpts,
			setup: func(t *testing.T, dir string) {
				t.Helper()

				writeDotenvFile(t, filepath.Join(dir, testDotenvFileName), "LOCAL_FLAG_BLANK=loaded\n", 0o600)
				t.Setenv(testChatbotgoLoadDotenv, "   ")
			},
			wantKey: "LOCAL_FLAG_BLANK",
			wantVal: "",
		},
		{
			// service env file이 처리되는 경로에서도 LocalEnableKey 값을 먼저 판정해야 한다.
			name: "unacceptable value fails before service env file",
			opts: DotenvOptions{ServiceName: testServiceTwentyq, LocalEnableKey: testChatbotgoLoadDotenv},
			setup: func(t *testing.T, dir string) {
				t.Helper()

				path := filepath.Join(dir, "twentyq.env")
				writeDotenvFile(t, path, "LOCAL_FLAG_SERVICE_INVALID=loaded\n", 0o600)
				t.Setenv("TWENTYQ_ENV_FILE", path)
				t.Setenv(testChatbotgoLoadDotenv, "enabled")
			},
			wantKey:   "LOCAL_FLAG_SERVICE_INVALID",
			wantErr:   "invalid bool env " + testChatbotgoLoadDotenv,
			wantErrIs: strconv.ErrSyntax,
		},
		{
			// 받아들일 수 있는 false는 service env file 적용을 막지 않는다.
			name: "accepted false keeps service env file",
			opts: DotenvOptions{ServiceName: testServiceTwentyq, LocalEnableKey: testChatbotgoLoadDotenv},
			setup: func(t *testing.T, dir string) {
				t.Helper()

				path := filepath.Join(dir, "twentyq.env")
				writeDotenvFile(t, path, "LOCAL_FLAG_SERVICE_OFF=loaded\n", 0o600)
				t.Setenv("TWENTYQ_ENV_FILE", path)
				t.Setenv(testChatbotgoLoadDotenv, "off")
			},
			wantKey: "LOCAL_FLAG_SERVICE_OFF",
			wantVal: "loaded",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.run(t)
		})
	}
}

// <PREFIX>_REQUIRE_STATIC_SECRETS도 BoolE 규칙으로 판정한다. 오타 값이 false로 접히면
// local dotenv로 넘어가거나 world-readable env file을 strict 검사 없이 적용하게 된다.
func TestLoadDotenvStaticSecretsGuard(t *testing.T) {
	tests := []loadDotenvCase{
		{
			name: "unacceptable value fails before local dotenv",
			opts: DotenvOptions{ServiceName: testServiceTwentyq, LocalPaths: []string{testDotenvFileName}},
			setup: func(t *testing.T, dir string) {
				t.Helper()

				writeDotenvFile(t, filepath.Join(dir, testDotenvFileName), "SERVICE_FLAG_LOCAL=loaded\n", 0o600)
				t.Setenv(testRequireStaticSecrets, "maybe")
			},
			wantKey:   "SERVICE_FLAG_LOCAL",
			wantErr:   "invalid bool env " + testRequireStaticSecrets,
			wantErrIs: strconv.ErrSyntax,
		},
		{
			name: "unacceptable value fails before explicit env file",
			opts: DotenvOptions{ServiceName: testServiceTwentyq},
			setup: func(t *testing.T, dir string) {
				t.Helper()

				path := filepath.Join(dir, "twentyq.env")
				writeDotenvFile(t, path, "SERVICE_FLAG_FILE=loaded\n", 0o644)
				t.Setenv("TWENTYQ_ENV_FILE", path)
				t.Setenv(testRequireStaticSecrets, "ture")
			},
			wantKey:   "SERVICE_FLAG_FILE",
			wantErr:   "invalid bool env " + testRequireStaticSecrets,
			wantErrIs: strconv.ErrSyntax,
		},
		{
			name: "accepted true enforces strict env file",
			opts: DotenvOptions{ServiceName: testServiceTwentyq},
			setup: func(t *testing.T, dir string) {
				t.Helper()

				path := filepath.Join(dir, "twentyq.env")
				writeDotenvFile(t, path, "SERVICE_FLAG_STRICT=loaded\n", 0o644)
				t.Setenv("TWENTYQ_ENV_FILE", path)
				t.Setenv(testRequireStaticSecrets, "YES")
			},
			wantKey: "SERVICE_FLAG_STRICT",
			wantErr: "world-accessible",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.run(t)
		})
	}
}
