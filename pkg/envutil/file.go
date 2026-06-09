package envutil

import (
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
		return def
	}

	if value := strings.TrimSpace(string(data)); value != "" {
		return value
	}

	return def
}
