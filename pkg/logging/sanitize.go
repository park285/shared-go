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
	"fmt"
	"log/slog"
	"maps"
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
	tokenUserName      = "user_name"
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
	"token":            {},
	"bot_token":        {},
	"access_token":     {},
	"refresh_token":    {},
	tokenPassword:      {},
	tokenPwd:           {},
	tokenPasswd:        {},
	tokenSecret:        {},
	"client_secret":    {},
	tokenAPIKey:        {},
	tokenAPIKeyCompact: {},
	tokenPrivateKey:    {},
	tokenSecretKey:     {},
	"authorization":    {},
	"auth_header":      {},
	"cookie":           {},
	"webhook_url":      {},
	"database_url":     {},
	"postgres_dsn":     {},
	"connection_url":   {},
}

// 정확 일치만 사용한다. *_id suffix 규칙을 더하면 channel_id 같은 공개 콘텐츠 ID까지 가려진다.
var privacyExactKeys = map[string]struct{}{
	"room":              {},
	"room_name":         {},
	"chat_id":           {},
	tokenUserName:       {},
	"thread_id":         {},
	"session_thread_id": {},
	"sender":            {},
	"game_key":          {},
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

		key := assignmentKeyBefore(s, separator)
		if !isSensitiveKey(key) {
			continue
		}

		valueStart, valueEnd := assignmentValueAfter(s, separator+1)
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

	// 변경 감지 패스가 처음 바뀐 attr의 위치와 정제 결과를 남긴다. 그 앞의 attr들은
	// changed=false로 판정됐으므로 정제해도 원본과 같은 값이라 재구축 때 원본을 그대로 쓴다.
	firstChangedIndex := -1

	var firstChangedAttr slog.Attr

	if !changed && !h.inMaskedGroup {
		index := 0

		record.Attrs(func(attr slog.Attr) bool {
			out, attrChanged := sanitizeAttrChanged(attr)
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
			return fmt.Errorf("handle: %w", err)
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
		return fmt.Errorf("handle: %w", err)
	}

	return nil
}

func (h *sanitizeHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	sanitized := make([]slog.Attr, 0, len(attrs))
	for _, attr := range attrs {
		sanitized = append(sanitized, h.sanitizeOwnedAttr(attr))
	}

	return &sanitizeHandler{inner: h.inner.WithAttrs(sanitized), inMaskedGroup: h.inMaskedGroup}
}

// 열린 group 이름이 privacy·credential key면 그 아래 attr은 key가 무엇이든 값이 그 식별자나
// credential의 구성 요소다.
func (h *sanitizeHandler) WithGroup(name string) slog.Handler {
	normalizedName := normalizeSensitiveKey(name)

	return &sanitizeHandler{
		inner:         h.inner.WithGroup(name),
		inMaskedGroup: h.inMaskedGroup || isMaskedNormalizedKey(normalizedName),
	}
}

func (h *sanitizeHandler) sanitizeOwnedAttr(attr slog.Attr) slog.Attr {
	if h.inMaskedGroup {
		return slog.String(attr.Key, redactedValue)
	}

	return sanitizeAttr(attr)
}

func sanitizeAttr(attr slog.Attr) slog.Attr {
	out, _ := sanitizeAttrChanged(attr)
	return out
}

// sanitizeAttrChanged는 sanitizeAttr와 동일한 정규화를 수행하되, 결과가 원본 attr과
// byte-identical하게 같은지(changed=false) 여부를 함께 반환한다. Handle의 fast-path는
// 이 신호로 변경이 전혀 없을 때 record 재구축을 건너뛴다.
// Resolve 변경 판정에 Value.Equal을 쓰면 안 된다: KindAny끼리의 Equal은 내부 any를
// ==로 비교해 []int 같은 uncomparable 타입에서 panic한다. Resolve가 값을 바꾸는 건
// LogValuer뿐이므로 Kind 검사로 보수적으로(LogValuer면 항상 changed) 판정한다.
func sanitizeAttrChanged(attr slog.Attr) (slog.Attr, bool) {
	changed := attr.Value.Kind() == slog.KindLogValuer

	attr.Value = attr.Value.Resolve()
	// key 기반 판정은 값을 읽지 않으므로 모든 값 분기(KindAny·KindGroup·KindString)보다 앞이어야
	// 한다. 뒤에 두면 마스킹 여부가 값 타입이나 무관한 map 내용에 종속된다.
	normalizedKey := normalizeSensitiveKey(attr.Key)
	if isMaskedNormalizedKey(normalizedKey) {
		return slog.String(attr.Key, redactedValue), true
	}

	if attr.Value.Kind() == slog.KindAny {
		if out, masked := sanitizeAnyAttr(attr); masked {
			return out, true
		}
	}

	if attr.Value.Kind() == slog.KindGroup {
		groupAttrs := attr.Value.Group()
		for index, groupAttr := range groupAttrs {
			out, childChanged := sanitizeAttrChanged(groupAttr)
			if !childChanged {
				continue
			}

			sanitized := make([]slog.Attr, len(groupAttrs))
			copy(sanitized, groupAttrs[:index])

			sanitized[index] = out
			for next := index + 1; next < len(groupAttrs); next++ {
				sanitized[next] = sanitizeAttr(groupAttrs[next])
			}

			attr.Value = slog.GroupValue(sanitized...)

			return attr, true
		}

		return attr, changed
	}

	if attr.Value.Kind() != slog.KindString {
		return attr, changed
	}

	if isBroadValueNormalizedKey(normalizedKey) && isSecretLikeValue(attr.Value.String()) {
		return slog.String(attr.Key, redactedValue), true
	}

	redacted := redactSecrets(attr.Value.String())
	if redacted != attr.Value.String() {
		changed = true
	}

	return slog.String(attr.Key, redacted), changed
}

// 두 번째 반환값은 sanitizeAttrChanged의 changed와 달리 "이 분기가 결과를 확정했다"는 신호다.
func sanitizeAnyAttr(attr slog.Attr) (slog.Attr, bool) {
	value := attr.Value.Any()
	if raw, ok := value.(map[string]any); ok {
		if masked, mapChanged := maskPrivacyMap(raw); mapChanged {
			return slog.Any(attr.Key, masked), true
		}
	}

	if err, ok := value.(error); ok {
		return slog.String(attr.Key, RedactDiagnostic(err.Error())), true
	}

	return attr, false
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

func isMaskedNormalizedKey(normalized string) bool {
	return isPrivacyNormalizedKey(normalized) || isSensitiveNormalizedKey(normalized)
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

func isBroadValueKey(key string) bool {
	return isBroadValueNormalizedKey(normalizeSensitiveKey(key))
}

func isBroadValueNormalizedKey(normalized string) bool {
	_, ok := broadValueKeys[normalized]
	return ok
}

func isPrivacyKey(key string) bool {
	return isPrivacyNormalizedKey(normalizeSensitiveKey(key))
}

func isPrivacyNormalizedKey(normalized string) bool {
	_, ok := privacyExactKeys[normalized]
	return ok
}

const maxPrivacyMapDepth = 8

// 호출자 map을 제자리에서 바꾸면 로깅이 호출자 상태를 변조하므로 privacy·credential hit일 때만 사본을 만든다.
func maskPrivacyMap(raw map[string]any) (map[string]any, bool) {
	return maskPrivacyMapDepth(raw, 0)
}

func maskPrivacyMapDepth(raw map[string]any, depth int) (map[string]any, bool) {
	var masked map[string]any

	for key, value := range raw {
		if shouldMaskStructuredMapValue(key, value) {
			if masked == nil {
				masked = make(map[string]any, len(raw))
				maps.Copy(masked, raw)
			}

			masked[key] = redactedValue

			continue
		}

		nested, ok := value.(map[string]any)
		// self-referential map도 유한 시간에 끝나도록 중첩 탐색을 제한한다.
		if !ok || depth >= maxPrivacyMapDepth {
			continue
		}

		nestedMasked, changed := maskPrivacyMapDepth(nested, depth+1)
		if !changed {
			continue
		}

		if masked == nil {
			masked = make(map[string]any, len(raw))
			maps.Copy(masked, raw)
		}

		masked[key] = nestedMasked
	}

	return masked, masked != nil
}

func shouldMaskStructuredMapValue(key string, value any) bool {
	normalizedKey := normalizeSensitiveKey(key)
	if isMaskedNormalizedKey(normalizedKey) {
		return true
	}

	text, ok := value.(string)

	return ok && isBroadValueNormalizedKey(normalizedKey) && isSecretLikeValue(text)
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
