package contracttest

import (
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/park285/shared-go/v2/pkg/irisdurable"
)

type Suite struct {
	Admitter    func(t *testing.T) irisdurable.Admitter
	NonceStore  func(t *testing.T) irisdurable.NonceStore
	NonceExpiry time.Duration
	ReplyOutbox func(t *testing.T) ReplyOutboxFixture
	Reissue     *ReissueFixture
	Retention   *RetentionFixture
}

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

func uniqueID(kind string) string {
	return fmt.Sprintf("%s-%d-%d", kind, time.Now().UnixNano(), uniqueCounter.Add(1))
}
