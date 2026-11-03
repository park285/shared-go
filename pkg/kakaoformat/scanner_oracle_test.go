package kakaoformat

import (
	"cmp"
	"math/rand/v2"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// 바이트 스캐너로 바꾸기 전의 정규식 구현을 오라클로 두고, 같은 입력에서 결과가 바이트 단위로 같은지 비교한다.
var (
	oracleReferenceHead = regexp.MustCompile(`!?\[([^\]]*)\]\(`)
	oracleDateListLine  = regexp.MustCompile(`(?m)^([ \t]{0,3})(\d{4})\.([ \t]+\d{1,2}\.)`)
	oracleBlankRun      = regexp.MustCompile(`\n{3,}`)
	oracleLiteralURL    = regexp.MustCompile(`https?://[^\s<>]+`)
)

func oracleLiteralDestinations(input string) [][2]int {
	var ranges [][2]int

	closing := linkParentheses(input)

	for _, match := range oracleReferenceHead.FindAllStringIndex(input, -1) {
		if end, ok := closing[match[1]-1]; ok {
			ranges = append(ranges, [2]int{match[1], end})
		}
	}

	for _, match := range oracleLiteralURL.FindAllStringIndex(input, -1) {
		ranges = append(ranges, [2]int{match[0], match[1]})
	}

	slices.SortFunc(ranges, func(a, b [2]int) int { return cmp.Compare(a[0], b[0]) })

	merged := ranges[:0]
	for _, span := range ranges {
		if len(merged) > 0 && span[0] <= merged[len(merged)-1][1] {
			merged[len(merged)-1][1] = max(merged[len(merged)-1][1], span[1])
		} else {
			merged = append(merged, span)
		}
	}

	return merged
}

func oracleEscapeDateListMarkers(input string) string {
	return oracleDateListLine.ReplaceAllString(input, `$1$2\.$3`)
}

func oracleCleanupSpacing(input string) string {
	lines := strings.Split(input, "\n")
	for i, line := range lines {
		lead := line[:len(line)-len(strings.TrimLeft(line, " \t"))]

		lines[i] = lead + strings.TrimRight(line[len(lead):], " \t")
	}

	return oracleBlankRun.ReplaceAllString(strings.Join(lines, "\n"), "\n\n")
}

// oracleLiteralURLParserEnd는 URL 인라인 파서가 줄 첫머리에서만 받던 정규식 일치의 끝을, 없으면 -1을 돌려준다.
func oracleLiteralURLParserEnd(line []byte) int {
	match := oracleLiteralURL.FindIndex(line)
	if len(match) != 2 || match[0] != 0 {
		return -1
	}

	return match[1]
}

func checkScannersMatchOracle(t *testing.T, input string) {
	t.Helper()

	if got, want := literalURLMatchEnd([]byte(input), 0), oracleLiteralURLParserEnd([]byte(input)); got != want {
		t.Fatalf("literalURLMatchEnd(%q, 0) = %d, want %d", input, got, want)
	}

	if got, want := literalDestinations(input), oracleLiteralDestinations(input); !slices.Equal(got, want) {
		t.Fatalf("literalDestinations(%q) = %v, want %v", input, got, want)
	}

	if got, want := escapeDateListMarkers(input), oracleEscapeDateListMarkers(input); got != want {
		t.Fatalf("escapeDateListMarkers(%q) = %q, want %q", input, got, want)
	}

	if got, want := cleanupSpacing(input), oracleCleanupSpacing(input); got != want {
		t.Fatalf("cleanupSpacing(%q) = %q, want %q", input, got, want)
	}
}

// 정규식 의미가 갈릴 수 있는 지점(꼬리 없는 URL 스킴, '\f'·'<'에서 끝나는 URL, Go `\s`에 없어 URL이 끝나지 않는
// '\v'·U+0085·U+00A0, 개행을 넘는 링크 머리, 겹치지 않는 재탐색, '\r' 뒤 줄 시작, 들여쓰기 4칸, 숫자 3개, 공백만 있는 줄)을 시드로 둔다.
var scannerOracleSeeds = []string{
	"", "http://", "https://", "https:// x", "http:/x", "httphttps://a", "https://a<b", "http://a\fb", "https://\xffa b",
	"http://a\vb", "https://a\u0085b", "http://a\u00a0b",
	"[a](b)", "![x](y)", "[a\nb](c)", "[a[b](c)", "[a]x[b](c)", "[a](b)(c)", "[a](b[c](d))", "](", "[]()", "[a]",
	"2026. 10. 4.", "   2026.\t1.", "    2026. 1.", "2026. 123.", "2026.1.", "\r2026. 1.", "a\n2026. 1.\n 2026.  12.", "２０２６. 1.",
	"a  \n\n\n\n  \nb \t", " \t\n\n\n", "\r \n\n\n\r", "\n\n\n", "x\t\n", "https://a.b/[c](d) e",
}

var scannerOraclePieces = []string{
	"[", "]", "(", ")", "!", "\\", " ", "\t", "\n", "\r", "\f", "\v", "\u0085", "\u00a0", "0", "1", "2", "9", ".", "a", "가", "\u200b", "\xff",
	"http://", "https://x.y/", "http", "https", "s", ":", "/", "://", "<", ">", "2026.", " 10.", "1.", "*", "`",
}

func TestScannersMatchRegexpOracle(t *testing.T) {
	t.Parallel()

	for _, input := range scannerOracleSeeds {
		checkScannersMatchOracle(t, input)
	}

	// #nosec G404 -- 실패를 재현할 수 있도록 고정 시드로 만드는 테스트 입력이며 보안 용도가 아니다.
	random := rand.New(rand.NewPCG(1, 2))

	var input strings.Builder

	for range 20_000 {
		input.Reset()

		for range random.IntN(48) {
			input.WriteString(scannerOraclePieces[random.IntN(len(scannerOraclePieces))])
		}

		checkScannersMatchOracle(t, input.String())
	}
}

func FuzzScannersMatchRegexpOracle(f *testing.F) {
	for _, input := range scannerOracleSeeds {
		f.Add(input)
	}

	f.Fuzz(func(t *testing.T, input string) {
		if len(input) > 4096 {
			t.Skip()
		}

		checkScannersMatchOracle(t, input)
	})
}
