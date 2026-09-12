package logger

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pablogore/kit-logger/pkg/logger/handler"
)

var (
	// globalLogger holds the process-wide logger. It stores a *Logger rather
	// than a Logger because an interface value is two words -- a type and a
	// data pointer -- and publishing two words is not atomic: a reader can
	// observe one word of a new value next to one word of the old. The
	// pointer is the single word that gets published, so a reader either sees
	// the previous logger or the new one, never a mixture.
	//
	// atomic.Pointer rather than an RWMutex because L() is on the per-log path
	// of every consumer: even an uncontended read lock is a shared cache line
	// that every core has to fight over.
	globalLogger atomic.Pointer[Logger]

	// globalOnce guards the lazy default, so a burst of first callers
	// constructs exactly one logger -- and emits exactly one startup line.
	globalOnce sync.Once
)

// Config holds the configuration for the logger.
type Config struct {
	BufferSize  int
	FilterRules []handler.FilterRule
	// FilterMode selects what a FilterRules match does. Defaults to
	// handler.ModeDrop, matching this field's pre-existing behavior.
	FilterMode   handler.Mode
	Format       string
	GlobalFields map[string]string

	// Sink is the terminal handler that receives fully decorated records:
	// FilterRules, GlobalFields, component attribution, Sampling, Prometheus,
	// BufferSize and Hook are all applied on top of it, in the same order as
	// the default Text/JSON handler. When nil, that default handler (over
	// Writer, chosen by Format) is used instead.
	//
	// SetLevel works uniformly here too: Sink is wrapped in an internal
	// handler gated on the same LevelVar the default path uses, so a custom
	// sink gets dynamic level control for free.
	Sink slog.Handler

	// Handler is a deprecated alias for Sink; Sink wins if both are set.
	//
	// Deprecated: use Sink. Handler used to bypass the entire pipeline --
	// FilterRules, GlobalFields, component attribution, Sampling, Prometheus,
	// BufferSize, Hook and SetLevel were all silently ignored when it was set.
	// It now behaves exactly like Sink. Use PipelineOverride for the old
	// bypass behavior.
	Handler slog.Handler

	// PipelineOverride replaces the entire pipeline verbatim, exactly as
	// Handler used to: every other field in this Config is ignored, and
	// Validate reports that as an error naming the ignored fields. This is the
	// escape hatch for a caller that must not have its chain decorated, e.g.
	// one that already assembled its own Filter/Sampling/Buffer stack.
	PipelineOverride slog.Handler

	Hook      func(ctx context.Context, r slog.Record) (context.Context, bool)
	Keys      []string
	Level     string
	RateLimit RateLimitConfig
	Sampling  SamplingConfig

	// ContextFields extracts fields from a context. A logger configured with
	// one reads its own immutable field and never consults the process-wide
	// extractor, so it needs no synchronization and is unaffected by another
	// part of the process calling SetContextFieldExtractor.
	//
	// It is applied by WithContext and by every *Context log method.
	ContextFields ContextFieldExtractorFunc

	// Writer is the destination for the default text/JSON handler. Defaults to
	// stdout. Ignored when Sink, Handler or PipelineOverride is set.
	Writer io.Writer

	// ContextHandler wraps the assembled pipeline as its outermost decorator.
	//
	// It is the seam for handlers that must read the caller's context, which is
	// exactly what a handler cannot do from anywhere below the buffer: there it
	// would run on the worker goroutine. Trace correlation is the intended use:
	//
	//	cfg.ContextHandler = kitotel.Decorator(kitotel.Options{})
	//
	// A function seam rather than a typed field is what keeps pkg/logger free
	// of the OpenTelemetry dependency — the consumer imports
	// pkg/logger/otel, this package never does.
	ContextHandler HandlerDecorator

	// LogInitialization makes New emit one "Logger initialized" record through
	// the constructed pipeline. Defaults to false: a library constructor
	// should not write to stdout as a side effect of being called.
	LogInitialization bool
}

// Validate reports every problem with cfg that New cannot silently work
// around. It does not report an unparsable Level or Format string: those
// already fail toward a safe default (parseLevel) and retyping them is
// out of scope here.
func (cfg Config) Validate() error {
	var errs []error

	if cfg.Handler != nil && cfg.Sink != nil {
		errs = append(errs, fmt.Errorf("Config: both Handler and Sink are set; Sink takes precedence (Handler is deprecated, use Sink)"))
	}

	if cfg.PipelineOverride != nil {
		var ignored []string
		if cfg.Handler != nil {
			ignored = append(ignored, "Handler")
		}
		if cfg.Sink != nil {
			ignored = append(ignored, "Sink")
		}
		if len(cfg.FilterRules) > 0 {
			ignored = append(ignored, "FilterRules")
		}
		if len(cfg.GlobalFields) > 0 {
			ignored = append(ignored, "GlobalFields")
		}
		if cfg.Sampling.Enabled {
			ignored = append(ignored, "Sampling")
		}
		if cfg.BufferSize > 0 {
			ignored = append(ignored, "BufferSize")
		}
		if cfg.Hook != nil {
			ignored = append(ignored, "Hook")
		}
		if len(ignored) > 0 {
			errs = append(errs, fmt.Errorf("Config: PipelineOverride is set, which ignores: %s", strings.Join(ignored, ", ")))
		}
	}

	if cfg.Sampling.Enabled {
		if err := (handler.SamplingConfig{
			Interval:    cfg.Sampling.Interval,
			Probability: cfg.Sampling.Probability,
			KeyFunc:     cfg.Sampling.KeyFunc,
			MaxKeys:     cfg.Sampling.MaxKeys,
		}).Validate(); err != nil {
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}

// HandlerDecorator wraps a handler in another one. It is the shape of the
// Config.ContextHandler seam.
type HandlerDecorator func(slog.Handler) slog.Handler

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

// RateLimitConfig tunes the per-key rate limiting driven by WithRateLimit.
//
// It is additive and its zero value is the recommended configuration, so a
// Config written before this field existed keeps behaving the same way -- only
// bounded.
//
// There is deliberately no idle-TTL knob to go with MaxKeys. Reclamation is
// semantic: a key whose interval has elapsed with no unreported suppressions
// holds no information an operator could observe, so it is the first thing
// dropped when the map is full, and the oldest keys by last use go after that.
// A TTL would only ask an operator to guess a second duration that the interval
// already implies.
type RateLimitConfig struct {
	// MaxKeys bounds how many distinct rate-limit keys are tracked. Defaults
	// to DefaultRateLimitMaxKeys.
	//
	// Rate-limit keys are caller data, so their cardinality is not something
	// this library can predict; MaxKeys is the ceiling that keeps a
	// high-cardinality key from turning the logger into a memory leak. Like
	// SamplingConfig.MaxKeys it governs memory rather than emission, so any
	// non-positive value -- zero meaning unset, negative meaning invalid --
	// falls back to the same bounded default instead of being rejected. Raising
	// it trades memory for a longer memory of which keys were recently limited.
	MaxKeys int
}

// SamplingConfig controls the frequency and probability of logs.
type SamplingConfig struct {
	Enabled  bool
	Interval time.Duration
	MinLevel string

	// Probability is the chance in [0,1] that a record passes the probability
	// gate. Zero means "unset" and is treated as 1 (emit everything): a
	// sampler that discards every record is indistinguishable from a
	// misconfiguration, so this fails toward emitting. Use Enabled=false to
	// turn sampling off.
	Probability float64

	// KeyFunc derives the sampling key. Defaults to level plus message, so
	// distinct events never suppress each other.
	KeyFunc handler.SamplingKeyFunc

	// MaxKeys bounds how many distinct sampling keys are tracked. Defaults to
	// handler.DefaultSamplingMaxKeys.
	MaxKeys int
}

// SetGlobal sets the global logger. It is safe to call concurrently with L,
// including while requests are in flight.
//
// It no longer reconfigures slog.Default. Rewiring the standard library for the
// whole process is not something a library should do as a side effect of
// setting its own global -- and it is how a logger whose Slog() is unusable
// becomes a landmine for unrelated code. Call SetGlobalAndSlogDefault when
// installing the slog default is what you actually want.
//
// A nil logger panics: storing one would turn every later L() into a nil
// dereference somewhere far away from the mistake.
func SetGlobal(log Logger) {
	if log == nil {
		panic("kit-logger: SetGlobal called with a nil Logger")
	}
	globalLogger.Store(&log)
}

// SetGlobalAndSlogDefault sets the global logger and also installs it as the
// standard library's default through slog.SetDefault.
//
// The process-wide effect is in the name, which is the whole point: it is the
// opt-in version of what SetGlobal used to do implicitly.
func SetGlobalAndSlogDefault(log Logger) {
	SetGlobal(log)
	slog.SetDefault(log.Slog())
}

// L returns the global logger, constructing a default one on first use.
//
// Prefer owning a logger explicitly and passing it where it is needed; L exists
// so that consumers which cannot yet do that are not forced into a racy global.
func L() Logger {
	if p := globalLogger.Load(); p != nil {
		return *p
	}
	globalOnce.Do(func() {
		l := New(Config{LogInitialization: true})
		// CompareAndSwap rather than Store: a SetGlobal that landed while New
		// was running expresses more recent intent and must not be clobbered
		// by the lazy default.
		globalLogger.CompareAndSwap(nil, &l)
	})
	return *globalLogger.Load()
}

// New creates a new Logger instance with all handlers configured.
// Optional opts (e.g. WithCounterHook) apply rate-limit-related behavior when WithRateLimit/WithCounter are used in log calls.
//
// A Config that fails Validate still produces a usable logger -- New reports
// the problem to stderr rather than failing outright. Use NewWithError to
// receive the error instead.
func New(cfg Config, opts ...Option) Logger {
	l, err := NewWithError(cfg, opts...)
	if err != nil {
		fmt.Fprintf(os.Stderr, "kit-logger: invalid configuration: %v\n", err)
	}
	return l
}

// NewWithError is New, but returns cfg.Validate's error instead of only
// printing it to stderr. The returned Logger is always usable: an invalid
// combination is resolved the same way New resolves it (see Validate).
func NewWithError(cfg Config, opts ...Option) (Logger, error) {
	level := parseLevel(cfg.Level)
	levelVar := new(slog.LevelVar)
	levelVar.Set(level)

	// lifecycleHandlers is captured while the pipeline is being assembled
	// rather than rediscovered at Flush/Shutdown time. Walking the chain at
	// call time is wrong twice over: any handler that does not implement
	// Unwrap severs the walk, and after With the outermost handler is a
	// derived one, not the root that owns the buffer.
	var lifecycleHandlers []Flusher
	var h slog.Handler

	switch {
	case cfg.PipelineOverride != nil:
		// The chain came from the caller and bypasses decoration entirely, so
		// this is the one case where the lifecycle-bearing handlers have to
		// be discovered by walking it.
		h = cfg.PipelineOverride
		lifecycleHandlers = collectFlushers(h)
	default:
		sink := cfg.Sink
		if sink == nil {
			sink = cfg.Handler // deprecated alias
		}

		var base slog.Handler
		if sink != nil {
			// Collected before wrapping: these are the caller's own
			// Flushers, found the same way PipelineOverride's are.
			lifecycleHandlers = collectFlushers(sink)
			base = &levelHandler{next: sink, level: levelVar}
		} else {
			w := cfg.Writer
			if w == nil {
				w = os.Stdout
			}
			if cfg.Format == "json" {
				base = slog.NewJSONHandler(w, &slog.HandlerOptions{Level: levelVar})
			} else {
				base = slog.NewTextHandler(w, &slog.HandlerOptions{Level: levelVar})
			}
		}

		var extra []Flusher
		h, extra = decorate(base, cfg)
		lifecycleHandlers = append(lifecycleHandlers, extra...)
	}

	// Outermost, and after the flushers have been collected: a handler that
	// reads the caller's context has to run on the caller's goroutine, which
	// means above the buffer. Applied to a caller-supplied chain too, so a test
	// can exercise the seam against its own sink.
	if cfg.ContextHandler != nil {
		if wrapped := cfg.ContextHandler(h); wrapped != nil {
			h = wrapped
		}
	}

	slogLogger := slog.New(h)
	if cfg.LogInitialization {
		slogLogger.Info("Logger initialized", "level", cfg.Level, "format", cfg.Format)
	}
	optVal := &loggerOptions{}
	for _, o := range opts {
		o(optVal)
	}
	logger := &SlogLogger{
		logger:        slogLogger,
		levelVar:      levelVar,
		rateState:     newRateState(cfg.RateLimit.MaxKeys),
		counterHook:   optVal.counterHook,
		lifecycle:     newLifecycleGroup(lifecycleHandlers...),
		contextFields: cfg.ContextFields,
	}
	return logger, cfg.Validate()
}

// decorate wraps base in the Filter/GlobalFields/Component/Sampling/
// Prometheus/Buffered/Hook chain, in that order. It is shared by the default
// Text/JSON base and a caller-supplied Sink -- which is what makes Sink a
// terminal handler for the pipeline instead of a total override of it.
func decorate(h slog.Handler, cfg Config) (slog.Handler, []Flusher) {
	var lifecycleHandlers []Flusher

	if len(cfg.FilterRules) > 0 {
		h = handler.NewFilterHandlerWithMode(h, cfg.FilterRules, cfg.FilterMode)
	}
	if len(cfg.GlobalFields) > 0 {
		h = handler.NewGlobalFieldsHandler(h, cfg.GlobalFields, true)
	}
	h = handler.NewComponentHandler(h)

	if cfg.Sampling.Enabled {
		sampler, err := handler.NewSamplingHandlerWithError(h, handler.SamplingConfig{
			Interval:    cfg.Sampling.Interval,
			Probability: cfg.Sampling.Probability,
			MinLevel:    parseLevel(cfg.Sampling.MinLevel),
			KeyFunc:     cfg.Sampling.KeyFunc,
			MaxKeys:     cfg.Sampling.MaxKeys,
		})
		if err != nil {
			// Report loudly rather than silently reinterpreting the
			// configuration. The handler is still usable: invalid values
			// were clamped to emitting defaults, so no record is lost.
			fmt.Fprintf(os.Stderr, "kit-logger: invalid sampling configuration, using emitting defaults: %v\n", err)
		}
		h = sampler
	}

	h = handler.NewPrometheusHandler(h)

	if cfg.BufferSize > 0 {
		buffered := handler.NewBufferedHandler(h, cfg.BufferSize)
		lifecycleHandlers = append(lifecycleHandlers, buffered)
		h = buffered
	}

	if cfg.Hook != nil {
		h = handler.NewHookHandler(h, cfg.Hook)
	}

	return h, lifecycleHandlers
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
