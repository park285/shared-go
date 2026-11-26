package kakaoformat

import "testing"

func TestRenderEmphasisNextToText(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ input, want string }{
		{"**988원**이었다는 기준", "988원이었다는 기준"},
		{"작년**988원**과 지금**874.2원**입니다.", "작년988원과 지금874.2원입니다."},
		{"**작년**보다 **지금**이 저렴합니다.", "작년보다 지금이 저렴합니다."},
		{"*강조*입니다.", "강조입니다."},
		{"앞*강조*뒤", "앞강조뒤"},
		{"***강조***입니다.", "강조입니다."},
		{"앞***강조***뒤", "앞강조뒤"},
		{"foo**bar**baz", "foobarbaz"},
		{"**123**원", "123원"},
		{"(**금액**), **차이**.", "(금액), 차이."},
		{`a**"foo"**`, `a**"foo"**`},
		{"**foo!**bar", "**foo!**bar"},
		{"** foo**", "** foo**"},
		{"**foo **", "**foo **"},
		{"foo_bar_baz foo__bar__baz 한글_원문_보존", "foo_bar_baz foo__bar__baz 한글_원문_보존"},
		{"__강조__ _기울임_ ___둘다___", "강조 기울임 둘다"},
		{"`**988원**이었다는`", "**988원**이었다는"},
		{`\*\*988원\*\*이었다는`, "**988원**이었다는"},
		{"https://example.com/a*b* https://example.com/a_b_c", "https://example.com/a*b* https://example.com/a_b_c"},
		{"**https://example.com/a_b**입니다.", "https://example.com/a_b입니다."},
	} {
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()

			if got := Render(tc.input); got != tc.want {
				t.Fatalf("Render() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestRenderExchangeRateAnswer(t *testing.T) {
	t.Parallel()

	const input = `마스터, **100엔당 988원**이었다는 기준이면 지금 환율(1엔당 약 8.742원, 즉 **100엔당 약 874.2원**)로 220만 엔을 결제했을 때 약 **250만 3,600원 아꼈을** 거예요.

- 작년 환율 적용: 약 **2,173만 6,000원**
- 지금 환율 적용: 약 **1,923만 2,400원**
- 차이: 약 **250만 원**

카드 해외결제 수수료·환전 우대율은 제외한 단순 환율 기준이에요.

출처
1. [Japanese yen to South Korean wons Exchange Rate History | Currency Converter | Wise](https://wise.com/us/currency-converter/jpy-to-krw-rate/history)

웹에서 확인함 · 출처 1개`

	const want = `마스터, 100엔당 988원이었다는 기준이면 지금 환율(1엔당 약 8.742원, 즉 100엔당 약 874.2원)로 220만 엔을 결제했을 때 약 250만 3,600원 아꼈을 거예요.

- 작년 환율 적용: 약 2,173만 6,000원
- 지금 환율 적용: 약 1,923만 2,400원
- 차이: 약 250만 원

카드 해외결제 수수료·환전 우대율은 제외한 단순 환율 기준이에요.

출처
1. Japanese yen to South Korean wons Exchange Rate History | Currency Converter | Wise( https://wise.com/us/currency-converter/jpy-to-krw-rate/history )

웹에서 확인함 · 출처 1개`

	if got := Render(input); got != want {
		t.Fatalf("Render() = %q, want %q", got, want)
	}
}

func TestRenderEmphasisBeforeKoreanParticle(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ input, want string }{
		{"**최대 50%**까지 할인됩니다.", "최대 50%까지 할인됩니다."},
		{`**"인용"**은 중요합니다.`, `"인용"은 중요합니다.`},
		{"**사과(apple)**는 과일입니다.", "사과(apple)는 과일입니다."},
		{"**주의:**다음을 확인하세요.", "주의:다음을 확인하세요."},
		{"*'참고'*로 표시합니다.", "'참고'로 표시합니다."},
		{"3*4*5 = 60이고 2**10 = 1024", "3*4*5 = 60이고 2**10 = 1024"},
		{"__init__.py와 __init__(self), __강조__.", "__init__.py와 __init__(self), 강조."},
	} {
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()

			if got := Render(tc.input); got != tc.want {
				t.Fatalf("Render() = %q, want %q", got, tc.want)
			}
		})
	}
}
