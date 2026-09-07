package kakaoformat

import "testing"

func TestRenderLinkAndCodeBoundaries(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ input, want string }{
		{"[x](https://example.com/`item`)", "x( https://example.com/`item` )"},
		{"`[x](https://example.com/item)`", "⦗ [x](https://example.com/item) ⦘"},
		{"**https://example.com/path**", "❪https://example.com/path❫"},
		{"__https://example.com/path__", "❪https://example.com/path❫"},
		{"See **https://example.com/path** now", "See ❪https://example.com/path❫ now"},
		{"https://example.com/a*b*", "https://example.com/a*b*"},
		{"https://example.com/a_b_c", "https://example.com/a_b_c"},
		{"[x](https://example.com/a**)", "x( https://example.com/a** )"},
	} {
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()

			if got := Render(tc.input); got != tc.want {
				t.Fatalf("Render() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestRenderPreservesLinkDestinations(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ input, want string }{
		{"[x](https://example.com/a_(b))", "x( https://example.com/a_(b) )"},
		{"[x](https://example.com/a_b_c)", "x( https://example.com/a_b_c )"},
		{"https://example.com/a_b_c", "https://example.com/a_b_c"},
		{"![x](https://example.com/a(b).png)", "x( https://example.com/a(b).png )"},
	} {
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()

			if got := Render(tc.input); got != tc.want {
				t.Fatalf("Render() = %q, want %q", got, tc.want)
			}
		})
	}
}
