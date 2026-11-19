package panicguard

import (
	"fmt"
	"log/slog"
	"runtime/debug"
)

type LogContract struct {
	Message string
	NameKey string
}

func Run(logger *slog.Logger, contract LogContract, name string, fn func()) {
	contract.mustBeValid()

	defer func() {
		if recovered := recover(); recovered != nil {
			logPanic(logger, contract, name, recovered)
		}
	}()

	fn()
}

func RunE(logger *slog.Logger, contract LogContract, name string, fn func() error) (err error) {
	contract.mustBeValid()

	defer func() {
		if recovered := recover(); recovered != nil {
			logPanic(logger, contract, name, recovered)

			if recoveredErr, ok := recovered.(error); ok {
				err = fmt.Errorf("panicguard: %s: recovered panic: %w", name, recoveredErr)

				return
			}

			err = fmt.Errorf("panicguard: %s: recovered panic: %v", name, recovered)
		}
	}()

	return fn()
}

func (contract LogContract) mustBeValid() {
	if contract.Message == "" || contract.NameKey == "" {
		panic(fmt.Sprintf("panicguard: invalid log contract %+v", contract))
	}
}

func logPanic(logger *slog.Logger, contract LogContract, name string, recovered any) {
	if logger == nil {
		logger = slog.Default()
	}

	logger.Error(contract.Message,
		slog.String(contract.NameKey, name),
		slog.String("panic", fmt.Sprint(recovered)),
		slog.String("stack", string(debug.Stack())),
	)
}
