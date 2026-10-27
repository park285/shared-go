package pgstore_test

import (
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/park285/shared-go/v2/pkg/irisdurable/pgstore"
)

// 테스트가 명시하는 정책 값이다. Iris admission 보존·지평은 iris-client-go 공개 상수와 같은 값이며,
// 라이브러리 기본값이 아니라 소비자가 넘기는 값을 흉내 낸다.
const (
	testIrisAdmissionRetention = 168 * time.Hour
	testReplayHorizon          = 144 * time.Hour
	testInboxTerminalRetention = 8 * 24 * time.Hour
	testManualReviewRetention  = 30 * 24 * time.Hour
)

// testPolicy는 테스트가 따로 정하지 않은 정책 항목을 명시 테스트 값으로 채운다.
func testPolicy(opts pgstore.Options) pgstore.Options {
	fill := func(value *time.Duration, def time.Duration) {
		if *value == 0 {
			*value = def
		}
	}

	fill(&opts.IrisAdmissionRetention, testIrisAdmissionRetention)
	fill(&opts.Lease, 30*time.Second)
	fill(&opts.BackoffBase, time.Second)
	fill(&opts.BackoffMax, time.Minute)
	fill(&opts.AutomaticReplayHorizon, testReplayHorizon)
	fill(&opts.ReplyRetention, testIrisAdmissionRetention)
	fill(&opts.InboxTerminalRetention, testInboxTerminalRetention)

	if opts.MaxAttempts == 0 {
		opts.MaxAttempts = 8
	}

	return opts
}

// TestOptionsEnforceIrisAdmissionBoundaries는 보존·지평이 호출자가 넘긴 Iris admission 보존을 기준으로
// 기동 시점에 검증되는지 본다. 종단 inbox 보존은 admission 보존과 같아도 거부한다(XST-02).
func TestOptionsEnforceIrisAdmissionBoundaries(t *testing.T) {
	t.Parallel()

	pool := &pgxpool.Pool{}
	admission := testIrisAdmissionRetention

	rejected := []struct {
		name string
		opts pgstore.Options
	}{
		{"retention without admission", pgstore.Options{InboxTerminalRetention: testInboxTerminalRetention}},
		{"horizon without admission", pgstore.Options{AutomaticReplayHorizon: testReplayHorizon}},
		{"reply retention below admission", pgstore.Options{IrisAdmissionRetention: admission, ReplyRetention: time.Hour}},
		{"horizon equal to admission", pgstore.Options{IrisAdmissionRetention: admission, AutomaticReplayHorizon: admission}},
		{"inbox terminal at replay horizon", pgstore.Options{IrisAdmissionRetention: admission, InboxTerminalRetention: testReplayHorizon}},
		{"inbox terminal equal to admission", pgstore.Options{IrisAdmissionRetention: admission, InboxTerminalRetention: admission}},
		{"manual review below terminal", pgstore.Options{IrisAdmissionRetention: admission, InboxTerminalRetention: testInboxTerminalRetention, InboxManualReviewRetention: admission + time.Hour}},
		{"backoff max only", pgstore.Options{BackoffMax: time.Minute}},
		{"backoff max below base", pgstore.Options{BackoffBase: time.Minute, BackoffMax: time.Second}},
		{"negative lease", pgstore.Options{Lease: -time.Second}},
		{"negative attempts", pgstore.Options{MaxAttempts: -1}},
	}

	for _, tc := range rejected {
		if _, err := pgstore.New(pool, tc.opts); err == nil {
			t.Errorf("%s: New error = nil, want rejection", tc.name)
		}
	}

	accepted := pgstore.Options{IrisAdmissionRetention: admission, InboxTerminalRetention: admission + time.Second}
	if _, err := pgstore.New(pool, accepted); err != nil {
		t.Fatalf("New(inbox terminal just above admission) error = %v", err)
	}
}

// TestOperationsRejectMissingPolicy는 정책 기본값이 없어 0인 값을 쓰는 연산이 SQL 전에 오류를 내는지 본다.
// 이전에는 lease 30초·시도 8회·백오프 1초~1분이 조용히 채워져 소비자 정책과 달라질 수 있었다.
func TestOperationsRejectMissingPolicy(t *testing.T) {
	t.Parallel()

	// SQL에 닿으면 nil Querier 호출로 panic하므로 오류 반환은 SQL 전 판정을 뜻한다.
	store, err := pgstore.New(struct{ pgstore.Querier }{}, pgstore.Options{Scope: "nonce-only"})
	if err != nil {
		t.Fatalf("New(policy-free store) error = %v", err)
	}

	ctx := t.Context()

	_, _, claimErr := store.Claim(ctx)
	_, batchErr := store.ClaimBatch(ctx, 1)
	_, pruneErr := store.PruneInbox(ctx, 1)
	_, snapshotErr := store.ReplyReadySnapshot(ctx)
	_, backoffErr := store.BackoffFor(1)

	for name, err := range map[string]error{
		"Lease":                  claimErr,
		"Lease batch":            batchErr,
		"Lease renew":            store.RenewInbox(ctx, pgstore.InboxClaim{}),
		"InboxTerminalRetention": pruneErr,
		"MaxAttempts":            snapshotErr,
		"BackoffBase":            backoffErr,
	} {
		if err == nil || !strings.Contains(err.Error(), "requires Options.") {
			t.Errorf("%s: error = %v, want missing policy error", name, err)
		}
	}
}
