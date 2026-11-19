package kakaoformat

import "strings"

func IsOpenChat(roomType, roomLinkID string) bool {
	if strings.TrimSpace(roomLinkID) != "" {
		return true
	}

	roomType = strings.TrimSpace(roomType)

	return roomType != "" && strings.HasPrefix(strings.ToUpper(roomType), "O")
}
