package irisdurable_test

import (
	"testing"
	"time"

	"github.com/park285/shared-go/v2/pkg/irisdurable"
)

func TestReplayHorizonKeepsMarginInsideIrisRetention(t *testing.T) {
	t.Parallel()

	if irisdurable.AutomaticReplayHorizon != 144*time.Hour {
		t.Fatalf("AutomaticReplayHorizon = %s; want 144h", irisdurable.AutomaticReplayHorizon)
	}

	if irisdurable.AutomaticReplayHorizon+irisdurable.ReplayHorizonMargin != irisdurable.IrisReplyAdmissionRetention {
		t.Fatal("automatic replay horizon plus margin must equal Iris admission retention")
	}

	if irisdurable.ReplyOutboxMinRetention < irisdurable.IrisReplyAdmissionRetention {
		t.Fatal("reply outbox retention must cover Iris admission retention")
	}
}

func TestReplyStatusVocabulary(t *testing.T) {
	t.Parallel()

	for _, status := range []irisdurable.ReplyStatus{irisdurable.ReplyStatusDead, irisdurable.ReplyStatusPermanentConflict} {
		if !status.Terminal() || status.Resendable() {
			t.Fatalf("%s must be terminal and not resendable", status)
		}
	}

	for _, status := range []irisdurable.ReplyStatus{
		irisdurable.ReplyStatusPending,
		irisdurable.ReplyStatusRetryablePreDispatch,
		irisdurable.ReplyStatusOutcomeUnknown,
	} {
		if status.Terminal() || !status.Resendable() {
			t.Fatalf("%s must be resendable and not terminal", status)
		}
	}

	for _, status := range []irisdurable.ReplyStatus{irisdurable.ReplyStatusSubmitting, irisdurable.ReplyStatusAccepted} {
		if status.Terminal() || status.Resendable() {
			t.Fatalf("%s must be neither terminal nor resendable", status)
		}
	}
}
