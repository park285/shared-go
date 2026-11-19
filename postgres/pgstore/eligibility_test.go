package pgstore_test

import (
	"context"
	"testing"
	"time"

	"github.com/park285/shared-go/postgres/pgstore"
	"github.com/park285/shared-go/postgres/pgxdb"
	"github.com/stretchr/testify/require"

	"github.com/park285/shared-go/v2/pkg/irisdurable"
)

const inboxRoom = "room"

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

			if tc.futurePredecessor {
				_, err = store.Admit(t.Context(), irisdurable.AdmissionInput{MessageID: "predecessor", OrderingKey: inboxRoom, Payload: []byte(`{}`)})
				require.NoError(t, err)

				_, err = tx.Exec(t.Context(), "UPDATE iris_webhook_inbox SET available_at = now() + interval '1 hour' WHERE scope = $1 AND message_id = 'predecessor'", tc.name)
				require.NoError(t, err)
			}

			_, err = store.Admit(t.Context(), irisdurable.AdmissionInput{MessageID: "candidate", OrderingKey: inboxRoom, Payload: []byte(`{}`)})
			require.NoError(t, err)

			_, err = tx.Exec(t.Context(), `UPDATE iris_webhook_inbox
SET created_at = now() - interval '1 hour', available_at = $2,
    status = CASE WHEN $3 THEN 'processing' ELSE 'pending' END,
    claim_token = CASE WHEN $3 THEN 'old-owner' ELSE NULL END,
    lease_until = CASE WHEN $3 THEN $4::timestamptz ELSE NULL END
WHERE scope = $1 AND message_id = 'candidate'`, tc.name, now.Add(tc.available), tc.processing, now.Add(tc.lease))
			require.NoError(t, err)

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

func TestInboxClaimDoesNotPassProcessingHeadAfterClockRollback(t *testing.T) {
	t.Run("first key", func(t *testing.T) { checkClockRollback(t, false) })
	t.Run("subsequent key", func(t *testing.T) { checkClockRollback(t, true) })
}

func checkClockRollback(t *testing.T, subsequentKey bool) {
	t.Helper()

	pool := newMigratedPool(t)
	store := newScopedStoreWithOptions(t, pool, pgstore.Options{Scope: "clock-rollback", InboxExplicitRecovery: true})
	ctx := t.Context()

	if subsequentKey {
		_, err := pool.Exec(ctx, `INSERT INTO iris_webhook_inbox (scope, message_id, ordering_key, payload, available_at)
VALUES ($1, 'earlier-key', 'a', '{}', 'infinity')`, store.Options().Scope)
		require.NoError(t, err)
	}

	input := irisdurable.AdmissionInput{MessageID: "first", OrderingKey: inboxRoom, Payload: []byte(`{}`)}
	_, err := store.Admit(ctx, input)
	require.NoError(t, err)

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)

	defer func() { require.NoError(t, pgxdb.Rollback(ctx, tx)) }()

	_, err = tx.Exec(ctx, `
CREATE SCHEMA earlier_clock;
CREATE FUNCTION earlier_clock.clock_timestamp() RETURNS timestamptz LANGUAGE SQL
RETURN (SELECT created_at - interval '1 microsecond' FROM public.iris_webhook_inbox WHERE message_id = 'first');
SET LOCAL search_path = earlier_clock, pg_catalog, public`)
	require.NoError(t, err)

	writer, err := pgstore.New(tx, store.Options())
	require.NoError(t, err)

	input.MessageID = "second"
	_, err = writer.Admit(ctx, input)
	require.NoError(t, err)

	first, ok, err := store.Claim(ctx)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "first", first.MessageID)

	pinned, err := pool.Exec(ctx, `UPDATE iris_webhook_inbox SET lease_until = 'infinity'
WHERE id = $1 AND claim_token = $2`, first.ID, first.ClaimToken)
	require.NoError(t, err)
	require.EqualValues(t, 1, pinned.RowsAffected())
	require.NoError(t, tx.Commit(ctx))

	var (
		secondID      int64
		secondCreated time.Time
	)

	err = pool.QueryRow(ctx, `SELECT id, created_at FROM iris_webhook_inbox WHERE scope = $1 AND message_id = 'second'`,
		store.Options().Scope).Scan(&secondID, &secondCreated)
	require.NoError(t, err)
	require.Greater(t, secondID, first.ID)
	require.True(t, secondCreated.Before(first.CreatedAt))

	snapshot, err := store.InboxReadySnapshot(ctx)
	require.NoError(t, err)

	claims, err := store.ClaimBatch(ctx, 2)
	require.NoError(t, err)
	require.Empty(t, claims, "second must not pass first before settlement")
	require.Zero(t, snapshot.Ready)
	require.NoError(t, store.Complete(ctx, first, ""))

	second, ok, err := store.Claim(ctx)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, secondID, second.ID)
}

func TestDeferredInboxHeadYieldsToEarlierDueKey(t *testing.T) {
	pool := newMigratedPool(t)
	store := newScopedStore(t, pool)
	ctx := t.Context()

	for _, key := range []string{"a", "b"} {
		_, err := store.Admit(ctx, irisdurable.AdmissionInput{MessageID: key, OrderingKey: key, Payload: []byte(`{}`)})
		require.NoError(t, err)
	}

	_, err := pool.Exec(ctx, `UPDATE iris_webhook_inbox SET available_at =
CASE message_id WHEN 'a' THEN to_timestamp(0) ELSE to_timestamp(1) END`)
	require.NoError(t, err)

	first, ok, err := store.Claim(ctx)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "a", first.MessageID)
	require.NoError(t, store.DeferInboxUntil(ctx, first, time.Unix(2, 0)))

	next, ok, err := store.Claim(ctx)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "b", next.MessageID)
}

func TestInboxClaimBreaksTiesBetweenDueKeys(t *testing.T) {
	for _, tc := range []struct {
		name          string
		secondCreated time.Time
		want          string
	}{
		{"earlier creation wins over smaller ID", time.Unix(0, 0), "b"},
		{"smaller ID wins at equal creation", time.Unix(1, 0), "a"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pool := newMigratedPool(t)
			store := newScopedStore(t, pool)
			ctx := t.Context()

			_, err := pool.Exec(ctx, `INSERT INTO iris_webhook_inbox
(id, scope, message_id, ordering_key, payload, available_at, created_at)
VALUES (1, $1, 'a', 'a', '{}', to_timestamp(0), to_timestamp(1)),
       (2, $1, 'b', 'b', '{}', to_timestamp(0), $2)`, store.Options().Scope, tc.secondCreated)
			require.NoError(t, err)

			claim, ok, err := store.Claim(ctx)
			require.NoError(t, err)
			require.True(t, ok)
			require.Equal(t, tc.want, claim.MessageID)
		})
	}
}

func TestAdmitWaitsForOrderingLockBeforeTakingSequenceNumber(t *testing.T) {
	pool := newMigratedPool(t)
	ctx := t.Context()
	holder, err := pool.Begin(ctx)
	require.NoError(t, err)

	defer func() { require.NoError(t, pgxdb.Rollback(ctx, holder)) }()

	opts := testPolicy(pgstore.Options{Scope: "admission-fence"})
	first, err := pgstore.New(holder, opts)
	require.NoError(t, err)

	_, err = holder.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtext($1), hashtext($2))", opts.Scope, inboxRoom)
	require.NoError(t, err)

	conn, err := pool.Acquire(ctx)
	require.NoError(t, err)

	defer conn.Release()

	second, err := pgstore.New(conn, opts)
	require.NoError(t, err)

	waitCtx, cancel := context.WithTimeout(ctx, 10*time.Second)

	var admitErr error

	pid := conn.Conn().PgConn().PID()
	done := make(chan struct{})

	go func() {
		defer close(done)

		_, admitErr = second.Admit(waitCtx, irisdurable.AdmissionInput{MessageID: "waiter", OrderingKey: inboxRoom, Payload: []byte(`{}`)})
	}()

	defer func() {
		cancel()
		<-done
	}()

	for {
		var blocked bool

		err = holder.QueryRow(waitCtx, "SELECT pg_backend_pid() = ANY(pg_blocking_pids($1))", pid).Scan(&blocked)
		require.NoError(t, err)

		if blocked {
			break
		}

		select {
		case <-done:
			t.Fatalf("second admission finished without waiting for the ordering lock: %v", admitErr)
		default:
		}
	}

	var issued bool

	require.NoError(t, holder.QueryRow(ctx, "SELECT is_called FROM iris_webhook_inbox_id_seq").Scan(&issued))
	require.False(t, issued, "waiting admission consumed the first sequence number before acquiring the lock")

	_, err = first.Admit(ctx, irisdurable.AdmissionInput{MessageID: "holder", OrderingKey: inboxRoom, Payload: []byte(`{}`)})
	require.NoError(t, err)
	require.NoError(t, holder.Commit(ctx))
	<-done
	require.NoError(t, admitErr)

	var holderID, waiterID int64

	require.NoError(t, pool.QueryRow(ctx, `SELECT h.id, w.id FROM iris_webhook_inbox h JOIN iris_webhook_inbox w
ON h.scope = w.scope AND h.ordering_key = w.ordering_key WHERE h.message_id = 'holder' AND w.message_id = 'waiter'`).Scan(&holderID, &waiterID))
	require.Greater(t, waiterID, holderID)
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
		{MessageID: "head", OrderingKey: inboxRoom, Payload: []byte(`{}`)},
		{MessageID: "follower", OrderingKey: inboxRoom, Payload: []byte(`{}`)},
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
