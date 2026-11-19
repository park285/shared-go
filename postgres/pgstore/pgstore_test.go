package pgstore_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/park285/shared-go/postgres/pgstore"
	"github.com/park285/shared-go/postgres/pgxdb"

	"github.com/park285/shared-go/v2/pkg/irisdurable"
	"github.com/park285/shared-go/v2/pkg/irisdurable/contracttest"
	"github.com/park285/shared-go/v2/pkg/workercontract"
)

const (
	testDatabaseURLEnv     = "TEST_DATABASE_URL"
	allowExternalTestDBEnv = "ALLOW_EXTERNAL_TEST_DB"
)

var (
	scopeCounter  atomic.Uint64
	suffixPattern = regexp.MustCompile(`:r\d+$`)

	errPreHandoff  = errors.New("pgstore test transport: CLIENT_REQUEST_ID_FAILED")
	errTerminal409 = errors.New("pgstore test transport: CLIENT_REQUEST_ID_OUTCOME_UNKNOWN")
	errNestedBase  = errors.New("pgstore test ladder: base already reissued")
)

func TestRunAgainstPostgres(t *testing.T) {
	pool := newMigratedPool(t)

	contracttest.Run(t, contracttest.Suite{
		Admitter: func(t *testing.T) irisdurable.Admitter {
			t.Helper()

			return newScopedStore(t, pool)
		},
		NonceStore: func(t *testing.T) irisdurable.NonceStore {
			t.Helper()

			return newScopedStore(t, pool)
		},
		NonceExpiry: time.Second,
		ReplyOutbox: func(t *testing.T) contracttest.ReplyOutboxFixture {
			t.Helper()

			return &replyFixture{Store: newScopedStore(t, pool)}
		},
		Reissue: &contracttest.ReissueFixture{
			Ladder:                irisdurable.ReissueLadder{MaxGenerations: 2, Derive: testDerive},
			PreHandoffConflict:    func(err error) bool { return errors.Is(err, errPreHandoff) },
			NewPreHandoffConflict: func() error { return errPreHandoff },
			NewTerminalConflict:   func() error { return errTerminal409 },
		},
		Retention: retentionFixture(t),
	})
}

func TestReferenceSchemaIsRepeatable(t *testing.T) {
	pool := newBlankPool(t)

	for range 2 {
		applyReferenceSchema(t, pool)
	}

	for _, table := range []string{"iris_webhook_inbox", "iris_nonce", "iris_reply_outbox"} {
		var exists bool

		if err := pool.QueryRow(t.Context(), "SELECT to_regclass($1) IS NOT NULL", table).Scan(&exists); err != nil {
			t.Fatalf("check table %s: %v", table, err)
		}

		if !exists {
			t.Fatalf("table %s was not created", table)
		}
	}
}

func TestAdmitClassifiesDeterministicRejections(t *testing.T) {
	pool := newMigratedPool(t)
	store := newScopedStore(t, pool)
	ctx := t.Context()

	cases := map[string][]byte{
		"payload is a JSON array, not an object": []byte(`["not","an","object"]`),
		"payload is not JSON at all":             []byte(`{`),
	}

	for name, payload := range cases {
		t.Run(name, func(t *testing.T) {
			result, err := store.Admit(ctx, irisdurable.AdmissionInput{
				MessageID:   fmt.Sprintf("msg-reject-%d", scopeCounter.Add(1)),
				OrderingKey: "room-reject",
				Payload:     payload,
			})
			if err == nil {
				t.Fatal("admit with an unstorable payload must fail")
			}

			if result != workercontract.AdmissionRejected {
				t.Fatalf("admit result = %q; want %q, because the server rejected the statement and nothing was committed", result, workercontract.AdmissionRejected)
			}
		})
	}
}

func TestInboxOrderingKeyServesOneMessageAtATime(t *testing.T) {
	pool := newMigratedPool(t)
	store := newScopedStore(t, pool)
	ctx := t.Context()
	orderingKey := "room-" + strconv.FormatUint(scopeCounter.Add(1), 10)

	for i := range 2 {
		input := irisdurable.AdmissionInput{
			MessageID:   fmt.Sprintf("msg-%s-%d", orderingKey, i),
			OrderingKey: orderingKey,
			Payload:     fmt.Appendf(nil, `{"ordinal":%d}`, i),
		}
		if _, err := store.Admit(ctx, input); err != nil {
			t.Fatalf("admit %s: %v", input.MessageID, err)
		}
	}

	first, claimed, err := store.Claim(ctx)
	if err != nil || !claimed {
		t.Fatalf("first claim = (%v, %v, %v); want a claimed message", first, claimed, err)
	}

	_, claimedAgain, claimAgainErr := store.Claim(ctx)
	if claimAgainErr != nil || claimedAgain {
		t.Fatalf("second claim ok = %v (err %v); want no claim while the ordering key head is held", claimedAgain, claimAgainErr)
	}

	if completeErr := store.Complete(ctx, first, ""); completeErr != nil {
		t.Fatalf("complete first: %v", completeErr)
	}

	second, claimed, err := store.Claim(ctx)
	if err != nil || !claimed {
		t.Fatalf("claim after completion = (%v, %v, %v); want the next message", second, claimed, err)
	}

	if second.MessageID == first.MessageID {
		t.Fatalf("claimed %s twice; want the following message", second.MessageID)
	}
}

func TestInboxRenewAndRelease(t *testing.T) {
	pool := newMigratedPool(t)
	store := newScopedStore(t, pool)
	ctx := t.Context()
	orderingKey := "room-renew-" + strconv.FormatUint(scopeCounter.Add(1), 10)

	admission := irisdurable.AdmissionInput{
		MessageID:   "msg-" + orderingKey,
		OrderingKey: orderingKey,
		Payload:     []byte(`{"kind":"renew"}`),
	}
	if _, err := store.Admit(ctx, admission); err != nil {
		t.Fatalf("admit: %v", err)
	}

	first, ok, err := store.Claim(ctx)
	if err != nil || !ok {
		t.Fatalf("claim = (%v, %v); want a claimed message", ok, err)
	}

	before := leaseUntil(t, pool, first.ID)

	if renewErr := store.RenewInbox(ctx, first); renewErr != nil {
		t.Fatalf("renew: %v", renewErr)
	}

	if after := leaseUntil(t, pool, first.ID); !after.After(before) {
		t.Fatalf("lease_until %s did not move past %s", after, before)
	}

	if releaseErr := store.Release(ctx, first, 0); releaseErr != nil {
		t.Fatalf("release without a delay: %v", releaseErr)
	}

	second, ok, err := store.Claim(ctx)
	if err != nil || !ok {
		t.Fatalf("claim after an immediate release = (%v, %v); want the same message", ok, err)
	}

	if second.Attempts != first.Attempts+1 {
		t.Fatalf("attempts after reclaim = %d; want %d", second.Attempts, first.Attempts+1)
	}

	if renewErr := store.RenewInbox(ctx, first); !errors.Is(renewErr, pgstore.ErrClaimLost) {
		t.Fatalf("renew with the superseded token = %v; want ErrClaimLost", renewErr)
	}

	if releaseErr := store.Release(ctx, second, time.Hour); releaseErr != nil {
		t.Fatalf("release with a delay: %v", releaseErr)
	}

	if _, claimed, claimErr := store.Claim(ctx); claimErr != nil || claimed {
		t.Fatalf("claim during the retry delay = (%v, %v); want no claim", claimed, claimErr)
	}
}

func TestAdmitSerializesOrderingKeyAcrossTransactions(t *testing.T) {
	pool := newMigratedPool(t)
	store := newScopedStore(t, pool)
	ctx := t.Context()
	orderingKey := "room-serialize-" + strconv.FormatUint(scopeCounter.Add(1), 10)
	firstID := "msg-" + orderingKey + "-first"

	tx := beginAdmittedTx(t, pool, store.Options().Scope, firstID, orderingKey)
	second := admitAsync(store, "msg-"+orderingKey+"-second", orderingKey)

	select {
	case admitErr := <-second:
		t.Fatalf("the second admit returned (%v) while the first was uncommitted; the ordering key was not serialized", admitErr)
	case <-time.After(500 * time.Millisecond):
	}

	if commitErr := tx.Commit(ctx); commitErr != nil {
		t.Fatalf("commit the first admit: %v", commitErr)
	}

	select {
	case admitErr := <-second:
		if admitErr != nil {
			t.Fatalf("the second admit after the commit: %v", admitErr)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("the second admit never finished after the first transaction committed")
	}

	claim, claimed, err := store.Claim(ctx)
	if err != nil || !claimed {
		t.Fatalf("claim = (%v, %v); want the first message", claimed, err)
	}

	if claim.MessageID != firstID {
		t.Fatalf("claimed %s; want %s, the message admitted first", claim.MessageID, firstID)
	}
}

func beginAdmittedTx(t *testing.T, pool *pgxpool.Pool, scope, messageID, orderingKey string) pgx.Tx {
	t.Helper()

	tx, err := pool.Begin(t.Context())
	if err != nil {
		t.Fatalf("begin: %v", err)
	}

	t.Cleanup(func() {
		if rollbackErr := pgxdb.Rollback(t.Context(), tx); rollbackErr != nil {
			t.Errorf("rollback: %v", rollbackErr)
		}
	})

	store, err := pgstore.New(tx, testPolicy(pgstore.Options{Scope: scope}))
	if err != nil {
		t.Fatalf("new store on the transaction: %v", err)
	}

	if _, admitErr := store.Admit(t.Context(), irisdurable.AdmissionInput{
		MessageID:   messageID,
		OrderingKey: orderingKey,
		Payload:     []byte(`{"ordinal":0}`),
	}); admitErr != nil {
		t.Fatalf("admit inside the open transaction: %v", admitErr)
	}

	return tx
}

func admitAsync(store *pgstore.Store, messageID, orderingKey string) <-chan error {
	done := make(chan error, 1)

	go func() {
		_, err := store.Admit(context.Background(), irisdurable.AdmissionInput{
			MessageID:   messageID,
			OrderingKey: orderingKey,
			Payload:     []byte(`{"ordinal":1}`),
		})
		done <- err
	}()

	return done
}

func TestPruneNoncePreservesConcurrentRefresh(t *testing.T) {
	pool := newMigratedPool(t)
	store := newScopedStore(t, pool)
	ctx := t.Context()
	key := "nonce-refresh-" + strconv.FormatUint(scopeCounter.Add(1), 10)

	if _, err := pool.Exec(ctx, `
		INSERT INTO iris_nonce (scope, nonce_key, expires_at, created_at)
		VALUES ($1, $2, now() - interval '1 hour', now() - interval '2 hours')
	`, store.Options().Scope, key); err != nil {
		t.Fatalf("insert expired nonce: %v", err)
	}

	tx := beginExpiredNonceRefresh(t, pool, store.Options(), key)

	deleted, err := pruneNonceWithin(ctx, store, 5*time.Second)
	if err != nil {
		t.Fatalf("prune while nonce refresh is open: %v", err)
	}

	if deleted != 0 {
		t.Fatalf("pruned %d nonces; want the refreshing nonce skipped", deleted)
	}

	if commitErr := tx.Commit(ctx); commitErr != nil {
		t.Fatalf("commit nonce refresh: %v", commitErr)
	}

	duplicate, err := store.IsDuplicate(ctx, key, time.Hour)
	if err != nil {
		t.Fatalf("observe refreshed nonce: %v", err)
	}

	if !duplicate {
		t.Fatal("refreshed nonce was removed by pruning and accepted again")
	}
}

func beginExpiredNonceRefresh(t *testing.T, pool *pgxpool.Pool, opts pgstore.Options, key string) pgx.Tx {
	t.Helper()

	tx, err := pool.Begin(t.Context())
	if err != nil {
		t.Fatalf("begin nonce refresh: %v", err)
	}

	t.Cleanup(func() {
		if rollbackErr := pgxdb.Rollback(t.Context(), tx); rollbackErr != nil {
			t.Errorf("rollback nonce refresh: %v", rollbackErr)
		}
	})

	txStore, err := pgstore.New(tx, opts)
	if err != nil {
		t.Fatalf("new store on nonce refresh transaction: %v", err)
	}

	duplicate, err := txStore.IsDuplicate(t.Context(), key, time.Hour)
	if err != nil {
		t.Fatalf("refresh expired nonce: %v", err)
	}

	if duplicate {
		t.Fatal("expired nonce refresh was reported as a duplicate")
	}

	return tx
}

func pruneNonceWithin(ctx context.Context, store *pgstore.Store, timeout time.Duration) (int64, error) {
	pruneCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	deleted, err := store.PruneNonce(pruneCtx, 1)
	if err != nil {
		return 0, fmt.Errorf("prune nonce: %w", err)
	}

	return deleted, nil
}

func TestPruneNonceHonorsExpiryLimitAndScope(t *testing.T) {
	pool := newMigratedPool(t)
	store := newScopedStore(t, pool)
	neighbor := newScopedStore(t, pool)
	ctx := t.Context()
	keyPrefix := "nonce-prune-" + strconv.FormatUint(scopeCounter.Add(1), 10)
	oldestKey := keyPrefix + "-oldest"
	middleKey := keyPrefix + "-middle"
	newestKey := keyPrefix + "-newest"
	liveKey := keyPrefix + "-live"

	if _, err := pool.Exec(ctx, `
		INSERT INTO iris_nonce (scope, nonce_key, expires_at, created_at)
		VALUES
			($1, $3, now() - interval '4 hours', now()),
			($1, $4, now() - interval '3 hours', now()),
			($1, $5, now() - interval '2 hours', now()),
			($1, $6, now() + interval '1 hour', now()),
			($2, $3, now() - interval '4 hours', now())
	`, store.Options().Scope, neighbor.Options().Scope, oldestKey, middleKey, newestKey, liveKey); err != nil {
		t.Fatalf("insert nonce pruning fixtures: %v", err)
	}

	deleted, err := store.PruneNonce(ctx, 2)
	if err != nil {
		t.Fatalf("prune expired nonces: %v", err)
	}

	if deleted != 2 {
		t.Fatalf("pruned %d nonces; want the two oldest expired rows", deleted)
	}

	if nonceExists(t, pool, store.Options().Scope, oldestKey) || nonceExists(t, pool, store.Options().Scope, middleKey) {
		t.Fatal("the two oldest expired nonces were not pruned")
	}

	if !nonceExists(t, pool, store.Options().Scope, newestKey) {
		t.Fatal("pruning crossed its limit and removed a third expired nonce")
	}

	if !nonceExists(t, pool, store.Options().Scope, liveKey) {
		t.Fatal("pruning removed a live nonce")
	}

	if !nonceExists(t, pool, neighbor.Options().Scope, oldestKey) {
		t.Fatal("pruning removed an expired nonce from another scope")
	}
}

func nonceExists(t *testing.T, pool *pgxpool.Pool, scope, key string) bool {
	t.Helper()

	var exists bool

	if err := pool.QueryRow(t.Context(), `
		SELECT EXISTS (
			SELECT 1
			FROM iris_nonce
			WHERE scope = $1
			  AND nonce_key = $2
		)
	`, scope, key).Scan(&exists); err != nil {
		t.Fatalf("check nonce %s: %v", key, err)
	}

	return exists
}

func TestReplyRedriveAndRetire(t *testing.T) {
	pool := newMigratedPool(t)
	store := newScopedStore(t, pool)
	fixture := &replyFixture{Store: store}
	ctx := t.Context()

	record := fixture.NewRecord(t, []byte(`{"type":"text","text":"redrive"}`))
	if _, err := store.Stage(ctx, record); err != nil {
		t.Fatalf("stage: %v", err)
	}

	candidates, err := store.Redrive(ctx, 10)
	if err != nil {
		t.Fatalf("redrive: %v", err)
	}

	if len(candidates) != 1 || candidates[0].MessageID != record.MessageID {
		t.Fatalf("redrive candidates = %+v; want the staged record", candidates)
	}

	attempt, err := store.BeginAttempt(ctx, record.ReplyIdentity)
	if err != nil {
		t.Fatalf("begin attempt: %v", err)
	}

	if settleErr := store.Settle(ctx, attempt, irisdurable.ReplyOutcome{
		Status:          irisdurable.ReplyStatusOutcomeUnknown,
		ClientRequestID: attempt.ClientRequestID,
	}); settleErr != nil {
		t.Fatalf("settle: %v", settleErr)
	}

	retired, err := retireAndCommit(t, pool, store.Options())
	if err != nil {
		t.Fatalf("retire: %v", err)
	}

	if len(retired) != 0 {
		t.Fatalf("retired %d rows; want none while attempts remain", len(retired))
	}

	exhausted := newScopedStoreWithOptions(t, pool, pgstore.Options{MaxAttempts: 1, Scope: store.Options().Scope})

	retired, err = retireAndCommit(t, pool, exhausted.Options())
	if err != nil {
		t.Fatalf("retire with exhausted attempts: %v", err)
	}

	if len(retired) != 1 {
		t.Fatalf("retired %d rows; want the attempt-exhausted row", len(retired))
	}

	state, err := store.Inspect(ctx, record.ReplyIdentity)
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}

	if state.Status != irisdurable.ReplyStatusDead || state.PayloadPresent {
		t.Fatalf("state after retire = %+v; want dead with the payload scrubbed", state)
	}
}

func TestRetireLeavesInFlightAttemptAlone(t *testing.T) {
	pool := newMigratedPool(t)
	store := newScopedStoreWithOptions(t, pool, pgstore.Options{
		Scope:       fmt.Sprintf("test-%d-%d", time.Now().UnixNano(), scopeCounter.Add(1)),
		MaxAttempts: 1,
		Lease:       time.Millisecond,
	})
	fixture := &replyFixture{Store: store}
	ctx := t.Context()

	record := fixture.NewRecord(t, []byte(`{"type":"text","text":"in flight"}`))
	if _, err := store.Stage(ctx, record); err != nil {
		t.Fatalf("stage: %v", err)
	}

	attempt, err := store.BeginAttempt(ctx, record.ReplyIdentity)
	if err != nil {
		t.Fatalf("begin attempt: %v", err)
	}

	retired, err := retireAndCommit(t, pool, store.Options())
	if err != nil {
		t.Fatalf("retire while the attempt is in flight: %v", err)
	}

	if len(retired) != 0 {
		t.Fatalf("retired %d rows; want none while the attempt has not settled", len(retired))
	}

	state, err := store.Inspect(ctx, record.ReplyIdentity)
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}

	if state.Status != irisdurable.ReplyStatusSubmitting || !state.PayloadPresent {
		t.Fatalf("state during the attempt = %+v; want submitting with the payload intact", state)
	}

	if settleErr := store.Settle(ctx, attempt, irisdurable.ReplyOutcome{
		Status:          irisdurable.ReplyStatusOutcomeUnknown,
		ClientRequestID: attempt.ClientRequestID,
	}); settleErr != nil {
		t.Fatalf("settle after the lease lapsed: %v", settleErr)
	}

	retired, err = retireAndCommit(t, pool, store.Options())
	if err != nil {
		t.Fatalf("retire after the attempt settled: %v", err)
	}

	if len(retired) != 1 {
		t.Fatalf("retired %d rows; want the settled attempt-exhausted row", len(retired))
	}
}

type replyFixture struct {
	*pgstore.Store
}

func (f *replyFixture) NewRecord(t *testing.T, payload []byte) irisdurable.ReplyRecord {
	t.Helper()

	id := fmt.Sprintf("msg-%d-%d", time.Now().UnixNano(), scopeCounter.Add(1))

	return irisdurable.ReplyRecord{
		MessageID: id, Phase: "reply",
		RoomID:          "room-" + id,
		ClientRequestID: "crid." + id,
		Payload:         payload,
	}
}

func leaseUntil(t *testing.T, pool *pgxpool.Pool, id int64) time.Time {
	t.Helper()

	var lease time.Time

	if err := pool.QueryRow(t.Context(), "SELECT lease_until FROM iris_webhook_inbox WHERE id = $1", id).Scan(&lease); err != nil {
		t.Fatalf("read lease_until of %d: %v", id, err)
	}

	return lease
}

func retentionFixture(t *testing.T) *contracttest.RetentionFixture {
	t.Helper()

	opts := testPolicy(pgstore.Options{})

	return &contracttest.RetentionFixture{
		IrisAdmissionRetention:     opts.IrisAdmissionRetention,
		IrisAutomaticReplayHorizon: opts.IrisAutomaticReplayHorizon,
		ReplyOutboxRetention:       opts.ReplyRetention,
		AutomaticReplayHorizon:     opts.AutomaticReplayHorizon,
		InboxTerminalRetention:     opts.InboxTerminalRetention,
	}
}

func newScopedStore(t *testing.T, pool *pgxpool.Pool) *pgstore.Store {
	t.Helper()

	return newScopedStoreWithOptions(t, pool, pgstore.Options{
		Scope: fmt.Sprintf("test-%d-%d", time.Now().UnixNano(), scopeCounter.Add(1)),
	})
}

func newScopedStoreWithOptions(t *testing.T, pool *pgxpool.Pool, opts pgstore.Options) *pgstore.Store {
	t.Helper()

	store, err := pgstore.New(pool, testPolicy(opts))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	return store
}

func newMigratedPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	pool := newBlankPool(t)
	applyReferenceSchema(t, pool)

	return pool
}

func applyReferenceSchema(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	schema, err := os.ReadFile(filepath.Join("testdata", "schema.sql"))
	if err != nil {
		t.Fatalf("read reference schema: %v", err)
	}

	if _, err := pool.Exec(t.Context(), string(schema)); err != nil {
		t.Fatalf("apply reference schema: %v", err)
	}
}

func newBlankPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	baseDSN := os.Getenv(testDatabaseURLEnv)
	if baseDSN == "" || os.Getenv(allowExternalTestDBEnv) != "true" {
		t.Skipf("set %s and %s=true to run the PostgreSQL durability contract", testDatabaseURLEnv, allowExternalTestDBEnv)
	}

	//nolint:usetesting // 이 컨텍스트는 t.Cleanup의 데이터베이스 정리에서 재사용되므로 t.Context()를 쓸 수 없다.
	ctx := context.Background()
	name := fmt.Sprintf("pgstore_test_%d_%d", time.Now().UnixNano(), scopeCounter.Add(1))

	admin, err := pgxpool.New(ctx, baseDSN)
	if err != nil {
		t.Fatalf("connect %s: %v", testDatabaseURLEnv, err)
	}
	defer admin.Close()

	if _, createErr := admin.Exec(ctx, `CREATE DATABASE "`+name+`"`); createErr != nil {
		t.Fatalf("create database %s: %v", name, createErr)
	}

	config, err := pgxpool.ParseConfig(baseDSN)
	if err != nil {
		t.Fatalf("parse %s: %v", testDatabaseURLEnv, err)
	}

	config.ConnConfig.Database = name

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatalf("connect database %s: %v", name, err)
	}

	t.Cleanup(func() {
		pool.Close()

		cleanupAdmin, adminErr := pgxpool.New(ctx, baseDSN)
		if adminErr != nil {
			t.Errorf("connect for cleanup: %v", adminErr)

			return
		}
		defer cleanupAdmin.Close()

		if _, dropErr := cleanupAdmin.Exec(ctx, `DROP DATABASE IF EXISTS "`+name+`" WITH (FORCE)`); dropErr != nil {
			t.Errorf("drop database %s: %v", name, dropErr)
		}
	})

	return pool
}

func testDerive(base string, generation int) (string, error) {
	if suffixPattern.MatchString(base) {
		return "", errNestedBase
	}

	return base + ":r" + strconv.Itoa(generation), nil
}
