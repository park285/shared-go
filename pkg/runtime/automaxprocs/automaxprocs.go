package automaxprocs

import (
	"fmt"
	"log/slog"
	"os"
	"strings"

	"go.uber.org/automaxprocs/maxprocs"
)

// 운영자가 설정하는 환경 변수 이름이다. 이름 자체가 배포 계약이므로 상수만 비공개로 둔다.
const (
	disableEnv = "AUTOMAXPROCS_DISABLE"
	forceEnv   = "AUTOMAXPROCS_FORCE"
)

type automaxprocsDecisionReason string

const (
	automaxprocsDecisionDisabled      automaxprocsDecisionReason = "disabled"
	automaxprocsDecisionForced        automaxprocsDecisionReason = "forced"
	automaxprocsDecisionNativeRuntime automaxprocsDecisionReason = "native_runtime"
)

type automaxprocsDecision struct {
	run     bool
	reason  automaxprocsDecisionReason
	message string
	fields  []any
}

// Init은 AUTOMAXPROCS_DISABLE·AUTOMAXPROCS_FORCE에 따라 GOMAXPROCS 조정 여부를 정하고 결정을 logger에 기록한다.
// 인자 logger가 nil이면 slog.Default()를 쓴다. 서비스 logger를 만든 직후 runtime/bootstrap이 부른다.
func Init(logger *slog.Logger) {
	if logger == nil {
		logger = slog.Default()
	}

	decision := decideAutomaxprocs()
	if !decision.run {
		logger.Info(decision.message, decision.fields...)

		return
	}

	_, err := maxprocs.Set(maxprocs.Logger(func(format string, args ...any) {
		logger.Info(fmt.Sprintf(format, args...))
	}))
	if err != nil {
		logger.Warn("automaxprocs set failed", "err", err)
	}
}

func decideAutomaxprocs() automaxprocsDecision {
	if isTruthy(os.Getenv(disableEnv)) {
		return automaxprocsDecision{
			run:     false,
			reason:  automaxprocsDecisionDisabled,
			message: "automaxprocs disabled by env",
			fields:  []any{"env", disableEnv},
		}
	}

	if isTruthy(os.Getenv(forceEnv)) {
		return automaxprocsDecision{
			run:    true,
			reason: automaxprocsDecisionForced,
		}
	}

	return automaxprocsDecision{
		run:     false,
		reason:  automaxprocsDecisionNativeRuntime,
		message: "automaxprocs skipped; Go 1.25+ runtime handles container-aware GOMAXPROCS",
		fields:  []any{"force_env", forceEnv},
	}
}

func isTruthy(v string) bool {
	v = strings.TrimSpace(v)
	v = strings.ToLower(v)

	return v == "1" || v == "true" || v == "yes" || v == "y"
}
