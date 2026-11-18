package contracttest

import (
	"testing"
	"time"
)

type RetentionFixture struct {
	IrisAdmissionRetention     time.Duration
	IrisAutomaticReplayHorizon time.Duration
	ReplyOutboxRetention       time.Duration
	AutomaticReplayHorizon     time.Duration
	InboxTerminalRetention     time.Duration
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

	t.Run("AutomaticReplayHorizonStaysInsideIrisHorizon", func(t *testing.T) {
		if fixture.AutomaticReplayHorizon == 0 {
			t.Skip("no automatic replay horizon")
		}

		canonical := fixture.IrisAutomaticReplayHorizon
		if canonical <= 0 {
			t.Fatal("RetentionFixture requires IrisAutomaticReplayHorizon with AutomaticReplayHorizon")
		}

		if canonical >= admission {
			t.Fatalf("Iris automatic replay horizon %s must be shorter than Iris admission retention %s", canonical, admission)
		}

		if fixture.AutomaticReplayHorizon > canonical {
			t.Fatalf("automatic replay horizon %s exceeds Iris automatic replay horizon %s", fixture.AutomaticReplayHorizon, canonical)
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
