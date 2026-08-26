package envutil

import (
	"fmt"
	"io"
	"os"
	"strings"
)

// no-follow 파일 열기를 지원하지 않는 플랫폼에서는 오류를 반환합니다.
func secretFile(key string) (string, error) {
	filePath := strings.TrimSpace(os.Getenv(key + "_FILE"))
	if filePath == "" {
		return "", fmt.Errorf("%s_FILE is required", key)
	}

	//nolint:gosec // *_FILE env vars are intentional operator-supplied secret file paths.
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

func StringOrSecretFile(key, def string) (string, error) {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value, nil
	}

	if strings.TrimSpace(os.Getenv(key+"_FILE")) != "" {
		out, err := secretFile(key)
		if err != nil {
			return out, fmt.Errorf("secret file: %w", err)
		}

		return out, nil
	}

	return def, nil
}

func validateSecretFileMode(key, filePath string, perm os.FileMode) error {
	if perm&0o400 == 0 || perm&0o137 != 0 {
		return fmt.Errorf("%s_FILE path %q has insecure permissions %s", key, filePath, perm.String())
	}

	return nil
}
