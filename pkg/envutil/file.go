package envutil

import (
	"log/slog"
	"os"
	"strings"
)

func StringOrFile(key, def string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}

	filePath := strings.TrimSpace(os.Getenv(key + "_FILE"))
	if filePath == "" {
		return def
	}

	//nolint:gosec // *_FILE env vars are intentional operator-supplied secret file paths.
	data, err := os.ReadFile(filePath)
	if err != nil {
		slog.Warn("failed to read secret file for environment variable",
			"key", key,
			"path", filePath,
			"error", err.Error())
		return def
	}

	warnIfWorldAccessible(key, filePath)

	if value := strings.TrimSpace(string(data)); value != "" {
		return value
	}

	return def
}

// 0o007 group/other 권한 중 other 비트가 켜져 있으면 경고합니다.
// 0o640(group-read)은 OpenBao agent 렌더링의 정상 상태이므로 경고하지 않습니다.
func warnIfWorldAccessible(key, filePath string) {
	//nolint:gosec // *_FILE env vars are intentional operator-supplied secret file paths.
	info, err := os.Stat(filePath)
	if err != nil {
		return
	}
	if info.Mode().Perm()&0o007 != 0 {
		slog.Warn("secret file for environment variable is world-accessible",
			"key", key,
			"path", filePath,
			"mode", info.Mode().Perm().String())
	}
}
