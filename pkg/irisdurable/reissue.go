package irisdurable

import (
	"context"
	"errors"
	"fmt"
)

var (
	// ErrReissueGenerationOutOfRange는 세대가 0..MaxGenerations 밖일 때 반환한다.
	ErrReissueGenerationOutOfRange = errors.New("irisdurable: reissue generation out of range")
	// ErrReissueExhausted는 모든 세대가 pre-handoff conflict로 끝났을 때 마지막 오류를 감싼다.
	ErrReissueExhausted = errors.New("irisdurable: reissue generations exhausted")
	// ErrReissueLadderInvalid는 ladder 구성이나 Run 인자가 비어 있을 때 반환한다.
	ErrReissueLadderInvalid = errors.New("irisdurable: reissue ladder is not configured")
)

// ReissueLadder는 clientRequestId의 bounded 재발급 사다리다.
//
// 세대 0은 base id 그대로이고 1..MaxGenerations는 Derive가 만든다. Derive와 MaxGenerations에는
// iris-client-go의 ReissuedClientRequestID와 ReplyReissueMaxGenerations를 그대로 넘겨 세대 규칙이
// 한 곳에서만 정의되게 한다.
type ReissueLadder struct {
	MaxGenerations int
	Derive         func(base string, generation int) (string, error)
}

// SendFunc는 한 세대의 id로 발송을 시도한다.
type SendFunc func(ctx context.Context, clientRequestID string, generation int) error

// ReissueResult는 Run이 마지막으로 시도한 id와 세대다.
type ReissueResult struct {
	ClientRequestID string
	Generation      int
}

// ClientRequestID는 base의 generation차 id를 만든다.
func (l ReissueLadder) ClientRequestID(base string, generation int) (string, error) {
	if l.Derive == nil || l.MaxGenerations <= 0 {
		return "", ErrReissueLadderInvalid
	}

	if generation < 0 || generation > l.MaxGenerations {
		return "", fmt.Errorf("%w: generation=%d max=%d", ErrReissueGenerationOutOfRange, generation, l.MaxGenerations)
	}

	if generation == 0 {
		return base, nil
	}

	id, err := l.Derive(base, generation)
	if err != nil {
		return "", fmt.Errorf("derive reissued clientRequestId: %w", err)
	}

	return id, nil
}

// GenerationOf는 저장된 id가 base의 몇 세대인지 복원한다. 어느 세대와도 맞지 않으면 false다.
func (l ReissueLadder) GenerationOf(base, stored string) (int, bool) {
	if stored == base {
		return 0, true
	}

	for generation := 1; generation <= l.MaxGenerations; generation++ {
		candidate, err := l.ClientRequestID(base, generation)
		if err != nil {
			return 0, false
		}

		if candidate == stored {
			return generation, true
		}
	}

	return 0, false
}

// Run은 start 세대부터 발송을 시도하고, isPreHandoffConflict가 참인 오류에서만 다음 세대로 올린다.
// 그 외 오류는 그대로 반환해 outcome_unknown·payload mismatch를 재발급으로 덮지 않는다. 세대를
// 다 쓰면 마지막 conflict를 ErrReissueExhausted로 감싸고, context가 끝나면 그 시점의 conflict를 반환한다.
func (l ReissueLadder) Run(
	ctx context.Context,
	base string,
	start int,
	send SendFunc,
	isPreHandoffConflict func(error) bool,
) (ReissueResult, error) {
	if send == nil || isPreHandoffConflict == nil {
		return ReissueResult{}, ErrReissueLadderInvalid
	}

	var result ReissueResult

	for generation := start; generation <= l.MaxGenerations; generation++ {
		id, err := l.ClientRequestID(base, generation)
		if err != nil {
			return result, err
		}

		result = ReissueResult{ClientRequestID: id, Generation: generation}

		sendErr := send(ctx, id, generation)
		if sendErr == nil {
			return result, nil
		}

		if !isPreHandoffConflict(sendErr) || ctx.Err() != nil {
			return result, sendErr
		}

		if generation == l.MaxGenerations {
			return result, fmt.Errorf("%w: %w", ErrReissueExhausted, sendErr)
		}
	}

	return result, fmt.Errorf("%w: start=%d max=%d", ErrReissueGenerationOutOfRange, start, l.MaxGenerations)
}
