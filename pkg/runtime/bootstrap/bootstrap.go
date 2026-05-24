package bootstrap

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"

	sharedlogging "github.com/park285/shared-go/pkg/logging"
	"github.com/park285/shared-go/pkg/runtime/automaxprocs"
)

type runtime interface {
	Run()
	Close()
}

type Options[Config any, Runtime runtime] struct {
	Version                string
	Initialize             func(version string)
	LoadConfig             func() (Config, error)
	LoadConfigErrorMessage string
	NewLogger              func(config Config) (*slog.Logger, error)
	LoggerConfig           func(config Config) sharedlogging.Config
	LoggerFileName         string
	LoggerLevel            func(config Config) string
	StartupMessage         string
	StartupFields          func(config Config) []any
	BuildTimeout           time.Duration
	BuildRuntime           func(ctx context.Context, config Config, logger *slog.Logger) (Runtime, error)
	BuildErrorMessage      string
	Stderr                 io.Writer
}

func Run[Config any, Runtime runtime](opts Options[Config, Runtime]) int {
	initializeRuntime(opts.Initialize, opts.Version)
	stderr := runtimeStderr(opts.Stderr)

	config, err := opts.LoadConfig()
	if err != nil {
		return printBootstrapError(stderr, fallback(opts.LoadConfigErrorMessage, "Failed to load config"), err)
	}

	logger, err := newLogger(opts, config)
	if err != nil {
		return printBootstrapError(stderr, "Failed to initialize logger", err)
	}

	if message := opts.StartupMessage; message != "" {
		logger.Info(message, startupFields(opts, config)...)
	}

	buildCtx, buildCancel := context.WithTimeout(context.Background(), opts.BuildTimeout)
	defer buildCancel()

	rt, err := opts.BuildRuntime(buildCtx, config, logger)
	if err != nil {
		logger.Error(fallback(opts.BuildErrorMessage, "Failed to build runtime"), slog.Any("error", err))
		return 1
	}
	defer rt.Close()

	rt.Run()
	return 0
}

func initializeRuntime(initialize func(version string), version string) {
	if initialize == nil {
		initialize = func(version string) {
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
	if _, writeErr := fmt.Fprintf(stderr, "%s: %v\n", message, err); writeErr != nil {
		return 1
	}
	return 1
}

func newLogger[Config any, Runtime runtime](opts Options[Config, Runtime], config Config) (*slog.Logger, error) {
	if opts.NewLogger != nil {
		return opts.NewLogger(config)
	}

	logConfig := sharedlogging.Config{}
	if opts.LoggerConfig != nil {
		logConfig = opts.LoggerConfig(config)
	}

	level := ""
	if opts.LoggerLevel != nil {
		level = opts.LoggerLevel(config)
	}

	return sharedlogging.EnableFileLoggingWithLevel(logConfig, opts.LoggerFileName, level)
}

func startupFields[Config any, Runtime runtime](opts Options[Config, Runtime], config Config) []any {
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
