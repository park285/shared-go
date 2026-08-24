package llm

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

const (
	DefaultDiagnosticLimit = 4096
	truncatedMarker        = "...[truncated]"
)

var (
	envSecretRegex  = regexp.MustCompile(`(?i)\b((?:OPENAI|CODEX|ANTHROPIC|GEMINI|GOOGLE)_?(?:API_KEY|ACCESS_TOKEN))\s*=\s*([^\s]+)`)
	bearerRegex     = regexp.MustCompile(`(?i)\b(bearer\s+)[A-Za-z0-9._~+/=-]+`)
	jsonSecretRegex = regexp.MustCompile(`(?i)("?(?:openai|codex|anthropic|gemini|google)_?(?:api_key|access_token)"?\s*[:=]\s*"?)[^",\s]+`)
)

func RedactDiagnostic(text string, limit int) string {
	if limit <= 0 {
		limit = DefaultDiagnosticLimit
	}

	redacted := envSecretRegex.ReplaceAllString(text, "${1}=***REDACTED***")

	redacted = jsonSecretRegex.ReplaceAllString(redacted, "${1}***REDACTED***")
	redacted = bearerRegex.ReplaceAllString(redacted, "${1}***REDACTED***")

	return truncateDiagnostic(redacted, limit)
}

func truncateDiagnostic(text string, limit int) string {
	if len(text) <= limit {
		return text
	}

	cut := limit
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}

	return strings.TrimRight(text[:cut], "\x00") + truncatedMarker
}
