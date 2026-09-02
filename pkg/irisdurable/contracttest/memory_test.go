package contracttest_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/park285/shared-go/v2/pkg/irisdurable"
	"github.com/park285/shared-go/v2/pkg/workercontract"
)

var (
	errStaleClaim  = errors.New("memory outbox: stale claim token")
	errUnknownRow  = errors.New("memory outbox: unknown reply")
	errPreHandoff  = errors.New("memory transport: CLIENT_REQUEST_ID_FAILED")
	errTerminal409 = errors.New("memory transport: CLIENT_REQUEST_ID_OUTCOME_UNKNOWN")
	errNestedBase  = errors.New("memory ladder: base already reissued")
	suffixPattern  = regexp.MustCompile(`:r\d+$`)
	memoryCounter  atomic.Uint64
)

type memoryAdmitter struct {
	mu   sync.Mutex
	seen map[string]struct{}
}

func (m *memoryAdmitter) Admit(ctx context.Context, input irisdurable.AdmissionInput) (workercontract.AdmissionResult, error) {
	if err := ctx.Err(); err != nil {
		return workercontract.AdmissionFailed, fmt.Errorf("memory admit: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.seen[input.MessageID]; ok {
		return workercontract.AdmissionDuplicate, nil
	}

	m.seen[input.MessageID] = struct{}{}

	return workercontract.AdmissionAccepted, nil
}

type memoryNonceStore struct {
	mu       sync.Mutex
	expiries map[string]time.Time
}

func (s *memoryNonceStore) IsDuplicate(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, fmt.Errorf("memory nonce: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	if expiry, ok := s.expiries[key]; ok && now.Before(expiry) {
		return true, nil
	}

	s.expiries[key] = now.Add(ttl)

	return false, nil
}

func (s *memoryNonceStore) SetOnceNonce() {}

type memoryReplyRow struct {
	payload         []byte
	status          irisdurable.ReplyStatus
	attempts        int
	claim           string
	clientRequestID string
}

type memoryReplyOutbox struct {
	mu   sync.Mutex
	rows map[irisdurable.ReplyIdentity]*memoryReplyRow
}

func (o *memoryReplyOutbox) NewRecord(t *testing.T, payload []byte) irisdurable.ReplyRecord {
	t.Helper()

	id := fmt.Sprintf("memory-%d", memoryCounter.Add(1))

	return irisdurable.ReplyRecord{
		MessageID: id, Phase: "reply", Ordinal: 0,
		RoomID:          "room-" + id,
		ClientRequestID: "memory:v1:" + id + ":reply:0",
		Payload:         payload,
	}
}

func (o *memoryReplyOutbox) Stage(ctx context.Context, record irisdurable.ReplyRecord) (irisdurable.ReplyStageOutcome, error) {
	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("memory stage: %w", err)
	}

	o.mu.Lock()
	defer o.mu.Unlock()

	if row, ok := o.rows[record.ReplyIdentity]; ok {
		if bytes.Equal(row.payload, record.Payload) {
			return irisdurable.ReplyAlreadyStaged, nil
		}

		return irisdurable.ReplyPayloadDiverged, nil
	}

	o.rows[record.ReplyIdentity] = &memoryReplyRow{
		payload:         bytes.Clone(record.Payload),
		status:          irisdurable.ReplyStatusPending,
		clientRequestID: record.ClientRequestID,
	}

	return irisdurable.ReplyStaged, nil
}

func (o *memoryReplyOutbox) BeginAttempt(ctx context.Context, identity irisdurable.ReplyIdentity) (irisdurable.ReplyAttempt, error) {
	if err := ctx.Err(); err != nil {
		return irisdurable.ReplyAttempt{}, fmt.Errorf("memory begin attempt: %w", err)
	}

	o.mu.Lock()
	defer o.mu.Unlock()

	row, ok := o.rows[identity]
	if !ok || row.claim != "" || !row.status.Resendable() {
		return irisdurable.ReplyAttempt{}, irisdurable.ErrReplyNotClaimable
	}

	row.attempts++

	row.claim = fmt.Sprintf("claim-%d", memoryCounter.Add(1))
	row.status = irisdurable.ReplyStatusSubmitting

	return irisdurable.ReplyAttempt{
		ReplyIdentity:   identity,
		ClaimToken:      row.claim,
		Attempt:         row.attempts,
		ClientRequestID: row.clientRequestID,
	}, nil
}

func (o *memoryReplyOutbox) Settle(ctx context.Context, attempt irisdurable.ReplyAttempt, outcome irisdurable.ReplyOutcome) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("memory settle: %w", err)
	}

	o.mu.Lock()
	defer o.mu.Unlock()

	row, ok := o.rows[attempt.ReplyIdentity]
	if !ok {
		return errUnknownRow
	}

	if row.claim != attempt.ClaimToken {
		return errStaleClaim
	}

	row.status = outcome.Status
	row.clientRequestID = outcome.ClientRequestID
	row.claim = ""

	if outcome.Status.Terminal() {
		row.payload = nil
	}

	return nil
}

func (o *memoryReplyOutbox) Inspect(ctx context.Context, identity irisdurable.ReplyIdentity) (irisdurable.ReplyState, error) {
	if err := ctx.Err(); err != nil {
		return irisdurable.ReplyState{}, fmt.Errorf("memory inspect: %w", err)
	}

	o.mu.Lock()
	defer o.mu.Unlock()

	row, ok := o.rows[identity]
	if !ok {
		return irisdurable.ReplyState{}, errUnknownRow
	}

	return irisdurable.ReplyState{
		Status:          row.status,
		ClientRequestID: row.clientRequestID,
		Attempts:        row.attempts,
		PayloadPresent:  row.payload != nil,
	}, nil
}

func memoryDerive(base string, generation int) (string, error) {
	if suffixPattern.MatchString(base) {
		return "", errNestedBase
	}

	return base + ":r" + strconv.Itoa(generation), nil
}
