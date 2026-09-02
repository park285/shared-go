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

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/park285/shared-go/v2/pkg/irisdurable"
	"github.com/park285/shared-go/v2/pkg/irisdurable/contracttest"
	"github.com/park285/shared-go/v2/pkg/irisdurable/pgstore"
)

const (
	// TEST_DATABASE_URL이 가리키는 서버에 일회용 데이터베이스를 만들어 쓴다. 두 변수가 모두
	// 설정된 경우에만 돌아가므로 개발자 머신의 실제 데이터베이스를 건드리지 않는다.
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

// TestReferenceSchemaIsRepeatable은 소비자가 자기 migration으로 옮겨 적는 참조 스키마가
// 유효한 SQL이고 다시 적용해도 안전한지 확인한다.
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

// TestOptionsRejectRetentionShorterThanIrisAdmission은 보존 계약 위반이 기동 시점에 막히는지 본다.
func TestOptionsRejectRetentionShorterThanIrisAdmission(t *testing.T) {
	t.Parallel()

	pool := &pgxpool.Pool{}

	if _, err := pgstore.New(pool, pgstore.Options{ReplyRetention: time.Hour}); err == nil {
		t.Fatal("New with a reply retention shorter than the Iris admission retention must fail")
	}

	if _, err := pgstore.New(pool, pgstore.Options{AutomaticReplayHorizon: irisdurable.AutomaticReplayHorizon + time.Hour}); err == nil {
		t.Fatal("New with a replay horizon beyond the stack horizon must fail")
	}
}

// TestInboxOrderingKeyServesOneMessageAtATime은 같은 ordering key의 FIFO head 규칙을 확인한다.
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

	if completeErr := store.Complete(ctx, first); completeErr != nil {
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

// TestInboxRenewAndRelease는 lease 연장과 호출자가 정한 재시도 지연을 확인한다.
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

// TestReplyRedriveAndRetire는 유지보수 연산이 재발송 후보와 소진 행을 갈라내는지 확인한다.
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

	retired, err := store.Retire(ctx, 10)
	if err != nil {
		t.Fatalf("retire: %v", err)
	}

	if retired != 0 {
		t.Fatalf("retired %d rows; want none while attempts remain", retired)
	}

	exhausted := newScopedStoreWithOptions(t, pool, pgstore.Options{MaxAttempts: 1, Scope: store.Options().Scope})

	retired, err = exhausted.Retire(ctx, 10)
	if err != nil {
		t.Fatalf("retire with exhausted attempts: %v", err)
	}

	if retired != 1 {
		t.Fatalf("retired %d rows; want the attempt-exhausted row", retired)
	}

	state, err := store.Inspect(ctx, record.ReplyIdentity)
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}

	if state.Status != irisdurable.ReplyStatusDead || state.PayloadPresent {
		t.Fatalf("state after retire = %+v; want dead with the payload scrubbed", state)
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

	store, err := pgstore.New(&pgxpool.Pool{}, pgstore.Options{})
	if err != nil {
		t.Fatalf("resolve default options: %v", err)
	}

	opts := store.Options()

	return &contracttest.RetentionFixture{
		ReplyOutboxRetention:   opts.ReplyRetention,
		AutomaticReplayHorizon: opts.AutomaticReplayHorizon,
		InboxTerminalRetention: opts.InboxTerminalRetention,
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

	store, err := pgstore.New(pool, opts)
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

// applyReferenceSchema는 testdata/schema.sql을 적용한다. 이 DDL은 스택 SQL 소유권 계약상
// 소비자 migration이 소유하므로 shared-go에서는 계약 스위트를 돌리기 위한 fixture로만 쓴다.
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

// newBlankPool은 TEST_DATABASE_URL 서버에 일회용 데이터베이스를 만들어 그 pool을 돌려주고,
// 테스트가 끝나면 지운다. 두 환경변수가 없으면 테스트를 건너뛴다.
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
