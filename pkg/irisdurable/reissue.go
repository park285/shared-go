package irisdurable

import (
	"context"
	"errors"
	"fmt"
)

var (
	ErrReissueGenerationOutOfRange = errors.New("irisdurable: reissue generation out of range")
	ErrReissueExhausted            = errors.New("irisdurable: reissue generations exhausted")
	ErrReissueLadderInvalid        = errors.New("irisdurable: reissue ladder is not configured")
)

type ReissueLadder struct {
	MaxGenerations int
	Derive         func(base string, generation int) (string, error)
}

type SendFunc func(ctx context.Context, clientRequestID string, generation int) error

type ReissueResult struct {
	ClientRequestID string
	Generation      int
}

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
