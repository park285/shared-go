package automaxprocs

import (
	"fmt"
	"log/slog"
	"os"
	"strings"

	"go.uber.org/automaxprocs/maxprocs"
)

const (
	DisableEnv = "AUTOMAXPROCS_DISABLE"
	ForceEnv   = "AUTOMAXPROCS_FORCE"
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

func Init(logger *slog.Logger) {
	decision := decideAutomaxprocs()
	if !decision.run {
		logInfo(logger, decision.message, decision.fields...)
		return
	}
	if logger == nil {
		logger = slog.Default()
	}

	_, err := maxprocs.Set(maxprocs.Logger(func(format string, args ...any) {
		logger.Info(fmt.Sprintf(format, args...))
	}))
	if err != nil {
		logger.Warn("automaxprocs set failed", "err", err)
	}
}

func shouldRunAutomaxprocs() bool {
	return decideAutomaxprocs().run
}

func decideAutomaxprocs() automaxprocsDecision {
	if isTruthy(os.Getenv(DisableEnv)) {
		return automaxprocsDecision{
			run:     false,
			reason:  automaxprocsDecisionDisabled,
			message: "automaxprocs disabled by env",
			fields:  []any{"env", DisableEnv},
		}
	}
	if isTruthy(os.Getenv(ForceEnv)) {
		return automaxprocsDecision{
			run:    true,
			reason: automaxprocsDecisionForced,
		}
	}

	return automaxprocsDecision{
		run:     false,
		reason:  automaxprocsDecisionNativeRuntime,
		message: "automaxprocs skipped; Go 1.25+ runtime handles container-aware GOMAXPROCS",
		fields:  []any{"force_env", ForceEnv},
	}
}

func isTruthy(v string) bool {
	v = strings.TrimSpace(v)
	v = strings.ToLower(v)
	return v == "1" || v == "true" || v == "yes" || v == "y"
}

func logInfo(logger *slog.Logger, msg string, fields ...any) {
	if logger != nil {
		logger.Info(msg, fields...)
	}
}
