package contracttest

import (
	"errors"
	"fmt"
	"testing"

	"github.com/park285/shared-go/v2/pkg/irisdurable"
)

// ReplyOutboxFixture는 구현의 reply outbox와, 그 구현이 받아들이는 형식의 레코드 생성기다.
type ReplyOutboxFixture interface {
	irisdurable.ReplyOutbox

	// NewRecord는 유일한 식별자·방·base clientRequestId를 가진 새 레코드를 만든다.
	NewRecord(t *testing.T, payload []byte) irisdurable.ReplyRecord
}

func runReplyOutbox(t *testing.T, newFixture func(*testing.T) ReplyOutboxFixture) {
	t.Helper()

	t.Run("StageIsIdempotentAndDetectsDivergence", func(t *testing.T) { testReplyStageIdempotent(t, newFixture(t)) })
	t.Run("OutcomeUnknownPreservesStoredIdentityForResend", func(t *testing.T) { testReplyOutcomeUnknownResend(t, newFixture(t)) })
	t.Run("TerminalStatusesScrubPayloadAndRefuseAttempts", func(t *testing.T) {
		for _, status := range []irisdurable.ReplyStatus{irisdurable.ReplyStatusDead, irisdurable.ReplyStatusPermanentConflict} {
			t.Run(string(status), func(t *testing.T) { testReplyTerminal(t, newFixture(t), status) })
		}
	})
	t.Run("AcceptedKeepsClientRequestID", func(t *testing.T) { testReplyAccepted(t, newFixture(t)) })
	t.Run("StaleClaimTokenCannotSettle", func(t *testing.T) { testReplyStaleClaim(t, newFixture(t)) })
}

func testReplyStageIdempotent(t *testing.T, outbox ReplyOutboxFixture) {
	t.Helper()

	record := outbox.NewRecord(t, replyPayload("first"))

	requireStage(t, outbox, record, irisdurable.ReplyStaged)
	requireStage(t, outbox, record, irisdurable.ReplyAlreadyStaged)

	diverged := record

	diverged.Payload = replyPayload("second")
	requireStage(t, outbox, diverged, irisdurable.ReplyPayloadDiverged)

	state := requireInspect(t, outbox, record.ReplyIdentity)
	if state.ClientRequestID != record.ClientRequestID || !state.PayloadPresent || state.Status.Terminal() {
		t.Fatalf("stored state after divergent stage = %+v; want the original record intact", state)
	}
}

func testReplyOutcomeUnknownResend(t *testing.T, outbox ReplyOutboxFixture) {
	t.Helper()

	record := outbox.NewRecord(t, replyPayload("unknown"))
	requireStage(t, outbox, record, irisdurable.ReplyStaged)

	first := requireBeginAttempt(t, outbox, record.ReplyIdentity)
	if first.ClientRequestID != record.ClientRequestID {
		t.Fatalf("first attempt clientRequestId = %q; want stored %q", first.ClientRequestID, record.ClientRequestID)
	}

	requireSettle(t, outbox, first, irisdurable.ReplyOutcome{
		Status:          irisdurable.ReplyStatusOutcomeUnknown,
		ClientRequestID: first.ClientRequestID,
	})

	state := requireInspect(t, outbox, record.ReplyIdentity)
	if state.Status != irisdurable.ReplyStatusOutcomeUnknown || !state.Status.Resendable() {
		t.Fatalf("status after unknown outcome = %s; want resendable outcome_unknown", state.Status)
	}

	if state.ClientRequestID != record.ClientRequestID || !state.PayloadPresent {
		t.Fatalf("state after unknown outcome = %+v; want stored clientRequestId and payload preserved", state)
	}

	second := requireBeginAttempt(t, outbox, record.ReplyIdentity)
	if second.Attempt <= first.Attempt || second.ClientRequestID != record.ClientRequestID {
		t.Fatalf("resend attempt = %+v after %+v; want a later attempt with the same clientRequestId", second, first)
	}
}

func testReplyTerminal(t *testing.T, outbox ReplyOutboxFixture, status irisdurable.ReplyStatus) {
	t.Helper()

	record := outbox.NewRecord(t, replyPayload(string(status)))
	requireStage(t, outbox, record, irisdurable.ReplyStaged)

	attempt := requireBeginAttempt(t, outbox, record.ReplyIdentity)
	requireSettle(t, outbox, attempt, irisdurable.ReplyOutcome{Status: status, ClientRequestID: attempt.ClientRequestID})

	state := requireInspect(t, outbox, record.ReplyIdentity)
	if state.Status != status || state.PayloadPresent {
		t.Fatalf("state after %s = %+v; want terminal status with payload scrubbed", status, state)
	}

	if _, err := outbox.BeginAttempt(t.Context(), record.ReplyIdentity); !errors.Is(err, irisdurable.ErrReplyNotClaimable) {
		t.Fatalf("BeginAttempt after %s error = %v; want ErrReplyNotClaimable", status, err)
	}
}

func testReplyAccepted(t *testing.T, outbox ReplyOutboxFixture) {
	t.Helper()

	record := outbox.NewRecord(t, replyPayload("accepted"))
	requireStage(t, outbox, record, irisdurable.ReplyStaged)

	attempt := requireBeginAttempt(t, outbox, record.ReplyIdentity)
	requireSettle(t, outbox, attempt, irisdurable.ReplyOutcome{
		Status:          irisdurable.ReplyStatusAccepted,
		ClientRequestID: attempt.ClientRequestID,
		IrisRequestID:   uniqueID("iris"),
	})

	state := requireInspect(t, outbox, record.ReplyIdentity)
	if state.Status != irisdurable.ReplyStatusAccepted || state.ClientRequestID != record.ClientRequestID {
		t.Fatalf("state after accepted = %+v; want accepted with the stored clientRequestId", state)
	}
}

func testReplyStaleClaim(t *testing.T, outbox ReplyOutboxFixture) {
	t.Helper()

	record := outbox.NewRecord(t, replyPayload("stale"))
	requireStage(t, outbox, record, irisdurable.ReplyStaged)

	attempt := requireBeginAttempt(t, outbox, record.ReplyIdentity)
	stale := attempt

	stale.ClaimToken = attempt.ClaimToken + "-stale"

	err := outbox.Settle(t.Context(), stale, irisdurable.ReplyOutcome{
		Status:          irisdurable.ReplyStatusDead,
		ClientRequestID: attempt.ClientRequestID,
	})
	if err == nil {
		t.Fatal("settle with a stale claim token must fail")
	}

	state := requireInspect(t, outbox, record.ReplyIdentity)
	if state.Status.Terminal() || !state.PayloadPresent {
		t.Fatalf("state after stale settle = %+v; want the live attempt untouched", state)
	}
}

func replyPayload(marker string) []byte {
	return fmt.Appendf(nil, `{"type":"text","text":%q}`, "irisdurable contract "+marker)
}

func requireStage(t *testing.T, outbox irisdurable.ReplyOutbox, record irisdurable.ReplyRecord, want irisdurable.ReplyStageOutcome) {
	t.Helper()

	got, err := outbox.Stage(t.Context(), record)
	if err != nil {
		t.Fatalf("stage %+v: %v", record.ReplyIdentity, err)
	}

	if got != want {
		t.Fatalf("stage %+v = %s; want %s", record.ReplyIdentity, got, want)
	}
}

func requireBeginAttempt(t *testing.T, outbox irisdurable.ReplyOutbox, identity irisdurable.ReplyIdentity) irisdurable.ReplyAttempt {
	t.Helper()

	attempt, err := outbox.BeginAttempt(t.Context(), identity)
	if err != nil {
		t.Fatalf("begin attempt %+v: %v", identity, err)
	}

	if attempt.ClaimToken == "" || attempt.Attempt <= 0 {
		t.Fatalf("begin attempt %+v = %+v; want a claim token and a positive attempt number", identity, attempt)
	}

	return attempt
}

func requireSettle(t *testing.T, outbox irisdurable.ReplyOutbox, attempt irisdurable.ReplyAttempt, outcome irisdurable.ReplyOutcome) {
	t.Helper()

	if err := outbox.Settle(t.Context(), attempt, outcome); err != nil {
		t.Fatalf("settle %+v as %s: %v", attempt.ReplyIdentity, outcome.Status, err)
	}
}

func requireInspect(t *testing.T, outbox irisdurable.ReplyOutbox, identity irisdurable.ReplyIdentity) irisdurable.ReplyState {
	t.Helper()

	state, err := outbox.Inspect(t.Context(), identity)
	if err != nil {
		t.Fatalf("inspect %+v: %v", identity, err)
	}

	return state
}
