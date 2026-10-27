// Package panicguard는 순수 panic-to-log 실행 경계를 제공한다.
package panicguard

import (
	"fmt"
	"log/slog"
	"runtime/debug"
)

// LogContract는 복구한 panic을 기록할 때 쓰는 로그 event 메시지와 작업 이름 attribute key다.
// 소비자는 자기 로그 어휘에 맞는 값을 직접 정의하며, 두 필드가 모두 비어 있지 않아야 한다.
type LogContract struct {
	Message string
	NameKey string
}

// Run은 fn의 panic을 복구해 기록한다. 정상 반환은 변경하지 않는다.
func Run(logger *slog.Logger, contract LogContract, name string, fn func()) {
	contract.mustBeValid()

	defer func() {
		if recovered := recover(); recovered != nil {
			logPanic(logger, contract, name, recovered)
		}
	}()

	fn()
}

// RunE는 fn의 오류를 그대로 반환하고 panic을 오류로 변환해 기록한다.
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

// 빈 계약은 로그에서 event나 작업 이름을 잃으므로 fn을 부르기 전에 거부한다.
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
