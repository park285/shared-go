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
		if err := step.Close(ctx); err != nil {
			errs = append(errs, fmt.Errorf("close %s: %w", name, err))

			if logger != nil {
				logger.Error("close step failed", slog.String("step", name), slog.Any("error", err))
			}
		}
	}

	return errors.Join(errs...)
}

func closeStepName(index int, name string) string {
	if name != "" {
		return name
	}

	return fmt.Sprintf("step[%d]", index)
}
