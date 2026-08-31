// Package panicguard는 순수 panic-to-log 실행 경계를 제공한다.
package panicguard

import (
	"fmt"
	"log/slog"
	"runtime/debug"
)

// LogContract는 기존 소비자의 panic 로그 event와 task key 계약을 고정한다.
type LogContract uint8

const (
	// BackgroundTask는 Hololive background task 로그 계약이다.
	BackgroundTask LogContract = iota
	// Goroutine은 ChatBotGo goroutine 로그 계약이다.
	Goroutine
)

// Run은 fn의 panic을 복구해 기록한다. 정상 반환은 변경하지 않는다.
func Run(logger *slog.Logger, contract LogContract, name string, fn func()) {
	message, nameKey := contract.fields()

	defer func() {
		if recovered := recover(); recovered != nil {
			logPanic(logger, message, nameKey, name, recovered)
		}
	}()

	fn()
}

// RunE는 fn의 오류를 감싸 반환하고 panic을 오류로 변환해 기록한다.
func RunE(logger *slog.Logger, contract LogContract, name string, fn func() error) (err error) {
	message, nameKey := contract.fields()

	defer func() {
		if recovered := recover(); recovered != nil {
			logPanic(logger, message, nameKey, name, recovered)

			if recoveredErr, ok := recovered.(error); ok {
				err = fmt.Errorf("%s: recovered panic: %w", name, recoveredErr)

				return
			}

			err = fmt.Errorf("%s: recovered panic: %v", name, recovered)
		}
	}()

	if err := fn(); err != nil {
		return fmt.Errorf("fn: %w", err)
	}

	return nil
}

func (contract LogContract) fields() (message, nameKey string) {
	switch contract {
	case BackgroundTask:
		return "background goroutine panic recovered", "guard"
	case Goroutine:
		return "goroutine_panic_recovered", "goroutine"
	default:
		panic(fmt.Sprintf("panicguard: invalid log contract %d", contract))
	}
}

func logPanic(logger *slog.Logger, message, nameKey, name string, recovered any) {
	if logger == nil {
		logger = slog.Default()
	}

	logger.Error(message,
		slog.String(nameKey, name),
		slog.String("panic", fmt.Sprint(recovered)),
		slog.String("stack", string(debug.Stack())),
	)
}
