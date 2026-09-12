package logger

import (
	"context"
	"log/slog"
)

// Logger defines the main methods of the logging framework.
type Logger interface {
	Debug(msg string, args ...any)
	Info(msg string, args ...any)
	Warn(msg string, args ...any)
	Error(msg string, args ...any)

	DebugContext(ctx context.Context, msg string, args ...any)
	InfoContext(ctx context.Context, msg string, args ...any)
	WarnContext(ctx context.Context, msg string, args ...any)
	ErrorContext(ctx context.Context, msg string, args ...any)

	Log(ctx context.Context, level slog.Level, msg string, args ...any)
	With(args ...any) Logger
	WithContext(ctx context.Context) Logger
	SetLevel(level slog.Level)
	Sync() error
	Slog() *slog.Logger
}

// CallerSkipper is implemented by loggers that can attribute records to a
// frame further up the stack than their direct caller. Adapters that wrap a
// Logger behind another logging interface use it so records name the real
// call site instead of the adapter; see SlogLogger.WithCallerSkip.
//
// It is an optional interface rather than a Logger method so that existing
// Logger implementations keep compiling; consumers assert for it.
type CallerSkipper interface {
	WithCallerSkip(n int) Logger
}
