package irisdurable

import (
	"context"
	"time"
)

// NonceStore는 iris-client-go webhook.SetOnceNonceStore와 같은 모양의 HMAC nonce 저장소 계약이다.
//
// IsDuplicate는 최초 관측이면 키를 원자적으로 한 번 기록하고 false를, 이미 기록된 키면 true를
// 반환한다. 기록에 실패하면 오류를 반환한다(fail-closed). Ttl이 지난 키는 다시 최초 관측으로
// 취급한다. SetOnceNonce는 그 성질을 backend가 선언하는 마커다.
type NonceStore interface {
	IsDuplicate(ctx context.Context, key string, ttl time.Duration) (bool, error)
	SetOnceNonce()
}
