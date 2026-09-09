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
	logger      *slog.Logger
	levelVar    *slog.LevelVar
	rateState   *rateState
	counterHook CounterHook
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
// that invoked it. It returns 0 if no such frame exists, which makes
// ComponentHandler add no component group rather than a wrong one.
func callerPC() uintptr {
	var pcs [1]uintptr
	if runtime.Callers(callerPCSkip, pcs[:]) == 0 {
		return 0
	}
	return pcs[0]
}

func (l *SlogLogger) Debug(msg string, args ...any) {
	ctx := context.Background()
	if !l.logger.Enabled(ctx, slog.LevelDebug) {
		return
	}
	pc := callerPC()
	l.logRateAware(args, func(filtered []any) {
		l.emit(ctx, pc, slog.LevelDebug, msg, filtered)
	})
}

func (l *SlogLogger) Info(msg string, args ...any) {
	ctx := context.Background()
	if !l.logger.Enabled(ctx, slog.LevelInfo) {
		return
	}
	pc := callerPC()
	l.logRateAware(args, func(filtered []any) {
		l.emit(ctx, pc, slog.LevelInfo, msg, filtered)
	})
}

func (l *SlogLogger) Warn(msg string, args ...any) {
	ctx := context.Background()
	if !l.logger.Enabled(ctx, slog.LevelWarn) {
		return
	}
	pc := callerPC()
	l.logRateAware(args, func(filtered []any) {
		l.emit(ctx, pc, slog.LevelWarn, msg, filtered)
	})
}

func (l *SlogLogger) Error(msg string, args ...any) {
	ctx := context.Background()
	if !l.logger.Enabled(ctx, slog.LevelError) {
		return
	}
	pc := callerPC()
	l.logRateAware(args, func(filtered []any) {
		l.emit(ctx, pc, slog.LevelError, msg, filtered)
	})
}

func (l *SlogLogger) DebugContext(ctx context.Context, msg string, args ...any) {
	ctx = orBackground(ctx)
	if !l.logger.Enabled(ctx, slog.LevelDebug) {
		return
	}
	pc := callerPC()
	l.logRateAware(args, func(filtered []any) {
		l.emit(ctx, pc, slog.LevelDebug, msg, filtered)
	})
}

func (l *SlogLogger) InfoContext(ctx context.Context, msg string, args ...any) {
	ctx = orBackground(ctx)
	if !l.logger.Enabled(ctx, slog.LevelInfo) {
		return
	}
	pc := callerPC()
	l.logRateAware(args, func(filtered []any) {
		l.emit(ctx, pc, slog.LevelInfo, msg, filtered)
	})
}

func (l *SlogLogger) WarnContext(ctx context.Context, msg string, args ...any) {
	ctx = orBackground(ctx)
	if !l.logger.Enabled(ctx, slog.LevelWarn) {
		return
	}
	pc := callerPC()
	l.logRateAware(args, func(filtered []any) {
		l.emit(ctx, pc, slog.LevelWarn, msg, filtered)
	})
}

func (l *SlogLogger) ErrorContext(ctx context.Context, msg string, args ...any) {
	ctx = orBackground(ctx)
	if !l.logger.Enabled(ctx, slog.LevelError) {
		return
	}
	pc := callerPC()
	l.logRateAware(args, func(filtered []any) {
		l.emit(ctx, pc, slog.LevelError, msg, filtered)
	})
}

func (l *SlogLogger) Log(ctx context.Context, level slog.Level, msg string, args ...any) {
	ctx = orBackground(ctx)
	if !l.logger.Enabled(ctx, level) {
		return
	}
	pc := callerPC()
	l.logRateAware(args, func(filtered []any) {
		l.emit(ctx, pc, level, msg, filtered)
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

func (l *SlogLogger) With(args ...any) Logger {
	filtered, _, _ := extractLogOptions(args)
	return &SlogLogger{
		logger:      l.logger.With(filtered...),
		levelVar:    l.levelVar,
		rateState:   l.rateState,
		counterHook: l.counterHook,
	}
}

func (l *SlogLogger) WithContext(ctx context.Context) Logger {
	if globalContextFieldExtractor == nil {
		return l
	}
	return l.With(globalContextFieldExtractor(ctx)...)
}

func (l *SlogLogger) SetLevel(level slog.Level) {
	if l.levelVar != nil {
		l.levelVar.Set(level)
	}
}

func (l *SlogLogger) Sync() error {
	if flusher, ok := l.logger.Handler().(interface{ Flush() }); ok {
		flusher.Flush()
	}
	return nil
}

func (l *SlogLogger) Slog() *slog.Logger {
	return l.logger
}
