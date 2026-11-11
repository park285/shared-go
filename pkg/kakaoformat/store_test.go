package kakaoformat

import (
	"math/rand/v2"
	"strings"
	"testing"
)

// 새 Restore는 모든 입력에서 이 결과와 바이트 단위로 같아야 한다.
func restoreWithReplacer(s *store, text string) string {
	if len(s.values) == 0 {
		return text
	}

	pairs := make([]string, 0, len(s.values)*2)
	for i, value := range s.values {
		pairs = append(pairs, s.token(i), value)
	}

	return strings.NewReplacer(pairs...).Replace(text)
}

// 값은 rawValues를 \x01로 나눈 조각을 돌려 써서 count개를 넣고, count 0은 값이 없는 store다.
func FuzzStoreRestoreMatchesReplacer(f *testing.F) {
	for _, seed := range []struct {
		text, rawValues string
		count           uint8
	}{
		{"\x00CODE0\x00", "a", 0},
		{"", "a", 1},
		{"앞\x00CODE0\x00뒤", "코드", 1},
		{"\x00CODE0\x00\x00CODE1\x00", "a\x01b", 2},
		{"\x00CODE1\x00\x00CODE10\x00\x00CODE11\x00", "v\x01w\x01x", 11},
		{"\x00CODE254\x00\x00CODE100\x00\x00CODE255\x00", "a\x01b\x01c", 255},
		{"\x00CODE00\x00\x00CODE01\x00\x00CODE+1\x00\x00CODE-1\x00\x00CODE\x00", "a\x01b", 2},
		{"\x00CODE99999999999999999999999\x00", "a", 3},
		{"\x00CODE1\x00CODE0\x00", "a\x01b", 2},
		{"\x00CODE\x00CODE0\x00", "a", 1},
		{"\x00\x00CODE0\x00\x00", "a", 1},
		{"\x00COD\x00CODE0", "a", 1},
		{"a\x00b\x00CODE0\x00c\x00", "a", 1},
		{"\x00NEUTRALIZED0\x00\x00CODE0\x00", "`\x00NEUTRALIZED0\x00`", 1},
		{"\x00CODE0\x00\x00CODE1\x00", "\x00CODE1\x00\x01x", 2},
		{"\x00CODE0\x00\x00CODE0\x00", "\x00", 1},
		{"\xff\x00CODE0\x00\xff", "", 1},
	} {
		f.Add("CODE", seed.text, seed.rawValues, seed.count)
	}

	for _, seed := range []struct {
		kind, text, rawValues string
		count                 uint8
	}{
		{"NEUTRALIZED", "\x00CODE0\x00\x00NEUTRALIZED0\x00", "*\u200b", 1},
		{"", "\x000\x00\x0010\x00", "a", 11},
		{"C1", "\x00C10\x00\x00C110\x00", "a\x01b", 11},
		{"A\x00B", "\x00A\x00B0\x00\x00B0\x00", "a", 1},
	} {
		f.Add(seed.kind, seed.text, seed.rawValues, seed.count)
	}

	addRandomRestoreSeeds(f)

	f.Fuzz(func(t *testing.T, kind, text, rawValues string, count uint8) {
		s := newStore(kind)
		parts := strings.Split(rawValues, "\x01")

		for i := range int(count) {
			s.Put(parts[i%len(parts)])
		}

		if got, want := s.Restore(text), restoreWithReplacer(s, text); got != want {
			t.Fatalf("kind=%q text=%q values=%q: got %q, want %q", kind, text, s.values, got, want)
		}
	})
}

// fuzz를 돌리지 않는 일반 go test에서도 표식 조각을 무작위로 이은 입력을 고정 시드로 재현해 비교한다.
// 값에도 표식 조각을 섞어 바꾼 값을 다시 훑지 않는지 함께 본다.
func addRandomRestoreSeeds(f *testing.F) {
	f.Helper()

	fragments := []string{
		"\x00", "\x00", "CODE", "NEUTRALIZED", "C1", "0", "1", "10", "00", "+", "x", "한", "\xff",
		"\x00CODE0\x00", "\x00CODE1\x00", "\x00CODE10\x00", "\x00NEUTRALIZED0\x00", "\x00C11\x00",
	}
	kinds := []string{"CODE", "NEUTRALIZED", "", "C1"}
	counts := []uint8{0, 1, 2, 3, 11, 12}
	random := rand.New(rand.NewPCG(20261103, 7)) //nolint:gosec // 재현 가능한 테스트 입력을 만드는 고정 시드 난수이며 보안 용도가 아니다.

	join := func(limit int) string {
		var output strings.Builder

		for range random.IntN(limit) {
			output.WriteString(fragments[random.IntN(len(fragments))])
		}

		return output.String()
	}

	for range 1000 {
		values := make([]string, 1+random.IntN(3))
		for i := range values {
			values[i] = join(3)
		}

		f.Add(kinds[random.IntN(len(kinds))], join(16), strings.Join(values, "\x01"), counts[random.IntN(len(counts))])
	}
}
