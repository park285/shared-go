package kakaoformat

import (
	"strconv"
	"strings"
	"testing"
)

func BenchmarkRenderNeutralizedStreamList(b *testing.B) {
	for _, count := range []int{1, 100} {
		b.Run(strconv.Itoa(count), func(b *testing.B) {
			entry := "1 · 채널 이름\n\u200b*\u200b*\u200b오늘의 방송*\u200b*\u200b _\u200b노래_\u200b #\u200b음악\nhttps://youtu.be/a_b#c\n\n──────────\n\n"
			input := strings.TrimSpace(strings.Repeat(entry, count))

			if got := Render(input); got != input {
				b.Fatal("stream display changed")
			}

			b.ReportAllocs()
			b.SetBytes(int64(len(input)))

			for b.Loop() {
				Render(input)
			}
		})
	}
}
