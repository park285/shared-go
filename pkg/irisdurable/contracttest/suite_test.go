package contracttest_test

import (
	"errors"
	"testing"
	"time"

	"github.com/park285/shared-go/v2/pkg/irisdurable"
	"github.com/park285/shared-go/v2/pkg/irisdurable/contracttest"
)

func TestRunAgainstMemoryImplementations(t *testing.T) {
	t.Parallel()

	contracttest.Run(t, contracttest.Suite{
		Admitter: func(t *testing.T) irisdurable.Admitter {
			t.Helper()

			return &memoryAdmitter{seen: make(map[string]struct{})}
		},
		NonceStore: func(t *testing.T) irisdurable.NonceStore {
			t.Helper()

			return &memoryNonceStore{expiries: make(map[string]time.Time)}
		},
		NonceExpiry: 200 * time.Millisecond,
		ReplyOutbox: func(t *testing.T) contracttest.ReplyOutboxFixture {
			t.Helper()

			return &memoryReplyOutbox{rows: make(map[irisdurable.ReplyIdentity]*memoryReplyRow)}
		},
		Reissue: &contracttest.ReissueFixture{
			Ladder:                irisdurable.ReissueLadder{MaxGenerations: 2, Derive: memoryDerive},
			PreHandoffConflict:    func(err error) bool { return errors.Is(err, errPreHandoff) },
			NewPreHandoffConflict: func() error { return errPreHandoff },
			NewTerminalConflict:   func() error { return errTerminal409 },
		},
		Retention: &contracttest.RetentionFixture{
			ReplyOutboxRetention:   irisdurable.ReplyOutboxMinRetention,
			AutomaticReplayHorizon: irisdurable.AutomaticReplayHorizon,
			InboxTerminalRetention: 8 * 24 * time.Hour,
		},
	})
}
