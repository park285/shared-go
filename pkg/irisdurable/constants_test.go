package irisdurable_test

import (
	"testing"

	"github.com/park285/shared-go/v2/pkg/irisdurable"
)

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
