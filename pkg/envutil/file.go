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

// StringOrSecretFile은 key의 평문 값 또는 key_FILE이 가리키는 비밀 파일 값을 읽는다.
// 둘 다 비어 있으면 빈 문자열을 반환한다. 둘 다 설정되면 어느 값이 쓰였는지 운영에서 알 수 없으므로
// 오류를 반환한다(비밀을 파일로 옮기는 중 남은 평문 env가 조용히 쓰이지 않게 한다).
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
