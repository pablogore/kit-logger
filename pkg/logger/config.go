package logger

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"slices"
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
	// constructs exactly one logger.
	globalOnce sync.Once
)

// Config holds the configuration for the logger.
type Config struct {
	BufferSize  int
	FilterRules []handler.FilterRule
	// FilterMode selects what a FilterRules match does. Defaults to
	// handler.ModeDrop, matching this field's pre-existing behavior.
	FilterMode handler.Mode
	Format     Format
	// FormatString is a deprecated bridge for Format: parsed with ParseFormat
	// and used whenever it is non-empty. If Format was also explicitly set to
	// a value other than its zero value (FormatText) and it disagrees with
	// FormatString, Validate reports the conflict instead of picking one
	// silently.
	//
	// That guarantee has one gap: FormatText is Format's zero value, so a
	// Config that never touches Format is indistinguishable from one that
	// explicitly sets Format: FormatText. Config{FormatString: "json"} and
	// Config{Format: FormatText, FormatString: "json"} are the same struct
	// value, so both resolve to FormatJSON with no error -- Validate cannot
	// detect that as a conflict. Do not combine FormatString with an
	// explicit Format: FormatText if you need that combination rejected.
	//
	// Deprecated: set Format directly; do not combine it with FormatString.
	FormatString string

	// GlobalFields describe the process (service, env, version) and are
	// attached to every record, first, in sorted key order. They are pinned:
	// an attr with the same key from With, from a context extractor or from
	// the call site never overrides them, and the line never carries the key
	// twice. See handler.DedupHandler for the full semantics.
	//
	// The one key a global field cannot claim is "source" while AddSource is
	// set: the call-site group owns it, the global is ignored and Validate
	// reports it.
	GlobalFields map[string]string

	// AddSource attaches a "source" group (function, file, line) naming the
	// call site of every record, in the shape slog's own AddSource uses.
	// Defaults to false: the attribution is three fields on every line, which
	// is noise in production and belongs in a development or debugging
	// configuration. See handler.SourceHandler.
	AddSource bool

	// Sink is the terminal handler that receives fully decorated records:
	// FilterRules, GlobalFields, component attribution, Sampling,
	// MetricsHandler, BufferSize and Hook are all applied on top of it, in
	// the same order as the default Text/JSON handler. When nil, that
	// default handler (over Writer, chosen by Format) is used instead.
	//
	// SetLevel works uniformly here too: Sink is wrapped in an internal
	// handler gated on the same LevelVar the default path uses, so a custom
	// sink gets dynamic level control for free.
	Sink slog.Handler

	// Handler is a deprecated alias for Sink; Sink wins if both are set.
	//
	// Deprecated: use Sink. Handler used to bypass the entire pipeline --
	// FilterRules, GlobalFields, component attribution, Sampling,
	// MetricsHandler, BufferSize, Hook and SetLevel were all silently ignored
	// when it was set.
	// It now behaves exactly like Sink. Use PipelineOverride for the old
	// bypass behavior.
	Handler slog.Handler

	// PipelineOverride replaces the handler-decoration pipeline verbatim,
	// exactly as Handler used to: Sink/Handler, FilterRules, GlobalFields,
	// Sampling, MetricsHandler, BufferSize, Hook, Writer, Format and Level
	// are all ignored, and Validate reports each one that was set as an
	// error naming it. This is the escape hatch for a caller that must not
	// have its chain decorated, e.g. one that already assembled its own
	// Filter/Sampling/Buffer stack.
	//
	// Logger-level behavior that lives outside that chain is unaffected:
	// RateLimit, ContextFields and the outer ContextHandler still apply.
	PipelineOverride slog.Handler

	Hook func(ctx context.Context, r slog.Record) (context.Context, bool)

	// MetricsHandler wraps the pipeline with instrumentation when non-nil.
	// Default: no instrumentation -- New inserts nothing, and no record
	// touches any metrics collector.
	//
	// Use pkg/logger/prometheus.New to build one:
	//
	//	cfg.MetricsHandler = func(next slog.Handler) (slog.Handler, error) {
	//	    return kitprom.New(next, prometheus.DefaultRegisterer, kitprom.Options{})
	//	}
	//
	// This is a function seam rather than a typed field or a bool flag so
	// that pkg/logger never imports a metrics package: the consumer imports
	// pkg/logger/prometheus, this package does not. An error returned here
	// is joined into NewWithError's result; New (which discards errors)
	// leaves the pipeline unwrapped rather than fail the whole construction.
	MetricsHandler func(next slog.Handler) (slog.Handler, error)

	// Keys no longer has any effect.
	//
	// Deprecated: unused; kept only so existing struct literals compile, and
	// will be removed in a future release.
	Keys []string

	Level Level
	// LevelString is a deprecated bridge for Level. See FormatString's doc
	// comment for the precedence rule and its zero-value gap: LevelInfo is
	// Level's zero value, so Config{LevelString: "debug"} and
	// Config{Level: LevelInfo, LevelString: "debug"} are the same struct
	// value and both resolve to LevelDebug without Validate flagging a
	// conflict.
	//
	// Deprecated: set Level directly; do not combine it with LevelString.
	LevelString string
	RateLimit   RateLimitConfig
	Sampling    SamplingConfig

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
// around: an unparsable LevelString/FormatString/Sampling.MinLevelString, a
// typed field explicitly set to a non-zero value that disagrees with its
// legacy string bridge, an out-of-range Format, a negative BufferSize, and
// everything Sampling.Validate already checked.
//
// Validate cannot detect every typed/legacy disagreement: when the typed
// field is left at its zero value (LevelInfo or FormatText), that is
// indistinguishable from the caller having explicitly chosen it, so the
// legacy string wins silently in that case. See LevelString's doc comment.
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
		if cfg.MetricsHandler != nil {
			ignored = append(ignored, "MetricsHandler")
		}
		if cfg.Writer != nil {
			ignored = append(ignored, "Writer")
		}
		if cfg.Format != FormatText || cfg.FormatString != "" {
			ignored = append(ignored, "Format")
		}
		if cfg.Level != LevelInfo || cfg.LevelString != "" {
			ignored = append(ignored, "Level")
		}
		if len(ignored) > 0 {
			errs = append(errs, fmt.Errorf("Config: PipelineOverride is set, which ignores: %s", strings.Join(ignored, ", ")))
		}
	}

	if _, err := resolveLevel("Level", cfg.Level, cfg.LevelString); err != nil {
		errs = append(errs, err)
	}
	if _, err := resolveFormat("Format", cfg.Format, cfg.FormatString); err != nil {
		errs = append(errs, err)
	}
	if cfg.Format > FormatConsole {
		errs = append(errs, fmt.Errorf("Config: Format(%d) is not a valid Format value", cfg.Format))
	}
	if cfg.BufferSize < 0 {
		errs = append(errs, fmt.Errorf("Config: BufferSize must be >= 0, got %d", cfg.BufferSize))
	}
	if _, ok := cfg.GlobalFields[slog.SourceKey]; ok && cfg.AddSource {
		errs = append(errs, fmt.Errorf("Config: GlobalFields[%q] is ignored when AddSource is set; the call-site group owns that key", slog.SourceKey))
	}
	if _, err := resolveLevel("Sampling.MinLevel", cfg.Sampling.MinLevel, cfg.Sampling.MinLevelString); err != nil {
		errs = append(errs, err)
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
	MinLevel Level
	// MinLevelString is a deprecated bridge for MinLevel, resolved with the
	// same precedence rule -- and the same zero-value gap -- as
	// Config.LevelString.
	//
	// Deprecated: set MinLevel directly; do not combine it with
	// MinLevelString.
	MinLevelString string

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
		// No startup line: a library must not write to stdout because a
		// consumer touched a global. The lazy default is a fallback, not
		// an event worth a log record.
		l := New(Config{})
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
// A Config that fails Validate still produces a usable logger -- New resolves
// the invalid combination the same way NewWithError does and does not report
// it anywhere. A library constructor must not perform I/O the caller did not
// ask for; use NewWithError to receive the error instead.
func New(cfg Config, opts ...Option) Logger {
	l, _ := NewWithError(cfg, opts...)
	return l
}

// NewWithError is New, but also returns cfg.Validate's error. The returned
// Logger is always usable: an invalid combination is resolved the same way
// New resolves it (see Validate).
func NewWithError(cfg Config, opts ...Option) (Logger, error) {
	level, _ := resolveLevel("Level", cfg.Level, cfg.LevelString)
	format, _ := resolveFormat("Format", cfg.Format, cfg.FormatString)
	levelVar := new(slog.LevelVar)
	levelVar.Set(level.slogLevel())

	// lifecycleHandlers is captured while the pipeline is being assembled
	// rather than rediscovered at Flush/Shutdown time. Walking the chain at
	// call time is wrong twice over: any handler that does not implement
	// Unwrap severs the walk, and after With the outermost handler is a
	// derived one, not the root that owns the buffer.
	var lifecycleHandlers []Flusher
	var h slog.Handler
	var metricsErr error

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
			base = newTerminalHandler(w, format, levelVar)
		}

		var extra []Flusher
		h, extra, metricsErr = decorate(base, cfg)
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
		// The values go under a "config" group: a bare "level" attr would
		// collide with the level key every slog handler already writes.
		slogLogger.Info("Logger initialized",
			slog.Group("config", "level", level.String(), "format", format.String()))
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
	return logger, errors.Join(cfg.Validate(), metricsErr)
}

// newTerminalHandler builds the default sink for format over w.
//
// The JSON handler renders a time.Duration as a string ("1.5s") instead of
// slog's default nanosecond integer, so JSON and text agree and a reader does
// not have to count digits. Values that carry a unit in their key
// (duration_ms) are plain integers and are left alone.
func newTerminalHandler(w io.Writer, format Format, level slog.Leveler) slog.Handler {
	switch format {
	case FormatJSON:
		return slog.NewJSONHandler(w, &slog.HandlerOptions{Level: level, ReplaceAttr: durationAsString})
	case FormatConsole:
		return handler.NewConsoleHandler(w, handler.ConsoleOptions{Level: level})
	default:
		return slog.NewTextHandler(w, &slog.HandlerOptions{Level: level})
	}
}

// durationAsString is the ReplaceAttr that makes the JSON handler print a
// time.Duration the way the text handler already does.
func durationAsString(_ []string, a slog.Attr) slog.Attr {
	if a.Value.Kind() == slog.KindDuration {
		a.Value = slog.StringValue(a.Value.Duration().String())
	}
	return a
}

// decorate wraps base in the Dedup/Filter/Source/Sampling/MetricsHandler/
// Buffered/Hook chain, in that order. It is shared by the default base and a
// caller-supplied Sink -- which is what makes Sink a terminal handler for the
// pipeline instead of a total override of it.
//
// DedupHandler is innermost on purpose: it is the one stage that must see
// every attr, from With, from the record and from the decorators above it,
// after all of them have had their say. GlobalFields are its pinned attrs,
// so they come first on the line and cannot be overridden.
//
// A non-nil error means cfg.MetricsHandler itself failed -- either it
// returned an error directly (e.g. a Prometheus registry collision), or it
// broke its contract by returning a nil handler with a nil error. Either way
// the returned handler is still fully usable, just without instrumentation,
// since decorate must not fail the whole pipeline over an optional seam.
func decorate(h slog.Handler, cfg Config) (slog.Handler, []Flusher, error) {
	var lifecycleHandlers []Flusher
	var metricsErr error

	h = handler.NewDedupHandler(h, dedupOptions(cfg))

	if len(cfg.FilterRules) > 0 {
		h = handler.NewFilterHandlerWithMode(h, cfg.FilterRules, cfg.FilterMode)
	}
	if cfg.AddSource {
		h = handler.NewSourceHandler(h)
	}

	if cfg.Sampling.Enabled {
		// decorate performs no I/O of its own -- reporting an invalid
		// Config is Validate's job alone, surfaced through NewWithError.
		// SamplingConfig.Validate checks exactly the fields passed here, so
		// Config.Validate already reports the identical problem; the
		// error-swallowing constructor just applies the same clamped,
		// emitting-safe defaults without a second, redundant report.
		minLevel, _ := resolveLevel("Sampling.MinLevel", cfg.Sampling.MinLevel, cfg.Sampling.MinLevelString)
		h = handler.NewSamplingHandler(h, handler.SamplingConfig{
			Interval:    cfg.Sampling.Interval,
			Probability: cfg.Sampling.Probability,
			MinLevel:    minLevel.slogLevel(),
			KeyFunc:     cfg.Sampling.KeyFunc,
			MaxKeys:     cfg.Sampling.MaxKeys,
		})
	}

	if cfg.MetricsHandler != nil {
		wrapped, err := cfg.MetricsHandler(h)
		switch {
		case err != nil:
			metricsErr = fmt.Errorf("Config: MetricsHandler: %w", err)
		case wrapped == nil:
			metricsErr = errors.New("Config: MetricsHandler returned nil handler without error")
		default:
			h = wrapped
		}
	}

	if cfg.BufferSize > 0 {
		buffered := handler.NewBufferedHandler(h, cfg.BufferSize)
		lifecycleHandlers = append(lifecycleHandlers, buffered)
		h = buffered
	}

	if cfg.Hook != nil {
		h = handler.NewHookHandler(h, cfg.Hook)
	}

	return h, lifecycleHandlers, metricsErr
}

// dedupOptions derives the DedupHandler configuration from cfg: GlobalFields
// become the pinned attrs, in sorted key order so two replicas built from the
// same config emit the same line shape.
//
// "source" is deliberately not reserved. SourceHandler sits above the dedup
// stage and delivers its group as a record attr, so reserving the key would
// rename the pipeline's own attribution. A caller attr named "source" is
// simply overridden by the group when AddSource is on, under the usual
// last-wins rule, and left alone otherwise.
//
// A pinned attr, on the other hand, wins over everything -- including that
// group -- so a GlobalFields["source"] would silently suppress the
// attribution AddSource asked for. When AddSource is set, "source" is
// therefore left out of the pinned set (and Validate reports it): the
// pipeline's own source group is the one that reaches the line.
func dedupOptions(cfg Config) handler.DedupOptions {
	keys := make([]string, 0, len(cfg.GlobalFields))
	for k := range cfg.GlobalFields {
		if cfg.AddSource && k == slog.SourceKey {
			continue
		}
		keys = append(keys, k)
	}
	slices.Sort(keys)
	pinned := make([]slog.Attr, 0, len(keys))
	for _, k := range keys {
		pinned = append(pinned, slog.String(k, cfg.GlobalFields[k]))
	}
	return handler.DedupOptions{Pinned: pinned}
}
