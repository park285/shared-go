package kakaoformat

import "strings"

// IsOpenChat은 방이 오픈채팅인지 보고합니다. RoomLinkID가 있거나 roomType이 O로 시작하면 참입니다.
func IsOpenChat(roomType, roomLinkID string) bool {
	if strings.TrimSpace(roomLinkID) != "" {
		return true
	}

	roomType = strings.TrimSpace(roomType)

	return roomType != "" && strings.HasPrefix(strings.ToUpper(roomType), "O")
}
