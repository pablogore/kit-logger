package logger

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/getsyntegrity/kit-logger/pkg/logger/handler"
)

var defaultLogger Logger

// Config holds the configuration for the logger.
type Config struct {
	BufferSize   int
	FilterRules  []handler.FilterRule
	Format       string
	GlobalFields map[string]string
	Handler      slog.Handler // optional; if set, used as the base handler (e.g. for tests)
	Hook         func(ctx context.Context, r slog.Record) (context.Context, bool)
	Keys         []string
	Level        string
	Sampling     SamplingConfig
}

// Option configures New (e.g. WithCounterHook).
type Option func(*loggerOptions)

type loggerOptions struct {
	counterHook CounterHook
}

// WithCounterHook injects a hook invoked when a log is emitted and WithCounter was used.
func WithCounterHook(hook CounterHook) Option {
	return func(o *loggerOptions) {
		o.counterHook = hook
	}
}

// SamplingConfig controls the frequency and probability of logs.
type SamplingConfig struct {
	Enabled     bool
	Interval    time.Duration
	MinLevel    string
	Probability float64
}

// SetGlobal sets the global logger.
func SetGlobal(log Logger) {
	defaultLogger = log
	slog.SetDefault(defaultLogger.Slog())
}

// L returns the global logger.
func L() Logger {
	if defaultLogger == nil {
		defaultLogger = New(Config{})
	}
	return defaultLogger
}

// New creates a new Logger instance with all handlers configured.
// Optional opts (e.g. WithCounterHook) apply rate-limit-related behavior when WithRateLimit/WithCounter are used in log calls.
func New(cfg Config, opts ...Option) Logger {
	level := parseLevel(cfg.Level)
	levelVar := new(slog.LevelVar)
	levelVar.Set(level)

	var h slog.Handler
	if cfg.Handler != nil {
		h = cfg.Handler
	} else {
		if cfg.Format == "json" {
			h = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: levelVar})
		} else {
			h = slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: levelVar})
		}

		if len(cfg.FilterRules) > 0 {
			h = handler.NewFilterHandler(h, cfg.FilterRules)
		}
		if len(cfg.GlobalFields) > 0 {
			h = handler.NewGlobalFieldsHandler(h, cfg.GlobalFields, true)
		}
		h = handler.NewComponentHandler(h)

		if cfg.Sampling.Enabled {
			h = handler.NewSamplingHandler(h, handler.SamplingConfig{
				Interval:    cfg.Sampling.Interval,
				Probability: cfg.Sampling.Probability,
				MinLevel:    parseLevel(cfg.Sampling.MinLevel),
			})
		}

		h = handler.NewPrometheusHandler(h)

		if cfg.BufferSize > 0 {
			h = handler.NewBufferedHandler(h, cfg.BufferSize)
		}

		if cfg.Hook != nil {
			h = handler.NewHookHandler(h, cfg.Hook)
		}
	}

	slogLogger := slog.New(h)
	if cfg.Handler == nil {
		slogLogger.Info("Logger initialized", "level", cfg.Level, "format", cfg.Format)
	}
	optVal := &loggerOptions{}
	for _, o := range opts {
		o(optVal)
	}
	return &SlogLogger{
		logger:      slogLogger,
		levelVar:    levelVar,
		rateState:   newRateState(),
		counterHook: optVal.counterHook,
	}
}

// parseLevel converts a string level to a slog.Level.
func parseLevel(lvl string) slog.Level {
	switch strings.ToLower(lvl) {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
