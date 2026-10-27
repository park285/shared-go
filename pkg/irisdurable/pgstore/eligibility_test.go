package pgstore_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/park285/shared-go/v2/pkg/db/pgxdb"
	"github.com/park285/shared-go/v2/pkg/irisdurable"
	"github.com/park285/shared-go/v2/pkg/irisdurable/pgstore"
)

var inboxReadinessCases = []struct {
	name              string
	available         time.Duration
	processing        bool
	lease             time.Duration
	explicitRecovery  bool
	futurePredecessor bool
	wantReady         int64
	wantAge           time.Duration
}{
	{name: "exact due boundary", wantReady: 1},
	{name: "recent retry keeps original admission age", available: -time.Second, wantReady: 1, wantAge: time.Second},
	{name: "future retry", available: time.Microsecond},
	{name: "lease expiry inclusive", available: -time.Minute, processing: true, wantReady: 1},
	{name: "expired lease age", available: -time.Minute, processing: true, lease: -time.Second, wantReady: 1, wantAge: time.Second},
	{name: "schedule later than expired lease", available: -time.Second, processing: true, lease: -time.Minute, wantReady: 1, wantAge: time.Second},
	{name: "live lease", processing: true, lease: time.Microsecond},
	{name: "explicit recovery excludes expired lease", processing: true, lease: -time.Minute, explicitRecovery: true},
	{name: "future head blocks due follower", futurePredecessor: true},
}

func TestInboxReadySnapshotMatchesClaimAtScheduledAndLeaseBoundaries(t *testing.T) {
	pool := newMigratedPool(t)

	for _, tc := range inboxReadinessCases {
		t.Run(tc.name, func(t *testing.T) {
			tx, err := pool.Begin(t.Context())
			require.NoError(t, err)

			defer func() { require.NoError(t, pgxdb.Rollback(t.Context(), tx)) }()

			store, err := pgstore.New(tx, testPolicy(pgstore.Options{Scope: tc.name, InboxExplicitRecovery: tc.explicitRecovery}))
			require.NoError(t, err)

			var now time.Time

			require.NoError(t, tx.QueryRow(t.Context(), "SELECT now()").Scan(&now))

			_, err = store.Admit(t.Context(), irisdurable.AdmissionInput{MessageID: "candidate", OrderingKey: "room", Payload: []byte(`{}`)})
			require.NoError(t, err)

			_, err = tx.Exec(t.Context(), `UPDATE iris_webhook_inbox
SET created_at = now() - interval '1 hour', available_at = $2,
    status = CASE WHEN $3 THEN 'processing' ELSE 'pending' END,
    claim_token = CASE WHEN $3 THEN 'old-owner' ELSE NULL END,
    lease_until = CASE WHEN $3 THEN $4::timestamptz ELSE NULL END
WHERE scope = $1`, tc.name, now.Add(tc.available), tc.processing, now.Add(tc.lease))
			require.NoError(t, err)

			if tc.futurePredecessor {
				_, err = store.Admit(t.Context(), irisdurable.AdmissionInput{MessageID: "predecessor", OrderingKey: "room", Payload: []byte(`{}`)})
				require.NoError(t, err)

				_, err = tx.Exec(t.Context(), "UPDATE iris_webhook_inbox SET created_at = now() - interval '2 hours', available_at = now() + interval '1 hour' WHERE scope = $1 AND message_id = 'predecessor'", tc.name)
				require.NoError(t, err)
			}

			snapshot, err := store.InboxReadySnapshot(t.Context())
			require.NoError(t, err)
			require.Equal(t, tc.wantReady, snapshot.Ready)
			require.Equal(t, tc.wantAge, snapshot.OldestAge)

			claims, err := store.ClaimBatch(t.Context(), 10)
			require.NoError(t, err)
			require.Len(t, claims, int(snapshot.Ready))

			if len(claims) != 0 {
				require.Equal(t, tc.wantAge, now.Sub(claims[0].EligibleAt))
				require.Equal(t, time.Hour, now.Sub(claims[0].CreatedAt))

				empty, err := store.InboxReadySnapshot(t.Context())
				require.NoError(t, err)
				require.Zero(t, empty.Ready)
				require.Zero(t, empty.OldestAge)
			}
		})
	}
}

var replyReadinessCases = []struct {
	name           string
	available      time.Duration
	leased         bool
	lease          time.Duration
	expired        bool
	exhausted      bool
	replayBoundary bool
	predecessor    bool
	wantReady      int64
	wantAge        time.Duration
}{
	{name: "exact due boundary", wantReady: 1},
	{name: "recent retry", available: -time.Second, wantReady: 1, wantAge: time.Second},
	{name: "future retry", available: time.Microsecond},
	{name: "lease expiry inclusive", available: -time.Minute, leased: true, wantReady: 1},
	{name: "expired lease age", available: -time.Minute, leased: true, lease: -time.Second, wantReady: 1, wantAge: time.Second},
	{name: "live lease", leased: true, lease: time.Microsecond},
	{name: "expiry equality excluded", expired: true},
	{name: "attempt limit equality excluded", exhausted: true},
	{name: "replay horizon equality excluded", replayBoundary: true},
	{name: "unaccepted predecessor blocks successor", predecessor: true},
}

func TestReplyReadySnapshotMatchesRedriveAndAttemptBoundaries(t *testing.T) {
	pool := newMigratedPool(t)

	for _, tc := range replyReadinessCases {
		t.Run(tc.name, func(t *testing.T) {
			tx, err := pool.Begin(t.Context())
			require.NoError(t, err)

			defer func() { require.NoError(t, pgxdb.Rollback(t.Context(), tx)) }()

			store, err := pgstore.New(tx, testPolicy(pgstore.Options{Scope: tc.name}))
			require.NoError(t, err)

			var now time.Time

			require.NoError(t, tx.QueryRow(t.Context(), "SELECT now()").Scan(&now))

			record := (&replyFixture{Store: store}).NewRecord(t, []byte(`{"text":"ready"}`))

			if tc.predecessor {
				_, err = store.Stage(t.Context(), record)
				require.NoError(t, err)

				record.Ordinal++

				record.ClientRequestID += ":next"
			}

			_, err = store.Stage(t.Context(), record)
			require.NoError(t, err)

			_, err = tx.Exec(t.Context(), `UPDATE iris_reply_outbox
SET created_at = now() - interval '1 hour', available_at = $2,
    claim_token = CASE WHEN $3 THEN 'old-owner' ELSE NULL END,
    lease_until = CASE WHEN $3 THEN $4::timestamptz ELSE NULL END,
    first_attempt_at = now() - CASE WHEN $5::boolean THEN $6::bigint * interval '1 microsecond' ELSE interval '1 hour' END,
    expires_at = now() + CASE WHEN $7::boolean THEN interval '0 seconds' ELSE interval '1 hour' END,
    attempts = CASE WHEN $8::boolean THEN $9::integer ELSE 0 END
WHERE scope = $1 AND ordinal = $10`,
				tc.name, now.Add(tc.available), tc.leased, now.Add(tc.lease), tc.replayBoundary,
				store.Options().AutomaticReplayHorizon.Microseconds(), tc.expired, tc.exhausted,
				store.Options().MaxAttempts, record.Ordinal)
			require.NoError(t, err)

			if tc.predecessor {
				_, err = tx.Exec(t.Context(), "UPDATE iris_reply_outbox SET available_at = now() + interval '1 hour' WHERE scope = $1 AND ordinal < $2", tc.name, record.Ordinal)
				require.NoError(t, err)
			}

			snapshot, err := store.ReplyReadySnapshot(t.Context())
			require.NoError(t, err)
			require.Equal(t, tc.wantReady, snapshot.Ready)
			require.Equal(t, tc.wantAge, snapshot.OldestAge)

			candidates, err := store.Redrive(t.Context(), 10)
			require.NoError(t, err)
			require.Len(t, candidates, int(snapshot.Ready))

			_, err = store.BeginAttempt(t.Context(), record.ReplyIdentity)

			if tc.wantReady == 0 {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestInboxReadySnapshotKeepsLockedHeadsWithoutAllowingOvertake(t *testing.T) {
	pool := newMigratedPool(t)
	store := newScopedStore(t, pool)

	for _, input := range []irisdurable.AdmissionInput{
		{MessageID: "head", OrderingKey: "room", Payload: []byte(`{}`)},
		{MessageID: "follower", OrderingKey: "room", Payload: []byte(`{}`)},
		{MessageID: "independent", OrderingKey: "other-room", Payload: []byte(`{}`)},
	} {
		_, err := store.Admit(t.Context(), input)
		require.NoError(t, err)
	}

	tx, err := pool.Begin(t.Context())
	require.NoError(t, err)

	defer func() { require.NoError(t, pgxdb.Rollback(t.Context(), tx)) }()

	var headID int64

	require.NoError(t, tx.QueryRow(t.Context(), "SELECT id FROM iris_webhook_inbox WHERE scope = $1 AND message_id = 'head' FOR UPDATE", store.Options().Scope).Scan(&headID))

	snapshot, err := store.InboxReadySnapshot(t.Context())
	require.NoError(t, err)
	require.EqualValues(t, 2, snapshot.Ready, "logical eligibility includes the locked head, not its successor")

	claims, err := store.ClaimBatch(t.Context(), 10)
	require.NoError(t, err)
	require.Len(t, claims, 1)
	require.Equal(t, "independent", claims[0].MessageID)
	require.NoError(t, pgxdb.Rollback(t.Context(), tx))

	claim, ok, err := store.Claim(t.Context())
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, headID, claim.ID)
	require.False(t, claim.EligibleAt.IsZero())
}
