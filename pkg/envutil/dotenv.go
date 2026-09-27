package envutil

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DotenvOptions는 service-prefix와 local dotenv 로딩 규칙을 지정한다.
type DotenvOptions struct {
	ServiceName    string
	LocalEnableKey string
	LocalPathKey   string
	LocalPaths     []string
}

// ServiceDotenvPath는 OpenBao Agent dotenv 렌더링 기본 경로를 반환한다.
func ServiceDotenvPath(serviceName string) string {
	name := strings.Trim(strings.ToLower(strings.TrimSpace(serviceName)), "/")
	if name == "" {
		return ""
	}

	return filepath.Join("/run", name, name+".env")
}

// LoadDotenv는 service env file과 local dotenv 후보를 옵션에 맞춰 로드한다.
// LocalEnableKey와 <PREFIX>_REQUIRE_STATIC_SECRETS는 BoolE 규칙으로 읽으며, 빈 값은 미설정(false)이고
// 받아들일 수 없는 값은 어떤 파일도 읽기 전에 설정 오류로 반환한다.
func LoadDotenv(opts DotenvOptions) error {
	// service env file이 처리되어 local 단계에 이르지 않는 경우에도 받아들일 수 없는 값을 드러내도록
	// service 단계보다 먼저 판정한다. service 단계가 처리하지 않았다면 아무 파일도 읽지 않았으므로 값은 같다.
	localEnabled := true

	if opts.LocalEnableKey != "" {
		enabled, err := BoolE(opts.LocalEnableKey, false)
		if err != nil {
			return fmt.Errorf("read local dotenv enable flag: %w", err)
		}

		localEnabled = enabled
	}

	serviceName := strings.TrimSpace(opts.ServiceName)
	if serviceName != "" {
		handled, err := loadServiceDotenv(serviceName)
		if err != nil {
			return fmt.Errorf("load service dotenv: %w", err)
		}

		if handled {
			return nil
		}
	}

	if !localEnabled {
		return nil
	}

	paths := opts.LocalPaths
	if opts.LocalPathKey != "" {
		if path := strings.TrimSpace(os.Getenv(opts.LocalPathKey)); path != "" {
			paths = []string{path}
		}
	}

	if len(paths) == 0 {
		paths = []string{".env"}
	}

	for _, path := range paths {
		if err := LoadDotenvFile(path, false, false); err != nil {
			return fmt.Errorf("load dotenv file: %w", err)
		}
	}

	return nil
}

// LoadDotenvFile는 dotenv 파일을 로드하며 strict 모드에서 symlink, non-regular file, world-accessible 파일을 거부한다.
// 전체 줄 주석(#로 시작)만 인식하며 따옴표 없는 값의 inline #는 주석이 아니라 값의 일부로 남는다.
func LoadDotenvFile(path string, required, strict bool) error {
	path = strings.TrimSpace(path)
	if path == "" {
		if required {
			return errors.New("dotenv path is empty")
		}

		return nil
	}

	info, err := os.Lstat(path) //nolint:gosec // dotenv path is operator supplied and validated before loading.
	if err != nil {
		if errors.Is(err, os.ErrNotExist) && !required {
			return nil
		}

		return fmt.Errorf("stat dotenv file failed path=%s: %w", path, err)
	}

	if info.IsDir() {
		return fmt.Errorf("dotenv path is directory path=%s", path)
	}

	if !strict {
		if err := loadDotenvFile(path); err != nil {
			return fmt.Errorf("load dotenv file: %w", err)
		}

		return nil
	}

	if err := checkStrictDotenvMode(path, info); err != nil {
		return fmt.Errorf("check strict dotenv mode: %w", err)
	}

	if err := loadStrictDotenvFile(path, info); err != nil {
		return fmt.Errorf("load strict dotenv file: %w", err)
	}

	return nil
}

func checkStrictDotenvMode(path string, info os.FileInfo) error {
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("dotenv file must not be symlink path=%s", path)
	}

	if !info.Mode().IsRegular() {
		return fmt.Errorf("dotenv file must be regular file path=%s mode=%s", path, info.Mode().String())
	}

	if info.Mode().Perm()&0o007 != 0 {
		return fmt.Errorf("dotenv file is world-accessible path=%s mode=%s", path, info.Mode().Perm().String())
	}

	return nil
}

func loadStrictDotenvFile(path string, info os.FileInfo) error {
	file, err := openSecretFileNoFollow(path)
	if err != nil {
		return fmt.Errorf("open dotenv file %s: %w", path, err)
	}

	openedInfo, statErr := file.Stat()
	if statErr != nil {
		_ = file.Close()
		return fmt.Errorf("stat dotenv file failed path=%s: %w", path, statErr)
	}

	if err := checkStrictDotenvMode(path, openedInfo); err != nil {
		_ = file.Close()
		return fmt.Errorf("check strict dotenv mode: %w", err)
	}

	if !os.SameFile(info, openedInfo) {
		_ = file.Close()
		return fmt.Errorf("dotenv file changed while opening path=%s", path)
	}

	if err := scanDotenvFile(file, path); err != nil {
		return fmt.Errorf("scan dotenv file: %w", err)
	}

	return nil
}

func loadServiceDotenv(serviceName string) (bool, error) {
	prefix := serviceEnvPrefix(serviceName)

	requireStaticSecrets, err := BoolE(prefix+"_REQUIRE_STATIC_SECRETS", false)
	if err != nil {
		return false, fmt.Errorf("read static secrets guard: %w", err)
	}

	defaultPath := ServiceDotenvPath(serviceName)

	if envFile := strings.TrimSpace(os.Getenv(prefix + "_ENV_FILE")); envFile != "" {
		strict := requireStaticSecrets || strings.HasPrefix(envFile, defaultPathDir(defaultPath)+string(os.PathSeparator))
		if err := LoadDotenvFile(envFile, true, strict); err != nil {
			return true, fmt.Errorf("load dotenv file: %w", err)
		}

		return true, nil
	}

	if requireStaticSecrets {
		if err := LoadDotenvFile(defaultPath, true, true); err != nil {
			return true, fmt.Errorf("load dotenv file: %w", err)
		}

		return true, nil
	}

	return false, nil
}

func loadDotenvFile(path string) error {
	file, err := os.Open(path) //nolint:gosec // dotenv path is operator supplied and validated by caller.
	if err != nil {
		return fmt.Errorf("open dotenv file %s: %w", path, err)
	}

	if err := scanDotenvFile(file, path); err != nil {
		return fmt.Errorf("scan dotenv file: %w", err)
	}

	return nil
}

func scanDotenvFile(file *os.File, path string) error {
	scanner := bufio.NewScanner(file)

	var scanErr error

	for scanner.Scan() {
		if err := applyDotenvLine(strings.TrimSpace(scanner.Text()), path); err != nil {
			scanErr = fmt.Errorf("apply dotenv line from %s: %w", path, err)
			break
		}
	}

	if scanErr == nil {
		if err := scanner.Err(); err != nil {
			scanErr = fmt.Errorf("scan dotenv file %s: %w", path, err)
		}
	}

	if err := file.Close(); err != nil && scanErr == nil {
		return fmt.Errorf("close dotenv file %s: %w", path, err)
	}

	return scanErr
}

func applyDotenvLine(line, path string) error {
	if line == "" || strings.HasPrefix(line, "#") {
		return nil
	}

	if after, ok := strings.CutPrefix(line, "export "); ok {
		line = strings.TrimSpace(after)
	}

	key, value, ok := strings.Cut(line, "=")
	if !ok {
		return nil
	}

	key = strings.TrimSpace(key)
	if key == "" {
		return nil
	}

	if _, exists := os.LookupEnv(key); exists {
		return nil
	}

	if err := os.Setenv(key, trimDotenvValue(strings.TrimSpace(value))); err != nil {
		return fmt.Errorf("set env %s from %s: %w", key, path, err)
	}

	return nil
}

func trimDotenvValue(value string) string {
	if len(value) >= 2 {
		if (value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'') {
			return value[1 : len(value)-1]
		}
	}

	return value
}

func serviceEnvPrefix(serviceName string) string {
	replacer := strings.NewReplacer("-", "_", ".", "_", "/", "_")
	return strings.ToUpper(replacer.Replace(strings.TrimSpace(serviceName)))
}

func defaultPathDir(path string) string {
	if path == "" {
		return ""
	}

	return filepath.Dir(path)
}
