package bootstrap

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"

	sharedlogging "github.com/park285/shared-go/v2/pkg/logging"
	"github.com/park285/shared-go/v2/pkg/runtime/automaxprocs"
)

// Run이 error를 반환하면 Options.Run은 비-0 exit code로 종료해 supervisor(systemd/Docker)의
// 재시작 정책이 동작하도록 합니다.
type runtime interface {
	Run() error
	Close()
}

type Options[Config any, Runtime runtime] struct {
	Version                string
	Initialize             func(version string)
	LoadConfig             func() (Config, error)
	LoadConfigErrorMessage string
	NewLogger              func(config Config) (*slog.Logger, io.Closer, error)
	LoggerConfig           func(config Config) sharedlogging.Config
	LoggerFileName         string
	LoggerLevel            func(config Config) string
	StartupMessage         string
	StartupFields          func(config Config) []any
	BuildTimeout           time.Duration
	BuildRuntime           func(ctx context.Context, config Config, logger *slog.Logger) (Runtime, error)
	BuildErrorMessage      string
	RunErrorMessage        string
	Stderr                 io.Writer
}

func (opts Options[Config, Runtime]) Run() int {
	initializeRuntime(opts.Initialize, opts.Version)

	stderr := runtimeStderr(opts.Stderr)

	config, err := opts.LoadConfig()
	if err != nil {
		return printBootstrapError(stderr, fallback(opts.LoadConfigErrorMessage, "Failed to load config"), err)
	}

	logger, loggerCloser, err := opts.newLogger(config)
	if err != nil {
		return printBootstrapError(stderr, "Failed to initialize logger", err)
	}
	if loggerCloser != nil {
		defer func() { _ = loggerCloser.Close() }()
	}

	if message := opts.StartupMessage; message != "" {
		logger.Info(message, opts.startupFields(config)...)
	}

	buildCtx, buildCancel := buildContext(opts.BuildTimeout)
	defer buildCancel()

	rt, err := opts.BuildRuntime(buildCtx, config, logger)
	if err != nil {
		logger.Error(
			sharedlogging.RedactDiagnostic(fallback(opts.BuildErrorMessage, "Failed to build runtime")),
			slog.String("error", sharedlogging.RedactDiagnostic(err.Error())),
		)

		return 1
	}
	defer rt.Close()

	if runErr := rt.Run(); runErr != nil {
		logger.Error(
			sharedlogging.RedactDiagnostic(fallback(opts.RunErrorMessage, "Runtime stopped with error")),
			slog.String("error", sharedlogging.RedactDiagnostic(runErr.Error())),
		)

		return 1
	}

	return 0
}

func buildContext(timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout > 0 {
		return context.WithTimeout(context.Background(), timeout)
	}

	return context.Background(), func() {}
}

func initializeRuntime(initialize func(version string), version string) {
	if initialize == nil {
		initialize = func(_ string) {
			automaxprocs.Init(nil)
		}
	}

	initialize(version)
}

func runtimeStderr(stderr io.Writer) io.Writer {
	if stderr == nil {
		return os.Stderr
	}

	return stderr
}

func printBootstrapError(stderr io.Writer, message string, err error) int {
	safeMessage := sharedlogging.RedactDiagnostic(message)
	safeError := "unknown error"

	if err != nil {
		safeError = sharedlogging.RedactDiagnostic(err.Error())
	}

	if _, writeErr := fmt.Fprintf(stderr, "%s: %s\n", safeMessage, safeError); writeErr != nil {
		return 1
	}

	return 1
}

func (opts Options[Config, Runtime]) newLogger(config Config) (*slog.Logger, io.Closer, error) {
	if opts.NewLogger != nil {
		out, closer, err := opts.NewLogger(config)
		if err != nil {
			return nil, nil, fmt.Errorf("logger hook: %w", err)
		}

		return out, closer, nil
	}

	logConfig := sharedlogging.Config{}

	if opts.LoggerConfig != nil {
		logConfig = opts.LoggerConfig(config)
	}

	level := ""

	if opts.LoggerLevel != nil {
		level = opts.LoggerLevel(config)
	}
	logConfig.Level = level

	out, closer, err := sharedlogging.EnableFileLoggingWithOptions(logConfig, opts.LoggerFileName, sharedlogging.Options{})
	if err != nil {
		return nil, nil, fmt.Errorf("enable file logging: %w", err)
	}

	return out, closer, nil
}

func (opts Options[Config, Runtime]) startupFields(config Config) []any {
	fields := []any{
		slog.String("version", opts.Version),
	}

	if opts.LoggerLevel != nil {
		fields = append(fields, slog.String("log_level", opts.LoggerLevel(config)))
	}

	if opts.StartupFields == nil {
		return fields
	}

	return append(fields, opts.StartupFields(config)...)
}

func fallback(value, def string) string {
	if value == "" {
		return def
	}

	return value
}
