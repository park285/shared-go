package kakaoformat

import "testing"

func TestIsOpenChat(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name, roomType, linkID string
		want                   bool
	}{
		{name: "open multi", roomType: "OM", want: true},
		{name: "open direct", roomType: "od", want: true},
		{name: "named open", roomType: "OpenChat", want: true},
		{name: "link only", linkID: "55", want: true},
		{name: "direct", roomType: "DirectChat", want: false},
		{name: "multi", roomType: "MultiChat", want: false},
		{name: "unknown", want: false},
		{name: "multi with link", roomType: "MultiChat", linkID: "9", want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := IsOpenChat(tt.roomType, tt.linkID); got != tt.want {
				t.Fatalf("IsOpenChat(%q, %q) = %v, want %v", tt.roomType, tt.linkID, got, tt.want)
			}
		})
	}
}
