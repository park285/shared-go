package contracttest

import (
	"testing"
	"time"
)

// RetentionFixture는 구현이 실제로 쓰는 보존·지평 값이다. IrisAdmissionRetention은 필수이며
// 정본(iris-client-go 공개 상수)을 넘긴다. 나머지 0인 항목은 검사하지 않는다.
type RetentionFixture struct {
	// IrisAdmissionRetention은 Iris reply admission 원장의 보존이다.
	IrisAdmissionRetention time.Duration
	// ReplyOutboxRetention은 reply outbox 행의 보존이다.
	ReplyOutboxRetention time.Duration
	// AutomaticReplayHorizon은 저장된 clientRequestId로 자동 재발송하는 상한이다.
	AutomaticReplayHorizon time.Duration
	// InboxTerminalRetention은 inbox 종단 행의 보존이다.
	InboxTerminalRetention time.Duration
}

func runRetention(t *testing.T, fixture RetentionFixture) {
	t.Helper()

	admission := fixture.IrisAdmissionRetention
	if admission <= 0 {
		t.Fatal("RetentionFixture requires IrisAdmissionRetention")
	}

	if fixture.ReplyOutboxRetention == 0 && fixture.AutomaticReplayHorizon == 0 && fixture.InboxTerminalRetention == 0 {
		t.Fatal("RetentionFixture carries no values")
	}

	t.Run("ReplyOutboxRetentionCoversIrisAdmission", func(t *testing.T) {
		if fixture.ReplyOutboxRetention == 0 {
			t.Skip("no reply outbox retention")
		}

		if fixture.ReplyOutboxRetention < admission {
			t.Fatalf("reply outbox retention %s is shorter than Iris admission retention %s", fixture.ReplyOutboxRetention, admission)
		}
	})

	t.Run("AutomaticReplayHorizonStaysInsideIrisAdmission", func(t *testing.T) {
		if fixture.AutomaticReplayHorizon == 0 {
			t.Skip("no automatic replay horizon")
		}

		if fixture.AutomaticReplayHorizon >= admission {
			t.Fatalf("automatic replay horizon %s must be shorter than Iris admission retention %s", fixture.AutomaticReplayHorizon, admission)
		}
	})

	t.Run("InboxTerminalRowsOutliveIrisAdmission", func(t *testing.T) {
		if fixture.InboxTerminalRetention == 0 {
			t.Skip("no inbox terminal retention")
		}

		if fixture.InboxTerminalRetention <= admission {
			t.Fatalf("inbox terminal retention %s must exceed Iris admission retention %s; a redelivered webhook would re-execute instead of dedup", fixture.InboxTerminalRetention, admission)
		}
	})
}
