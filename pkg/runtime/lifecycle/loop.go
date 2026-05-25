package lifecycle

import (
	"context"
	"errors"
	"time"
)

func RunTickerLoop(ctx context.Context, interval time.Duration, onTick func(context.Context) error) error {
	if err := validateTickerLoop(interval, onTick); err != nil {
		return err
	}
	return runTickerLoop(ctx, interval, onTick)
}

func validateTickerLoop(interval time.Duration, onTick func(context.Context) error) error {
	if interval <= 0 {
		return errors.New("interval must be positive")
	}
	if onTick == nil {
		return errors.New("onTick must not be nil")
	}
	return nil
}

func runTickerLoop(ctx context.Context, interval time.Duration, onTick func(context.Context) error) error {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		if err := waitForTicker(ctx, ticker.C); err != nil {
			return err
		}
		if err := onTick(ctx); err != nil {
			return err
		}
	}
}

func waitForTicker(ctx context.Context, ticks <-chan time.Time) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-ticks:
		return nil
	}
}
