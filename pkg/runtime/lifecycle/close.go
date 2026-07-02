package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
)

type CloseStep struct {
	Name  string
	Close func(context.Context) error
}

func RunCloseSteps(ctx context.Context, logger *slog.Logger, steps []CloseStep) error {
	errs := make([]error, 0, len(steps))

	for i, step := range steps {
		if step.Close == nil {
			continue
		}

		name := closeStepName(i, step.Name)
		if err := runCloseStep(ctx, step.Close); err != nil {
			errs = append(errs, fmt.Errorf("close %s: %w", name, err))

			if logger != nil {
				logger.Error("close step failed", slog.String("step", name), slog.Any("error", err))
			}
		}
	}

	return errors.Join(errs...)
}

func runCloseStep(ctx context.Context, closeFn func(context.Context) error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			if perr, ok := r.(error); ok {
				err = fmt.Errorf("panic: %w", perr)
			} else {
				err = fmt.Errorf("panic: %v", r)
			}
		}
	}()

	return closeFn(ctx)
}

func closeStepName(index int, name string) string {
	if name != "" {
		return name
	}

	return fmt.Sprintf("step[%d]", index)
}
