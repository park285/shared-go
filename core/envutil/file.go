package envutil

import (
	"fmt"
	"io"
	"os"
	"strings"
)

func secretFile(key string) (string, error) {
	filePath := strings.TrimSpace(os.Getenv(key + "_FILE"))
	if filePath == "" {
		return "", fmt.Errorf("%s_FILE is required", key)
	}

	//nolint:gosec // *_FILE 환경 변수는 운영자가 지정하는 비밀 파일 경로다.
	info, err := os.Lstat(filePath)
	if err != nil {
		return "", fmt.Errorf("%s_FILE path %q is not readable: %w", key, filePath, err)
	}

	if info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("%s_FILE path %q must not be a symlink", key, filePath)
	}

	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("%s_FILE path %q must be a regular file", key, filePath)
	}

	if modeErr := validateSecretFileMode(key, filePath, info.Mode().Perm()); modeErr != nil {
		return "", fmt.Errorf("validate secret file mode: %w", modeErr)
	}

	file, err := openSecretFileNoFollow(filePath)
	if err != nil {
		return "", fmt.Errorf("%s_FILE path %q is not readable: %w", key, filePath, err)
	}

	defer func() { _ = file.Close() }()

	openedInfo, err := file.Stat()
	if err != nil {
		return "", fmt.Errorf("%s_FILE path %q cannot be inspected: %w", key, filePath, err)
	}

	if !openedInfo.Mode().IsRegular() {
		return "", fmt.Errorf("%s_FILE path %q must be a regular file", key, filePath)
	}

	if !os.SameFile(info, openedInfo) {
		return "", fmt.Errorf("%s_FILE path %q changed while opening", key, filePath)
	}

	if modeErr := validateSecretFileMode(key, filePath, openedInfo.Mode().Perm()); modeErr != nil {
		return "", fmt.Errorf("validate secret file mode: %w", modeErr)
	}

	data, err := io.ReadAll(file)
	if err != nil {
		return "", fmt.Errorf("%s_FILE path %q cannot be read: %w", key, filePath, err)
	}

	value := strings.TrimSpace(string(data))
	if value == "" {
		return "", fmt.Errorf("%s_FILE path %q is empty", key, filePath)
	}

	return value, nil
}

func StringOrSecretFile(key string) (string, error) {
	value := strings.TrimSpace(os.Getenv(key))
	fileSet := strings.TrimSpace(os.Getenv(key+"_FILE")) != ""

	switch {
	case value != "" && fileSet:
		return "", fmt.Errorf("envutil: %s and %s_FILE are both set", key, key)
	case value != "":
		return value, nil
	case fileSet:
		out, err := secretFile(key)
		if err != nil {
			return out, fmt.Errorf("envutil: secret file: %w", err)
		}

		return out, nil
	default:
		return "", nil
	}
}

func validateSecretFileMode(key, filePath string, perm os.FileMode) error {
	if perm&0o400 == 0 || perm&0o137 != 0 {
		return fmt.Errorf("%s_FILE path %q has insecure permissions %s", key, filePath, perm.String())
	}

	return nil
}
