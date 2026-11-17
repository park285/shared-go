// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package logging

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
)

var (
	bearerTokenRegex   = regexp.MustCompile(`(?i)\b(bearer\s+)[A-Za-z0-9._~+/=-]+`)
	querySecretRegex   = regexp.MustCompile(`(?i)([?&;](?:key|api_key|apikey|token|password|pwd|passwd|client_secret|secret|private_key|secret_key)=)[^&\s]+`)
	credentialURLRegex = regexp.MustCompile(`(?i)\b([a-z][a-z0-9+.-]*://)[^/@\s]+@`)
)

const (
	redactedValue      = "***REDACTED***"
	tokenAPIKey        = "api_key"
	tokenAPIKeyCompact = "apikey"
	tokenPasswd        = "passwd"
	tokenPassword      = "password"
	tokenPrivateKey    = "private_key"
	tokenPwd           = "pwd"
	tokenSecret        = "secret"
	tokenSecretKey     = "secret_key"
)

// querySecretTokens는 querySecretRegex가 매치할 수 있는 키 이름 집합으로,
// 정규식 실행 전 싼 substring pre-check 게이트에 쓰인다. 정규식이 매치하는
// 입력은 반드시 이 토큰 중 하나를 case-insensitive로 포함하므로 게이트는 안전하다.
var querySecretTokens = []string{
	"key", tokenAPIKey, tokenAPIKeyCompact, "token", tokenPassword, tokenPwd, tokenPasswd,
	"client_secret", tokenSecret, tokenPrivateKey, tokenSecretKey,
}

var sensitiveExactKeys = map[string]struct{}{
	"token":               {},
	"bot_token":           {},
	"access_token":        {},
	"refresh_token":       {},
	tokenPassword:         {},
	tokenPwd:              {},
	tokenPasswd:           {},
	tokenSecret:           {},
	"client_secret":       {},
	tokenAPIKey:           {},
	tokenAPIKeyCompact:    {},
	tokenPrivateKey:       {},
	tokenSecretKey:        {},
	"authorization":       {},
	"proxy_authorization": {},
	"auth_header":         {},
	"cookie":              {},
	"webhook_url":         {},
	"database_url":        {},
	"postgres_dsn":        {},
	"connection_url":      {},
}

// privacyKeys는 서비스가 Options.PrivacyKeys로 넘긴 업무 식별자 키(정규화된 정확 일치)다.
// 어떤 키가 개인 식별자인지는 서비스 어휘라 라이브러리는 privacy 키를 내장하지 않고 credential 규칙만
// 소유한다. *_id suffix 규칙은 두지 않는다. 더하면 channel_id 같은 공개 콘텐츠 ID까지 가려진다.
// 값이 nil이면 credential 규칙만 적용한다.
type privacyKeys map[string]struct{}

// newPrivacyKeys는 서비스가 넘긴 privacy 키를 credential 키와 같은 규칙으로 정규화한다.
// 빈 키는 설정 실수이므로 거부한다.
func newPrivacyKeys(keys []string) (privacyKeys, error) {
	out := make(privacyKeys, len(keys))

	for _, key := range keys {
		normalized := normalizeSensitiveKey(key)
		if normalized == "" {
			return nil, errors.New("logging: privacy key must not be empty")
		}

		out[normalized] = struct{}{}
	}

	return out, nil
}

func mightContainBearer(s string) bool {
	return containsFold(s, "bearer")
}

func mightContainQuerySecret(s string) bool {
	if !strings.ContainsAny(s, "?&;") || !strings.Contains(s, "=") {
		return false
	}

	for _, tok := range querySecretTokens {
		if containsFold(s, tok) {
			return true
		}
	}

	return false
}

// containsFold는 ASCII-only 입력에서만 고정 폭 윈도우로 substr를 fold-검색한다.
// (?i) 정규식·EqualFold는 ſ(U+017F)↔s, K(U+212A)↔k처럼 토큰과 바이트 폭이 다른
// 멀티바이트 룬을 fold-equivalent로 보므로, non-ASCII 바이트가 있으면 윈도우가
// 정렬되지 않아 superset 불변식이 깨진다. 이 경우 true를 반환해 정규식이 직접
// 판정하도록 위임한다(게이트는 정규식 매치의 superset이어야 한다).
func containsFold(s, substr string) bool {
	if len(substr) > len(s) {
		// 짧은 입력이라도 non-ASCII가 있으면 게이트를 통과시켜 정규식에 위임한다.
		return hasNonASCII(s)
	}

	for i := range len(s) {
		if s[i] >= 0x80 {
			return true
		}

		if i+len(substr) <= len(s) && strings.EqualFold(s[i:i+len(substr)], substr) {
			return true
		}
	}

	return false
}

func hasNonASCII(s string) bool {
	for i := range len(s) {
		if s[i] >= 0x80 {
			return true
		}
	}

	return false
}

func redactSecrets(s string) string {
	return RedactDiagnostic(s)
}

func mightContainCredentialURL(s string) bool {
	return strings.Contains(s, "://") && strings.Contains(s, "@")
}

// RedactDiagnostic은 로그나 stderr에 출력할 진단 문자열에서 credential을 마스킹한다.
func RedactDiagnostic(s string) string {
	if mightContainBearer(s) {
		s = bearerTokenRegex.ReplaceAllString(s, "${1}***REDACTED***")
	}

	if mightContainQuerySecret(s) {
		s = querySecretRegex.ReplaceAllString(s, "${1}***REDACTED***")
	}

	if mightContainCredentialURL(s) {
		s = credentialURLRegex.ReplaceAllString(s, "${1}***REDACTED***@")
	}

	return redactSecretAssignments(s)
}

func redactSecretAssignments(s string) string {
	var redacted strings.Builder

	lastWritten := 0

	for separator := 0; separator < len(s); separator++ {
		if s[separator] != ':' && s[separator] != '=' {
			continue
		}

		normalizedKey := normalizeSensitiveKey(assignmentKeyBefore(s, separator))
		if !isSensitiveNormalizedKey(normalizedKey) {
			continue
		}

		var valueStart, valueEnd int

		if isHeaderCredentialNormalizedKey(normalizedKey) {
			valueStart, valueEnd = headerCredentialValueAfter(s, separator+1)
		} else {
			valueStart, valueEnd = assignmentValueAfter(s, separator+1)
		}

		if valueStart == valueEnd {
			continue
		}

		if redacted.Cap() == 0 {
			redacted.Grow(len(s))
		}

		redacted.WriteString(s[lastWritten:valueStart])
		redacted.WriteString(redactedValue)

		lastWritten = valueEnd
		separator = valueEnd - 1
	}

	if redacted.Cap() == 0 {
		return s
	}

	redacted.WriteString(s[lastWritten:])

	return redacted.String()
}

func assignmentKeyBefore(s string, separator int) string {
	end := separator
	for end > 0 && (s[end-1] == ' ' || s[end-1] == '\t') {
		end--
	}

	if end > 0 && s[end-1] == '"' {
		end--
	}

	start := end
	for start > 0 && isAssignmentKeyByte(s[start-1]) {
		start--
	}

	return s[start:end]
}

func assignmentValueAfter(s string, start int) (int, int) {
	for start < len(s) && (s[start] == ' ' || s[start] == '\t') {
		start++
	}

	if start == len(s) {
		return start, start
	}

	if s[start] == '"' || s[start] == '\'' {
		quote := s[start]
		for end := start + 1; end < len(s); end++ {
			if s[end] == '\\' && end+1 < len(s) {
				end++
				continue
			}

			if s[end] == quote {
				return start, end + 1
			}
		}

		return start, len(s)
	}

	end := start
	for end < len(s) && !strings.ContainsRune(" \t\r\n,;&", rune(s[end])) {
		end++
	}

	return start, end
}

func headerCredentialValueAfter(s string, start int) (int, int) {
	valueStart, valueEnd := assignmentValueAfter(s, start)
	if valueStart == valueEnd || s[valueStart] == '"' || s[valueStart] == '\'' {
		return valueStart, valueEnd
	}

	// 헤더 자격 증명 값은 공백·쉼표·세미콜론을 담을 수 있어 따옴표 없는 값은 레코드 경계(CR/LF)에서만 끝난다.
	for valueEnd < len(s) && s[valueEnd] != '\r' && s[valueEnd] != '\n' {
		valueEnd++
	}

	return valueStart, valueEnd
}

func isHeaderCredentialNormalizedKey(normalized string) bool {
	switch normalized {
	case "authorization", "proxy_authorization", "auth_header", "cookie":
		return true
	default:
		return false
	}
}

func isAssignmentKeyByte(value byte) bool {
	return value >= 'a' && value <= 'z' ||
		value >= 'A' && value <= 'Z' ||
		value >= '0' && value <= '9' ||
		value == '_' || value == '-' || value == '.'
}

func (h *sanitizeHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

func (h *sanitizeHandler) Handle(ctx context.Context, record slog.Record) error {
	msg := redactSecrets(record.Message)
	changed := msg != record.Message

	if h.blockedGroup != "" {
		blocked := slog.NewRecord(record.Time, record.Level, msg, record.PC)
		blocked.AddAttrs(slog.String(h.blockedGroup, blockedDepth))

		if err := h.inner.Handle(ctx, blocked); err != nil {
			return fmt.Errorf("logging: handle: %w", err)
		}

		return nil
	}

	// 변경 감지 패스가 처음 바뀐 attr의 위치와 정제 결과를 남긴다. 그 앞의 attr들은
	// changed=false로 판정됐으므로 정제해도 원본과 같은 값이라 재구축 때 원본을 그대로 쓴다.
	firstChangedIndex := -1

	var firstChangedAttr slog.Attr

	if !changed && !h.inMaskedGroup {
		index := 0

		record.Attrs(func(attr slog.Attr) bool {
			out, attrChanged := h.privacy.sanitizeAttrDepth(attr, h.groupDepth)
			if attrChanged {
				changed = true
				firstChangedIndex = index
				firstChangedAttr = out

				return false
			}

			index++

			return true
		})
	}

	if !changed && !h.inMaskedGroup {
		if err := h.inner.Handle(ctx, record); err != nil {
			return fmt.Errorf("logging: handle: %w", err)
		}

		return nil
	}

	newRecord := slog.NewRecord(record.Time, record.Level, msg, record.PC)
	index := 0

	record.Attrs(func(attr slog.Attr) bool {
		switch {
		case index < firstChangedIndex:
			newRecord.AddAttrs(attr)
		case index == firstChangedIndex:
			newRecord.AddAttrs(firstChangedAttr)
		default:
			newRecord.AddAttrs(h.sanitizeOwnedAttr(attr))
		}

		index++

		return true
	})

	if err := h.inner.Handle(ctx, newRecord); err != nil {
		return fmt.Errorf("logging: handle: %w", err)
	}

	return nil
}

func (h *sanitizeHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 || h.blockedGroup != "" {
		return h
	}

	sanitized := make([]slog.Attr, 0, len(attrs))
	for _, attr := range attrs {
		sanitized = append(sanitized, h.sanitizeOwnedAttr(attr))
	}

	return &sanitizeHandler{
		inner:         h.inner.WithAttrs(sanitized),
		inMaskedGroup: h.inMaskedGroup,
		privacy:       h.privacy,
		groupDepth:    h.groupDepth,
	}
}

// 열린 group 이름이 privacy·credential key면 그 아래 attr은 key가 무엇이든 값이 그 식별자나
// credential의 구성 요소다.
func (h *sanitizeHandler) WithGroup(name string) slog.Handler {
	if name == "" || h.blockedGroup != "" {
		return h
	}

	if h.groupDepth == maxSanitizeGroupDepth {
		return &sanitizeHandler{inner: h.inner, blockedGroup: name}
	}

	normalizedName := normalizeSensitiveKey(name)

	return &sanitizeHandler{
		inner:         h.inner.WithGroup(name),
		inMaskedGroup: h.inMaskedGroup || h.privacy.isMaskedNormalizedKey(normalizedName),
		privacy:       h.privacy,
		groupDepth:    h.groupDepth + 1,
	}
}

func (h *sanitizeHandler) sanitizeOwnedAttr(attr slog.Attr) slog.Attr {
	if h.inMaskedGroup {
		return slog.String(attr.Key, redactedValue)
	}

	out, _ := h.privacy.sanitizeAttrDepth(attr, h.groupDepth)

	return out
}

func (p privacyKeys) sanitizeAttr(attr slog.Attr) slog.Attr {
	out, _ := p.sanitizeAttrChanged(attr)
	return out
}

// 변경 없는 scalar·group은 원본을 유지해 record 재구축과 할당을 생략한다.
func (p privacyKeys) sanitizeAttrChanged(attr slog.Attr) (slog.Attr, bool) {
	return p.sanitizeAttrDepth(attr, 0)
}

const (
	maxSanitizeGroupDepth = 8
	maxSanitizeLogValues  = 100
	blockedUnsupported    = "***BLOCKED:unsupported_type***"
	blockedResolution     = "***BLOCKED:resolution_failed***"
	blockedDepth          = "***BLOCKED:group_depth_exceeded***"
)

func (p privacyKeys) sanitizeAttrDepth(attr slog.Attr, depth int) (slog.Attr, bool) {
	// 버릴 값의 LogValue·Error·marshal 메서드는 실행하지 않는다.
	normalizedKey := normalizeSensitiveKey(attr.Key)
	if p.isMaskedNormalizedKey(normalizedKey) {
		return slog.String(attr.Key, redactedValue), true
	}

	changed := attr.Value.Kind() == slog.KindLogValuer
	if changed {
		resolved, ok := resolveLogValue(attr.Value)
		if !ok {
			return slog.String(attr.Key, blockedResolution), true
		}

		attr.Value = resolved
	}

	switch attr.Value.Kind() {
	case slog.KindAny:
		value := attr.Value.Any()
		if value == nil {
			return attr, changed
		}

		if err, ok := value.(error); ok {
			return slog.String(attr.Key, sanitizeErrorDiagnostic(err)), true
		}

		return slog.String(attr.Key, blockedUnsupported), true
	case slog.KindGroup:
		return p.sanitizeGroupAttr(attr, depth, changed)
	case slog.KindString:
		if isBroadValueNormalizedKey(normalizedKey) && isSecretLikeValue(attr.Value.String()) {
			return slog.String(attr.Key, redactedValue), true
		}

		redacted := redactSecrets(attr.Value.String())

		return slog.String(attr.Key, redacted), changed || redacted != attr.Value.String()
	case slog.KindBool, slog.KindDuration, slog.KindFloat64, slog.KindInt64, slog.KindTime, slog.KindUint64:
		return attr, changed
	case slog.KindLogValuer:
		return slog.String(attr.Key, blockedResolution), true
	default:
		return slog.String(attr.Key, blockedUnsupported), true
	}
}

func (p privacyKeys) sanitizeGroupAttr(attr slog.Attr, depth int, changed bool) (slog.Attr, bool) {
	if depth == maxSanitizeGroupDepth {
		return slog.String(attr.Key, blockedDepth), true
	}

	groupAttrs := attr.Value.Group()
	for index, groupAttr := range groupAttrs {
		out, childChanged := p.sanitizeAttrDepth(groupAttr, depth+1)
		if !childChanged {
			continue
		}

		sanitized := make([]slog.Attr, len(groupAttrs))
		copy(sanitized, groupAttrs[:index])

		sanitized[index] = out
		for next := index + 1; next < len(groupAttrs); next++ {
			sanitized[next], _ = p.sanitizeAttrDepth(groupAttrs[next], depth+1)
		}

		attr.Value = slog.GroupValue(sanitized...)

		return attr, true
	}

	return attr, changed
}

// slog.Value.Resolve의 오류 값은 사용자 error와 구분할 수 없으므로 여기서 실패를 분리한다.
// 사용자 panic 값의 문자열화나 스택 출력 없이 고정 사유만 남긴다.
func resolveLogValue(value slog.Value) (resolved slog.Value, ok bool) {
	defer func() {
		if recover() != nil {
			resolved = slog.Value{}
			ok = false
		}
	}()

	for range maxSanitizeLogValues {
		value = value.LogValuer().LogValue()
		if value.Kind() != slog.KindLogValuer {
			return value, true
		}
	}

	return slog.Value{}, false
}

func sanitizeErrorDiagnostic(err error) (text string) {
	defer func() {
		if recover() != nil {
			text = blockedResolution
		}
	}()

	return RedactDiagnostic(err.Error())
}

func isSensitiveKey(key string) bool {
	return isSensitiveNormalizedKey(normalizeSensitiveKey(key))
}

func isSensitiveNormalizedKey(normalized string) bool {
	if normalized == "" {
		return false
	}

	if _, ok := sensitiveExactKeys[normalized]; ok {
		return true
	}

	return strings.HasSuffix(normalized, "_token") ||
		strings.HasSuffix(normalized, "_secret") ||
		strings.HasSuffix(normalized, "_password") ||
		strings.HasSuffix(normalized, "_pwd") ||
		strings.HasSuffix(normalized, "_passwd") ||
		strings.HasSuffix(normalized, "_api_key") ||
		strings.HasSuffix(normalized, "_private_key") ||
		strings.HasSuffix(normalized, "_secret_key")
}

func (p privacyKeys) isMaskedNormalizedKey(normalized string) bool {
	return p.isPrivacyNormalizedKey(normalized) || isSensitiveNormalizedKey(normalized)
}

func normalizeSensitiveKey(key string) string {
	key = strings.ToLower(strings.TrimSpace(key))
	key = strings.ReplaceAll(key, "-", "_")
	key = strings.ReplaceAll(key, ".", "_")
	key = strings.ReplaceAll(key, " ", "_")

	return key
}

var broadValueKeys = map[string]struct{}{
	"key": {},
}

func isBroadValueNormalizedKey(normalized string) bool {
	_, ok := broadValueKeys[normalized]
	return ok
}

func (p privacyKeys) isPrivacyNormalizedKey(normalized string) bool {
	_, ok := p[normalized]
	return ok
}

var secretLikePrefixes = []string{
	"sk_", "pk_live", "pk_test", "rk_live", "rk_test",
	"ghp_", "gho_", "ghu_", "ghs_", "ghr_", "github_pat_",
	"xoxb-", "xoxp-", "xoxa-", "xoxr-",
	"akia", "asia",
	"aiza",
	"eyj",
}

var secretLikeExactValues = map[string]struct{}{
	"access-token":  {},
	"access_token":  {},
	"api-key":       {},
	tokenAPIKey:     {},
	"private-key":   {},
	tokenPrivateKey: {},
	"refresh-token": {},
	"refresh_token": {},
	"secret-key":    {},
	tokenSecretKey:  {},
}

const secretLikeMinLen = 24

func isSecretLikeValue(v string) bool {
	if _, ok := secretLikeExactValues[strings.ToLower(strings.TrimSpace(v))]; ok {
		return true
	}

	if hasSecretLikePrefix(v) {
		return true
	}

	return isHighEntropyToken(v)
}

func hasSecretLikePrefix(v string) bool {
	lower := strings.ToLower(v)

	for _, p := range secretLikePrefixes {
		if strings.HasPrefix(lower, p) {
			return true
		}
	}

	return false
}

func isHighEntropyToken(v string) bool {
	if len(v) < secretLikeMinLen {
		return false
	}

	var hasLower, hasUpper, hasDigit bool

	for i := range len(v) {
		c := v[i]
		switch {
		case c >= 'a' && c <= 'z':
			hasLower = true
		case c >= 'A' && c <= 'Z':
			hasUpper = true
		case c >= '0' && c <= '9':
			hasDigit = true
		case c == '_' || c == '-' || c == '.' || c == '+' || c == '/' || c == '=':
		default:
			return false
		}
	}

	return hasLower && hasUpper && hasDigit
}
