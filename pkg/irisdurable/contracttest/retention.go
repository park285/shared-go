package contracttest

import (
	"testing"
	"time"

	"github.com/park285/shared-go/v2/pkg/irisdurable"
)

// RetentionFixture는 구현이 실제로 쓰는 보존·지평 값이다. 0인 항목은 검사하지 않는다.
type RetentionFixture struct {
	// ReplyOutboxRetention은 reply outbox 행의 보존이다.
	ReplyOutboxRetention time.Duration
	// AutomaticReplayHorizon은 저장된 clientRequestId로 자동 재발송하는 상한이다.
	AutomaticReplayHorizon time.Duration
	// InboxTerminalRetention은 inbox 종단 행의 보존이다.
	InboxTerminalRetention time.Duration
}

func runRetention(t *testing.T, fixture RetentionFixture) {
	t.Helper()

	if fixture.ReplyOutboxRetention == 0 && fixture.AutomaticReplayHorizon == 0 && fixture.InboxTerminalRetention == 0 {
		t.Fatal("RetentionFixture carries no values")
	}

	t.Run("ReplyOutboxRetentionCoversIrisAdmission", func(t *testing.T) {
		if fixture.ReplyOutboxRetention == 0 {
			t.Skip("no reply outbox retention")
		}

		if fixture.ReplyOutboxRetention < irisdurable.ReplyOutboxMinRetention {
			t.Fatalf("reply outbox retention %s is shorter than Iris admission retention %s", fixture.ReplyOutboxRetention, irisdurable.ReplyOutboxMinRetention)
		}
	})

	t.Run("AutomaticReplayHorizonStaysInsideStackHorizon", func(t *testing.T) {
		if fixture.AutomaticReplayHorizon == 0 {
			t.Skip("no automatic replay horizon")
		}

		if fixture.AutomaticReplayHorizon > irisdurable.AutomaticReplayHorizon {
			t.Fatalf("automatic replay horizon %s exceeds the stack horizon %s", fixture.AutomaticReplayHorizon, irisdurable.AutomaticReplayHorizon)
		}

		if fixture.ReplyOutboxRetention > 0 && fixture.AutomaticReplayHorizon+irisdurable.ReplayHorizonMargin > fixture.ReplyOutboxRetention {
			t.Fatalf("automatic replay horizon %s leaves less than %s before outbox retention %s", fixture.AutomaticReplayHorizon, irisdurable.ReplayHorizonMargin, fixture.ReplyOutboxRetention)
		}
	})

	t.Run("InboxTerminalRowsOutliveReplayHorizon", func(t *testing.T) {
		if fixture.InboxTerminalRetention == 0 {
			t.Skip("no inbox terminal retention")
		}

		if fixture.InboxTerminalRetention < irisdurable.AutomaticReplayHorizon {
			t.Fatalf("inbox terminal retention %s is shorter than the automatic replay horizon %s; a redelivered webhook would re-execute instead of dedup", fixture.InboxTerminalRetention, irisdurable.AutomaticReplayHorizon)
		}
	})
}
