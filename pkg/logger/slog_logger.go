package logger

import (
	"context"
	"log/slog"
)

// SlogLogger implements the Logger interface using the standard slog.Logger.
// Rate limiting (WithRateLimit/WithCounter) is applied inside log methods when those options appear in args.
type SlogLogger struct {
	logger      *slog.Logger
	levelVar    *slog.LevelVar
	rateState   *rateState
	counterHook CounterHook
}

func (l *SlogLogger) Debug(msg string, args ...any) {
	l.logRateAware(nil, slog.LevelDebug, msg, args, func(f []any) {
		l.logger.Debug(msg, f...)
	})
}

func (l *SlogLogger) Info(msg string, args ...any) {
	l.logRateAware(nil, slog.LevelInfo, msg, args, func(f []any) {
		l.logger.Info(msg, f...)
	})
}

func (l *SlogLogger) Warn(msg string, args ...any) {
	l.logRateAware(nil, slog.LevelWarn, msg, args, func(f []any) {
		l.logger.Warn(msg, f...)
	})
}

func (l *SlogLogger) Error(msg string, args ...any) {
	l.logRateAware(nil, slog.LevelError, msg, args, func(f []any) {
		l.logger.Error(msg, f...)
	})
}

func (l *SlogLogger) DebugContext(ctx context.Context, msg string, args ...any) {
	l.logRateAware(ctx, slog.LevelDebug, msg, args, func(f []any) {
		l.logger.DebugContext(ctx, msg, f...)
	})
}

func (l *SlogLogger) InfoContext(ctx context.Context, msg string, args ...any) {
	l.logRateAware(ctx, slog.LevelInfo, msg, args, func(f []any) {
		l.logger.InfoContext(ctx, msg, f...)
	})
}

func (l *SlogLogger) WarnContext(ctx context.Context, msg string, args ...any) {
	l.logRateAware(ctx, slog.LevelWarn, msg, args, func(f []any) {
		l.logger.WarnContext(ctx, msg, f...)
	})
}

func (l *SlogLogger) ErrorContext(ctx context.Context, msg string, args ...any) {
	l.logRateAware(ctx, slog.LevelError, msg, args, func(f []any) {
		l.logger.ErrorContext(ctx, msg, f...)
	})
}

func (l *SlogLogger) Log(ctx context.Context, level slog.Level, msg string, args ...any) {
	l.logRateAware(ctx, level, msg, args, func(f []any) {
		l.logger.Log(ctx, level, msg, f...)
	})
}

// logRateAware extracts rate options, applies rate limit (minimal lock scope), then emits without holding lock.
func (l *SlogLogger) logRateAware(ctx context.Context, level slog.Level, msg string, args []any, emit func([]any)) {
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
