package logger

import (
	"context"
	"log/slog"
	"runtime"
	"time"
)

// SlogLogger implements the Logger interface using the standard slog.Logger.
// Rate limiting (WithRateLimit/WithCounter) is applied inside log methods when those options appear in args.
//
// Source attribution: slog.Logger captures the program counter of *its own*
// caller, which for a wrapper like this one is the wrapper, not the consumer.
// Every exported method therefore captures the PC of its own caller with
// callerPC and builds the record itself, so ComponentHandler (and
// slog.HandlerOptions.AddSource) attribute the record to consumer code.
type SlogLogger struct {
	logger   *slog.Logger
	levelVar *slog.LevelVar

	// rateState is captured at construction and deliberately shared by every
	// derived logger. A per-request logger built with With must not come with
	// a fresh set of limiters: that would let the one usage pattern the rate
	// limit exists to protect -- a hot path that derives a logger per call --
	// defeat it entirely. It is nil for a SlogLogger built by hand, which then
	// simply has no rate limiting.
	rateState *rateState

	counterHook CounterHook

	// lifecycle is captured at construction and shared by every derived
	// logger, so Flush and Shutdown reach the buffer no matter how many
	// decorators wrap it and no matter how the logger was derived. It is nil
	// for a SlogLogger built by hand, which then has nothing to flush.
	lifecycle *lifecycleGroup

	// contextFields is this logger's own context extractor, set once at
	// construction from Config.ContextFields and never written again. Being
	// immutable is what makes WithContext race-free without a single atomic
	// operation on the hot path.
	contextFields ContextFieldExtractorFunc

	// callerSkip is the number of extra stack frames to skip past the
	// caller of an exported log method when attributing a record. It is
	// zero for a logger that consumers call directly and n for one wrapped
	// by n layers of adapter; see WithCallerSkip.
	callerSkip int
}

// callerPCSkip is the runtime.Callers skip depth that lands on the caller of an
// exported log method. The frames it skips are, in order:
//
//	0  runtime.Callers
//	1  callerPC
//	2  the exported log method (Info, InfoContext, Log, ...)
//	3  the consumer call site  <- the frame we want
//
// The depth is only valid when callerPC is invoked directly from an exported
// method. Never call it from a shared helper reached through another frame or
// through a closure: that adds a frame and silently misattributes every log.
// TestSlogLogger_AttributesTheConsumerCallSite pins this for every method.
const callerPCSkip = 3

// callerPC returns the program counter of the caller of the exported log method
// that invoked it, extra frames further up the stack. It returns 0 if no such
// frame exists, which makes ComponentHandler add no component group rather
// than a wrong one.
func callerPC(extra int) uintptr {
	var pcs [1]uintptr
	if runtime.Callers(callerPCSkip+extra, pcs[:]) == 0 {
		return 0
	}
	return pcs[0]
}

func (l *SlogLogger) Debug(msg string, args ...any) {
	ctx := context.Background()
	if !l.logger.Enabled(ctx, slog.LevelDebug) {
		return
	}
	pc := callerPC(l.callerSkip)
	l.logRateAware(args, func(filtered []any) {
		l.emit(ctx, pc, slog.LevelDebug, msg, filtered)
	})
}

func (l *SlogLogger) Info(msg string, args ...any) {
	ctx := context.Background()
	if !l.logger.Enabled(ctx, slog.LevelInfo) {
		return
	}
	pc := callerPC(l.callerSkip)
	l.logRateAware(args, func(filtered []any) {
		l.emit(ctx, pc, slog.LevelInfo, msg, filtered)
	})
}

func (l *SlogLogger) Warn(msg string, args ...any) {
	ctx := context.Background()
	if !l.logger.Enabled(ctx, slog.LevelWarn) {
		return
	}
	pc := callerPC(l.callerSkip)
	l.logRateAware(args, func(filtered []any) {
		l.emit(ctx, pc, slog.LevelWarn, msg, filtered)
	})
}

func (l *SlogLogger) Error(msg string, args ...any) {
	ctx := context.Background()
	if !l.logger.Enabled(ctx, slog.LevelError) {
		return
	}
	pc := callerPC(l.callerSkip)
	l.logRateAware(args, func(filtered []any) {
		l.emit(ctx, pc, slog.LevelError, msg, filtered)
	})
}

func (l *SlogLogger) DebugContext(ctx context.Context, msg string, args ...any) {
	ctx = orBackground(ctx)
	if !l.logger.Enabled(ctx, slog.LevelDebug) {
		return
	}
	pc := callerPC(l.callerSkip)
	l.logRateAware(args, func(filtered []any) {
		l.emitContext(ctx, pc, slog.LevelDebug, msg, filtered)
	})
}

func (l *SlogLogger) InfoContext(ctx context.Context, msg string, args ...any) {
	ctx = orBackground(ctx)
	if !l.logger.Enabled(ctx, slog.LevelInfo) {
		return
	}
	pc := callerPC(l.callerSkip)
	l.logRateAware(args, func(filtered []any) {
		l.emitContext(ctx, pc, slog.LevelInfo, msg, filtered)
	})
}

func (l *SlogLogger) WarnContext(ctx context.Context, msg string, args ...any) {
	ctx = orBackground(ctx)
	if !l.logger.Enabled(ctx, slog.LevelWarn) {
		return
	}
	pc := callerPC(l.callerSkip)
	l.logRateAware(args, func(filtered []any) {
		l.emitContext(ctx, pc, slog.LevelWarn, msg, filtered)
	})
}

func (l *SlogLogger) ErrorContext(ctx context.Context, msg string, args ...any) {
	ctx = orBackground(ctx)
	if !l.logger.Enabled(ctx, slog.LevelError) {
		return
	}
	pc := callerPC(l.callerSkip)
	l.logRateAware(args, func(filtered []any) {
		l.emitContext(ctx, pc, slog.LevelError, msg, filtered)
	})
}

func (l *SlogLogger) Log(ctx context.Context, level slog.Level, msg string, args ...any) {
	ctx = orBackground(ctx)
	if !l.logger.Enabled(ctx, level) {
		return
	}
	pc := callerPC(l.callerSkip)
	l.logRateAware(args, func(filtered []any) {
		l.emitContext(ctx, pc, level, msg, filtered)
	})
}

// orBackground normalizes a nil context the way slog.Logger does.
func orBackground(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

// logRateAware extracts rate options, applies the rate limit (minimal lock scope),
// then emits without holding the lock and finally reports the counter.
//
// It is only reached once the level is known to be enabled, so a suppressed
// level consumes no rate-limit token and fires no counter.
func (l *SlogLogger) logRateAware(args []any, emit func([]any)) {
	// A logger whose shutdown has begun accepts nothing: no record, no
	// rate-limit token and no counter increment. Dropping here rather than
	// inside emit is what keeps such a call free of side effects, and the gate
	// closes when the shutdown starts rather than when it ends so there is no
	// window where this passes and the handler underneath rejects.
	if l.lifecycle.isStopping() {
		l.lifecycle.rejectRecord()
		return
	}
	filtered, rateLimit, counter := extractLogOptions(args)
	allow, suppressed, addSuppressedField := l.rateState.shouldLog(rateLimit)
	if !allow {
		return
	}
	if addSuppressedField {
		filtered = append(filtered, "suppressed_count", suppressed)
	}
	emit(filtered)
	if counter != nil && counter.MetricName != "" && l.counterHook != nil {
		l.counterHook.Inc(counter.MetricName)
	}
}

// emit builds the record with the call-site PC captured by the exported method
// and hands it to the handler chain.
//
// It goes through l.logger.Handler() rather than l.logger.Info and friends
// because those would overwrite the PC with the address of this frame. The
// Enabled check that slog.Logger would have done has already happened in the
// exported method.
func (l *SlogLogger) emit(ctx context.Context, pc uintptr, level slog.Level, msg string, args []any) {
	record := slog.NewRecord(time.Now(), level, msg, pc)
	record.Add(args...)
	_ = l.logger.Handler().Handle(ctx, record)
}

// emitContext is emit plus the fields this logger's context extractor derives
// from ctx. Every *Context log method goes through it.
//
// Before this existed, the extractor ran only in WithContext, so the ergonomic
// call — log.InfoContext(ctx, "msg") — silently carried none of the fields the
// consumer had configured. The surprising half of the API was the one everybody
// reaches for.
//
// Note that log.WithContext(ctx).InfoContext(ctx, "msg") now applies the
// extractor twice. Pick one: WithContext for a derived logger reused across
// several calls, the *Context methods for a single call.
func (l *SlogLogger) emitContext(ctx context.Context, pc uintptr, level slog.Level, msg string, args []any) {
	if fields := l.contextAttrs(ctx); len(fields) > 0 {
		// args is the slice extractLogOptions built, freshly allocated per
		// call, so appending to it cannot write into the caller's storage.
		args = append(args, fields...)
	}
	l.emit(ctx, pc, level, msg, args)
}

// contextAttrs returns the fields this logger's extractor derives from ctx, or
// nil when no extractor is configured.
//
// A logger built with Config.ContextFields uses its own immutable extractor and
// touches no package-level state. Only a logger without one falls back to the
// deprecated process-wide extractor, and that read is atomic.
func (l *SlogLogger) contextAttrs(ctx context.Context) []any {
	extract := l.contextFields
	if extract == nil {
		extract = contextFieldExtractor()
	}
	if extract == nil {
		return nil
	}
	return extract(ctx)
}

// With returns a logger carrying args on every record.
//
// The derived logger shares this one's rate-limit state on purpose. Deriving a
// logger per request or per handler is the normal thing to do, and if each
// derived logger got its own limiters a rate-limited key would be limited once
// per derived logger -- which is to say, not at all. The lifecycle and the
// counter hook are shared for the same reason.
//
// Rate-limit and counter options passed here are stripped rather than
// remembered: they describe a single record, not a logger.
func (l *SlogLogger) With(args ...any) Logger {
	filtered, _, _ := extractLogOptions(args)
	return &SlogLogger{
		logger:        l.logger.With(filtered...),
		levelVar:      l.levelVar,
		rateState:     l.rateState,
		counterHook:   l.counterHook,
		lifecycle:     l.lifecycle,
		contextFields: l.contextFields,
		callerSkip:    l.callerSkip,
	}
}

// WithCallerSkip returns a logger that attributes every record n stack frames
// further up than this one does. It is for adapters: a wrapper that
// implements some other logging interface on top of this Logger sits one
// frame between the consumer and the exported log method, so without it every
// record is attributed to the wrapper's own file.
//
//	type adapter struct{ log logger.Logger }
//
//	func newAdapter(log logger.Logger) *adapter {
//	    if s, ok := log.(logger.CallerSkipper); ok {
//	        log = s.WithCallerSkip(1)
//	    }
//	    return &adapter{log: log}
//	}
//
//	func (a *adapter) Info(msg string, args ...any) { a.log.Info(msg, args...) }
//
// Skips add up: WithCallerSkip(1).WithCallerSkip(1) skips two frames, so a
// wrapper around a wrapper composes without either knowing about the other.
// A non-positive n returns the receiver. The derived logger shares this one's
// rate-limit state, lifecycle and counter hook, exactly like With.
func (l *SlogLogger) WithCallerSkip(n int) Logger {
	if n <= 0 {
		return l
	}
	derived := *l
	derived.callerSkip = l.callerSkip + n
	return &derived
}

// WithContext returns a logger carrying the fields extracted from ctx.
//
// A logger configured with Config.ContextFields uses its own extractor and
// touches no package-level state at all. Only a logger without one falls back
// to the deprecated process-wide extractor, and that read is atomic.
func (l *SlogLogger) WithContext(ctx context.Context) Logger {
	fields := l.contextAttrs(ctx)
	if len(fields) == 0 {
		return l
	}
	return l.With(fields...)
}

func (l *SlogLogger) SetLevel(level slog.Level) {
	if l.levelVar != nil {
		l.levelVar.Set(level)
	}
}

// Flush returns once every record accepted before the call has been delivered
// downstream, or ctx expires — in which case it returns ctx.Err(). It does not
// stop the logger: records logged afterwards are still delivered.
//
// It returns ErrLoggerShutdown once the logger's shutdown has begun — the gate
// closes when Shutdown starts, not when it finishes — because there is no
// longer a "deliver what I just logged" to honour.
func (l *SlogLogger) Flush(ctx context.Context) error {
	return l.lifecycle.flush(ctx)
}

// Shutdown stops accepting records, delivers everything already accepted and
// releases the resources held by the pipeline. It returns ctx.Err() if ctx
// expires while this call is still waiting.
//
// Shutdown is idempotent and safe to call concurrently. It is started once and
// shared: ctx bounds how long this call waits for it, not how long the drain is
// allowed to take, so a caller with a tight deadline cannot cut short a drain
// another caller was willing to wait for. Every caller that waits to the end
// sees the same result.
//
// Records logged from the moment Shutdown starts are discarded; they never
// panic and never block.
func (l *SlogLogger) Shutdown(ctx context.Context) error {
	return l.lifecycle.shutdown(ctx)
}

// Rejected is the number of records discarded because the logger's shutdown had
// already begun when they were submitted.
//
// Records rejected or dropped by the buffer itself are counted by the buffered
// handler, not here. Together the three counters account for every record a
// consumer submitted.
func (l *SlogLogger) Rejected() uint64 {
	return l.lifecycle.rejectedCount()
}

// RateLimitEvicted is the number of tracked rate-limit keys reclaimed to stay
// within Config.RateLimit.MaxKeys.
//
// A number that keeps climbing means the key cardinality has outgrown the
// bound: keys are being forgotten and re-created, so records that a stable key
// would have suppressed are being emitted. Either lower the cardinality of the
// keys or raise MaxKeys.
//
// Rate-limit state is shared with every logger derived through With, so this
// counter reports the whole family, not this logger alone. It is zero for a
// SlogLogger assembled by hand, which has no rate-limit state.
func (l *SlogLogger) RateLimitEvicted() uint64 {
	if l.rateState == nil {
		return 0
	}
	return l.rateState.evicted.Load()
}

// RateLimitTrackedKeys is the number of rate-limit keys currently tracked. It
// never exceeds Config.RateLimit.MaxKeys.
//
// Rate-limit state is shared with every logger derived through With, so this
// reports the whole family, not this logger alone. It is zero for a SlogLogger
// assembled by hand, which has no rate-limit state.
func (l *SlogLogger) RateLimitTrackedKeys() int {
	if l.rateState == nil {
		return 0
	}
	l.rateState.mu.Lock()
	defer l.rateState.mu.Unlock()
	return len(l.rateState.limiters)
}

// RateLimitInvalid is the number of rate-limit keys created with a non-positive
// interval. Those keys are emitted unlimited, so any non-zero value here is a
// call site that believes it is rate limited and is not.
//
// Rate-limit state is shared with every logger derived through With, so this
// counter reports the whole family, not this logger alone. It is zero for a
// SlogLogger assembled by hand, which has no rate-limit state.
func (l *SlogLogger) RateLimitInvalid() uint64 {
	if l.rateState == nil {
		return 0
	}
	return l.rateState.invalidConfigs.Load()
}

// RateLimitConflicts is the number of times a key was presented with an
// interval other than the one it was created with. The first interval is kept,
// so a non-zero value means some call site's interval is being ignored.
//
// Rate-limit state is shared with every logger derived through With, so this
// counter reports the whole family, not this logger alone. It is zero for a
// SlogLogger assembled by hand, which has no rate-limit state.
func (l *SlogLogger) RateLimitConflicts() uint64 {
	if l.rateState == nil {
		return 0
	}
	return l.rateState.conflicts.Load()
}

// Sync is Flush with a background context.
//
// Deprecated: use Flush or Shutdown, which take a context and therefore a
// deadline. Sync remains on the Logger interface for source compatibility, and
// unlike the version it replaces it actually flushes.
func (l *SlogLogger) Sync() error {
	return l.Flush(context.Background())
}

func (l *SlogLogger) Slog() *slog.Logger {
	return l.logger
}
