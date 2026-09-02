package contracttest

import (
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/park285/shared-go/v2/pkg/irisdurable"
)

// Suite는 한 구현이 제공하는 fixture 집합이다. Nil인 항목은 그 절을 건너뛴다.
type Suite struct {
	// Admitter는 호출마다 격리된 admitter를 만든다.
	Admitter func(t *testing.T) irisdurable.Admitter
	// NonceStore는 호출마다 격리된 nonce store를 만든다.
	NonceStore func(t *testing.T) irisdurable.NonceStore
	// NonceExpiry는 만료 검증에 쓸 ttl이다. 0이면 만료 절을 건너뛰고, 5초를 넘으면 실패한다.
	NonceExpiry time.Duration
	// ReplyOutbox는 호출마다 격리된 reply outbox fixture를 만든다.
	ReplyOutbox func(t *testing.T) ReplyOutboxFixture
	// Reissue는 구현의 reissue ladder와 conflict 판정이다.
	Reissue *ReissueFixture
	// Retention은 구현이 실제로 쓰는 보존·지평 값이다.
	Retention *RetentionFixture
}

// Run은 Suite가 제공한 모든 절을 서브테스트로 실행한다.
func Run(t *testing.T, suite Suite) {
	t.Helper()

	if suite.Admitter != nil {
		t.Run("Admission", func(t *testing.T) { runAdmission(t, suite.Admitter) })
	}

	if suite.NonceStore != nil {
		t.Run("Nonce", func(t *testing.T) { runNonce(t, suite.NonceStore, suite.NonceExpiry) })
	}

	if suite.ReplyOutbox != nil {
		t.Run("ReplyOutbox", func(t *testing.T) { runReplyOutbox(t, suite.ReplyOutbox) })
	}

	if suite.Reissue != nil {
		t.Run("Reissue", func(t *testing.T) { runReissue(t, *suite.Reissue) })
	}

	if suite.Retention != nil {
		t.Run("Retention", func(t *testing.T) { runRetention(t, *suite.Retention) })
	}
}

var uniqueCounter atomic.Uint64

// uniqueID는 프로세스 안에서 충돌하지 않고 Iris clientRequestId 문자 집합([A-Za-z0-9._:-])에도
// 맞는 식별자를 만든다.
func uniqueID(kind string) string {
	return fmt.Sprintf("%s-%d-%d", kind, time.Now().UnixNano(), uniqueCounter.Add(1))
}
