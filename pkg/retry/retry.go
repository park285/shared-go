package retry

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/park285/shared-go/v2/pkg/backoff"
)

type RetryOptions struct {
	MaxAttempts   int
	BaseDelay     time.Duration
	Jitter        time.Duration
	ShouldRetry   func(err error) bool
	OnRetry       func(attempt int, err error, delay time.Duration)
	MaxDelay      time.Duration
	DelayOverride func(err error, computed time.Duration) (delay time.Duration, ok bool)
	Sleep         func(ctx context.Context, d time.Duration) bool
}

func Sleep(ctx context.Context, duration time.Duration) bool {
	return sleepWithContext(ctx, duration)
}

func WithRetry(ctx context.Context, opts RetryOptions, fn func(ctx context.Context) error) error {
	opts = normalizeRetryOptions(opts)

	var lastErr error

	for attempt := range opts.MaxAttempts {
		outcome := runRetryAttempt(ctx, opts, fn, attempt, lastErr)
		if outcome.done {
			return outcome.err
		}
		lastErr = outcome.lastErr
	}

	return lastErr
}

type retryAttemptOutcome struct {
	lastErr error
	done    bool
	err     error
}

func runRetryAttempt(
	ctx context.Context,
	opts RetryOptions,
	fn func(ctx context.Context) error,
	attempt int,
	lastErr error,
) retryAttemptOutcome {
	if err := retryContextError(ctx, lastErr); err != nil {
		return retryAttemptOutcome{done: true, err: err}
	}

	err := fn(ctx)
	if err == nil {
		return retryAttemptOutcome{done: true}
	}

	return handleRetryFailure(ctx, opts, attempt, err)
}

func handleRetryFailure(ctx context.Context, opts RetryOptions, attempt int, err error) retryAttemptOutcome {
	if !shouldContinueRetry(opts, err) {
		return retryAttemptOutcome{done: true, err: fmt.Errorf("retry aborted by ShouldRetry predicate: %w", err)}
	}
	if attempt >= opts.MaxAttempts-1 {
		return retryAttemptOutcome{done: true, err: err}
	}
	slept, delayErr := sleepBeforeRetry(ctx, opts, attempt, err)
	if delayErr != nil {
		return retryAttemptOutcome{done: true, err: errors.Join(err, delayErr)}
	}
	if !slept {
		return retryAttemptOutcome{done: true, err: err}
	}
	return retryAttemptOutcome{lastErr: err}
}

func normalizeRetryOptions(opts RetryOptions) RetryOptions {
	if opts.MaxAttempts < 1 {
		opts.MaxAttempts = 1
	}
	if opts.Sleep == nil {
		opts.Sleep = sleepWithContext
	}
	return opts
}

func retryContextError(ctx context.Context, lastErr error) error {
	if ctx.Err() == nil {
		return nil
	}
	if lastErr != nil {
		return lastErr
	}
	return fmt.Errorf("context error: %w", ctx.Err())
}

func shouldContinueRetry(opts RetryOptions, err error) bool {
	return opts.ShouldRetry == nil || opts.ShouldRetry(err)
}

func sleepBeforeRetry(ctx context.Context, opts RetryOptions, attempt int, err error) (bool, error) {
	delay, delayErr := retryDelay(opts, attempt, err)
	if delayErr != nil {
		return false, delayErr
	}
	if opts.OnRetry != nil {
		opts.OnRetry(attempt+1, err, delay)
	}
	return opts.Sleep(ctx, delay), nil
}

func retryDelay(opts RetryOptions, attempt int, err error) (time.Duration, error) {
	delay := backoff.ComputeExponentialBackoff(attempt, opts.BaseDelay, 0, opts.Jitter)
	if opts.DelayOverride != nil {
		if override, ok := opts.DelayOverride(err, delay); ok {
			if override < 0 {
				return 0, fmt.Errorf("retry delay override returned negative duration %s", override)
			}
			delay = override
		}
	}
	if opts.MaxDelay > 0 && delay > opts.MaxDelay {
		return opts.MaxDelay, nil
	}
	return delay, nil
}

func sleepWithContext(ctx context.Context, d time.Duration) bool {
	// d가 0 이하면 timer가 즉시 발화해 select가 ctx.Done()과 무작위로 경합한다. 반환값은 호출자의
	// 루프 계속 여부라, 취소를 약 절반의 확률로 놓치면 종료 중에도 다음 시도가 나간다.
	if ctx.Err() != nil {
		return false
	}
	if d <= 0 {
		return true
	}

	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}
