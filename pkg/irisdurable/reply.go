package irisdurable

import (
	"context"
	"errors"
	"time"
)

// ReplyStatus는 reply outbox 행의 스택 공통 상태 어휘다. 봇별 추가 상태는 이 집합 밖에 둔다.
type ReplyStatus string

const (
	ReplyStatusPending              ReplyStatus = "pending"
	ReplyStatusSubmitting           ReplyStatus = "submitting"
	ReplyStatusAccepted             ReplyStatus = "accepted"
	ReplyStatusRetryablePreDispatch ReplyStatus = "retryable_pre_dispatch"
	ReplyStatusOutcomeUnknown       ReplyStatus = "outcome_unknown"
	ReplyStatusDead                 ReplyStatus = "dead"
	ReplyStatusPermanentConflict    ReplyStatus = "permanent_conflict"
)

// Terminal은 payload를 지우고 다시는 발송하지 않는 상태인지 알려준다.
func (s ReplyStatus) Terminal() bool {
	return s == ReplyStatusDead || s == ReplyStatusPermanentConflict
}

// Resendable은 저장된 clientRequestId로 발송을 다시 시도할 수 있는 상태인지 알려준다.
// Outcome_unknown이 포함되는 이유: 결과 미상은 실패가 아니며, 같은 id 재전송은 Iris admission이
// 멱등하게 흡수하므로 새 id 없이 다시 보내는 것이 유일하게 안전한 경로다.
func (s ReplyStatus) Resendable() bool {
	return s == ReplyStatusPending || s == ReplyStatusRetryablePreDispatch || s == ReplyStatusOutcomeUnknown
}

// ReplyIdentity는 (message, phase, ordinal) 튜플로 outbox 행을 유일하게 가리킨다.
type ReplyIdentity struct {
	MessageID string
	Phase     string
	Ordinal   int
}

// ReplyRecord는 outbox에 stage할 응답이다. ClientRequestID는 세대 0의 base id다.
type ReplyRecord struct {
	ReplyIdentity

	RoomID          string
	ClientRequestID string
	Payload         []byte
}

// ReplyStageOutcome은 Stage의 결과다.
type ReplyStageOutcome string

const (
	ReplyStaged          ReplyStageOutcome = "staged"
	ReplyAlreadyStaged   ReplyStageOutcome = "already_staged"
	ReplyPayloadDiverged ReplyStageOutcome = "payload_diverged"
	// ReplyOrdinalSuperseded는 같은 (message, phase)에 더 높은 ordinal이 이미 있어 받지 않았다는
	// 뜻이다. 순번은 단조 증가하므로 뒤늦게 도착한 낮은 순번은 재전달된 중복이고, 받으면 이미
	// 보낸 응답 뒤에 앞 순번이 다시 나간다.
	ReplyOrdinalSuperseded ReplyStageOutcome = "ordinal_superseded"
)

// ReplyAttempt는 BeginAttempt가 발급한 발송 시도의 소유권이다. ClaimToken은 Settle의 fence다.
type ReplyAttempt struct {
	ReplyIdentity

	ClaimToken      string
	Attempt         int
	ClientRequestID string
}

// ReplyOutcome은 한 시도의 결과다. ClientRequestID는 실제로 보낸 id(재발급 세대 포함)다.
type ReplyOutcome struct {
	Status          ReplyStatus
	ClientRequestID string
	IrisRequestID   string
	RetryAfter      time.Duration
	// Progressed는 이번 시도가 전달을 전진시켰는지다. 재시도 상한은 "전진하지 못한 연속 시도"에만
	// 걸려야 한다. 한 응답을 여러 조각으로 나눠 보내는 호출자에서 전진한 시도까지 세면, 조각 수가
	// (상한 / 조각당 왕복) 을 넘는 순간 상한이 먼저 소진돼 응답의 꼬리가 영구 유실된다.
	// 전진 커서 자체는 호출자가 소유하며, 저장소는 이 판정만 받는다.
	Progressed bool
}

// ReplyState는 Inspect가 돌려주는 행의 관측 상태다.
type ReplyState struct {
	Status          ReplyStatus
	ClientRequestID string
	Attempts        int
	PayloadPresent  bool
}

// ErrReplyNotClaimable은 종단 상태이거나 다른 시도가 lease를 쥐고 있어 BeginAttempt할 수 없을 때 반환한다.
var ErrReplyNotClaimable = errors.New("irisdurable: reply is not claimable")

// ReplyOutbox는 응답 발송 원장의 저장소 계약이다.
//
//   - Stage는 같은 ReplyIdentity에 멱등하고, payload가 다르면 ReplyPayloadDiverged를 반환하며 저장본을 유지한다.
//     같은 (message, phase)에 더 높은 ordinal이 이미 있으면 ReplyOrdinalSuperseded로 거절한다.
//   - BeginAttempt는 재발송 가능한 행의 소유권을 잡는다. 종단이거나 lease가 살아 있으면 ErrReplyNotClaimable이다.
//   - Settle은 같은 ClaimToken으로만 상태를 바꾼다. 종단 상태는 payload를 지우고, outcome_unknown은
//     payload와 clientRequestId를 보존한다.
//   - Inspect는 상태를 읽기만 한다.
type ReplyOutbox interface {
	Stage(ctx context.Context, record ReplyRecord) (ReplyStageOutcome, error)
	BeginAttempt(ctx context.Context, identity ReplyIdentity) (ReplyAttempt, error)
	Settle(ctx context.Context, attempt ReplyAttempt, outcome ReplyOutcome) error
	Inspect(ctx context.Context, identity ReplyIdentity) (ReplyState, error)
}
