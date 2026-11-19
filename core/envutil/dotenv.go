package envutil

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type DotenvOptions struct {
	EnvFileKey              string
	RequireStaticSecretsKey string
	DefaultPath             string
	LocalEnableKey          string
	LocalPathKey            string
	LocalPaths              []string
}

type serviceDotenv struct {
	envFileKey              string
	requireStaticSecretsKey string
	defaultPath             string
}

func (opts DotenvOptions) serviceDotenv() (serviceDotenv, bool, error) {
	service := serviceDotenv{
		envFileKey:              strings.TrimSpace(opts.EnvFileKey),
		requireStaticSecretsKey: strings.TrimSpace(opts.RequireStaticSecretsKey),
		defaultPath:             strings.TrimSpace(opts.DefaultPath),
	}

	set := 0

	for _, field := range []string{service.envFileKey, service.requireStaticSecretsKey, service.defaultPath} {
		if field != "" {
			set++
		}
	}

	switch set {
	case 0:
		return serviceDotenv{}, false, nil
	case 3:
		return service, true, nil
	default:
		return serviceDotenv{}, false, errors.New("EnvFileKey, RequireStaticSecretsKey and DefaultPath must be set together")
	}
}

func LoadDotenv(opts DotenvOptions) error {
	service, serviceEnabled, err := opts.serviceDotenv()
	if err != nil {
		return fmt.Errorf("envutil: dotenv options: %w", err)
	}

	localEnabled := true

	if opts.LocalEnableKey != "" {
		enabled, err := BoolE(opts.LocalEnableKey, false)
		if err != nil {
			return fmt.Errorf("envutil: read local dotenv enable flag: %w", err)
		}

		localEnabled = enabled
	}

	if serviceEnabled {
		handled, err := loadServiceDotenv(service)
		if err != nil {
			return fmt.Errorf("envutil: load service dotenv: %w", err)
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
		if err := loadDotenvFile(path, false, false); err != nil {
			return fmt.Errorf("envutil: load dotenv file: %w", err)
		}
	}

	return nil
}

func loadDotenvFile(path string, required, strict bool) error {
	path = strings.TrimSpace(path)
	if path == "" {
		if required {
			return errors.New("dotenv path is empty")
		}

		return nil
	}

	info, err := os.Lstat(path) //nolint:gosec // 운영자가 지정한 dotenv 경로를 로드 전에 검증한다.
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
		if err := loadNonStrictDotenvFile(path); err != nil {
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

func loadServiceDotenv(service serviceDotenv) (bool, error) {
	requireStaticSecrets, err := BoolE(service.requireStaticSecretsKey, false)
	if err != nil {
		return false, fmt.Errorf("read static secrets guard: %w", err)
	}

	if envFile := strings.TrimSpace(os.Getenv(service.envFileKey)); envFile != "" {
		strict := requireStaticSecrets || strings.HasPrefix(envFile, filepath.Dir(service.defaultPath)+string(os.PathSeparator))
		if err := loadDotenvFile(envFile, true, strict); err != nil {
			return true, fmt.Errorf("load dotenv file: %w", err)
		}

		return true, nil
	}

	if requireStaticSecrets {
		if err := loadDotenvFile(service.defaultPath, true, true); err != nil {
			return true, fmt.Errorf("load dotenv file: %w", err)
		}

		return true, nil
	}

	return false, nil
}

func loadNonStrictDotenvFile(path string) error {
	file, err := os.Open(path) //nolint:gosec // 운영자가 지정하고 호출자가 검증한 dotenv 경로다.
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
