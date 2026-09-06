package pgstore_test

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/park285/shared-go/v2/pkg/irisdurable"
	"github.com/park285/shared-go/v2/pkg/irisdurable/pgstore"
)

func TestBeginAttemptChecksReplayPolicyAtClaim(t *testing.T) {
	pool := newMigratedPool(t)

	for _, tc := range []struct {
		name         string
		age          time.Duration
		attempts     int
		firstAttempt bool
		allowed      bool
	}{
		{"before_horizon", time.Hour - time.Microsecond, 1, true, true},
		{"at_horizon", time.Hour, 1, true, false},
		{"after_horizon", time.Hour + time.Microsecond, 1, true, false},
		{"created_at_horizon", time.Hour, 0, false, false},
		{"last_attempt", time.Minute, 2, true, true},
		{"exhausted", time.Minute, 3, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tx, err := pool.Begin(t.Context())
			require.NoError(t, err)

			defer func() { require.NoError(t, tx.Rollback(t.Context())) }()

			store, err := pgstore.New(tx, pgstore.Options{Scope: tc.name, MaxAttempts: 3, AutomaticReplayHorizon: time.Hour})
			require.NoError(t, err)

			record := irisdurable.ReplyRecord{MessageID: "policy-message", Phase: "reply", RoomID: "policy-room", ClientRequestID: "policy-request", Payload: []byte(`{"text":"exact"}`)}

			_, err = store.Stage(t.Context(), record)
			require.NoError(t, err)

			// 같은 트랜잭션의 now()로 지평의 직전·동일·직후를 시간 대기 없이 비교한다.
			_, err = tx.Exec(t.Context(), `UPDATE iris_reply_outbox SET status='outcome_unknown', attempts=$2, created_at=now()-make_interval(secs=>$3), first_attempt_at=CASE WHEN $4 THEN now()-make_interval(secs=>$3) ELSE NULL END WHERE scope=$1`, tc.name, tc.attempts, tc.age.Seconds(), tc.firstAttempt)
			require.NoError(t, err)

			before, err := store.Inspect(t.Context(), record.ReplyIdentity)
			require.NoError(t, err)

			candidates, err := store.Redrive(t.Context(), 10)
			require.NoError(t, err)
			require.Equal(t, tc.allowed, len(candidates) == 1)

			attempt, err := store.BeginAttempt(t.Context(), record.ReplyIdentity)

			if tc.allowed {
				require.NoError(t, err)
				require.Equal(t, tc.attempts+1, attempt.Attempt)
				require.Equal(t, record.ClientRequestID, attempt.ClientRequestID)

				return
			}

			require.ErrorIs(t, err, irisdurable.ErrReplyNotClaimable)

			after, err := store.Inspect(t.Context(), record.ReplyIdentity)
			require.NoError(t, err)
			require.Equal(t, before, after, "refused claim changed durable outcome")
			require.True(t, after.PayloadPresent)
		})
	}
}

func TestBeginAttemptRechecksAfterRedriveAndProgress(t *testing.T) {
	pool := newMigratedPool(t)

	store, err := pgstore.New(pool, pgstore.Options{Scope: "claim-recheck", MaxAttempts: 1, AutomaticReplayHorizon: time.Hour})
	if err != nil {
		t.Fatal(err)
	}

	record := irisdurable.ReplyRecord{MessageID: "policy-message", Phase: "reply", RoomID: "policy-room", ClientRequestID: "policy-request", Payload: []byte(`{"text":"exact"}`)}
	if _, err = store.Stage(t.Context(), record); err != nil {
		t.Fatal(err)
	}

	candidates, err := store.Redrive(t.Context(), 10)
	if err != nil || len(candidates) != 1 {
		t.Fatalf("candidates=%v err=%v", candidates, err)
	}

	if _, err = pool.Exec(t.Context(), `UPDATE iris_reply_outbox SET first_attempt_at=now()-interval '1 hour' WHERE scope='claim-recheck'`); err != nil {
		t.Fatal(err)
	}

	if _, err = store.BeginAttempt(t.Context(), record.ReplyIdentity); !errors.Is(err, irisdurable.ErrReplyNotClaimable) {
		t.Fatalf("stale candidate claimed: %v", err)
	}

	if _, err = pool.Exec(t.Context(), `UPDATE iris_reply_outbox SET first_attempt_at=now() WHERE scope='claim-recheck'`); err != nil {
		t.Fatal(err)
	}

	first, err := store.BeginAttempt(t.Context(), record.ReplyIdentity)
	if err != nil {
		t.Fatal(err)
	}

	if _, err = store.BeginAttempt(t.Context(), record.ReplyIdentity); !errors.Is(err, irisdurable.ErrReplyNotClaimable) {
		t.Fatalf("active attempt reclaimed: %v", err)
	}

	if err = store.Settle(t.Context(), first, irisdurable.ReplyOutcome{Status: irisdurable.ReplyStatusOutcomeUnknown, Progressed: true}); err != nil {
		t.Fatal(err)
	}

	next, err := store.BeginAttempt(t.Context(), record.ReplyIdentity)
	if err != nil || next.Attempt != 1 || next.ClaimToken == first.ClaimToken {
		t.Fatalf("progress did not restore budget: %+v %v", next, err)
	}

	if err = store.Settle(t.Context(), first, irisdurable.ReplyOutcome{Status: irisdurable.ReplyStatusAccepted}); !errors.Is(err, pgstore.ErrClaimLost) {
		t.Fatalf("stale token settled: %v", err)
	}

	if err = store.Settle(t.Context(), next, irisdurable.ReplyOutcome{Status: irisdurable.ReplyStatusOutcomeUnknown}); err != nil {
		t.Fatal(err)
	}

	if _, err = store.BeginAttempt(t.Context(), record.ReplyIdentity); !errors.Is(err, irisdurable.ErrReplyNotClaimable) {
		t.Fatalf("stalled final attempt reclaimed: %v", err)
	}
}
