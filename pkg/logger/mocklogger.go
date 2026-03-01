package logger

import (
	"context"
	"log/slog"
	"sync"
)

// LogEntry represents a log captured by the MockLogger.
type LogEntry struct {
	Level   slog.Level
	Message string
	Args    []any
	Context context.Context
}

// MockLogger is a test logger that stores logs in memory.
type MockLogger struct {
	mu      sync.Mutex
	Entries []LogEntry
}

func NewMockLogger() *MockLogger {
	return &MockLogger{}
}

func (m *MockLogger) Debug(msg string, args ...any) { m.log(nil, slog.LevelDebug, msg, args...) }

func (m *MockLogger) Info(msg string, args ...any) { m.log(nil, slog.LevelInfo, msg, args...) }

func (m *MockLogger) Warn(msg string, args ...any) { m.log(nil, slog.LevelWarn, msg, args...) }

func (m *MockLogger) Error(msg string, args ...any) { m.log(nil, slog.LevelError, msg, args...) }

func (m *MockLogger) DebugContext(ctx context.Context, msg string, args ...any) {
	m.log(ctx, slog.LevelDebug, msg, args...)
}

func (m *MockLogger) InfoContext(ctx context.Context, msg string, args ...any) {
	m.log(ctx, slog.LevelInfo, msg, args...)
}

func (m *MockLogger) WarnContext(ctx context.Context, msg string, args ...any) {
	m.log(ctx, slog.LevelWarn, msg, args...)
}

func (m *MockLogger) ErrorContext(ctx context.Context, msg string, args ...any) {
	m.log(ctx, slog.LevelError, msg, args...)
}

func (m *MockLogger) Log(ctx context.Context, level slog.Level, msg string, args ...any) {
	m.log(ctx, level, msg, args...)
}

func (m *MockLogger) log(ctx context.Context, level slog.Level, msg string, args ...any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Entries = append(m.Entries, LogEntry{
		Level:   level,
		Message: msg,
		Args:    args,
		Context: ctx,
	})
}

func (m *MockLogger) With(_ ...any) Logger { return m }

func (m *MockLogger) WithContext(_ context.Context) Logger { return m }

func (m *MockLogger) SetLevel(_ slog.Level) {}

func (m *MockLogger) Sync() error { return nil }

func (m *MockLogger) HasMessage(expected string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, entry := range m.Entries {
		if entry.Message == expected {
			return true
		}
	}
	return false
}

func (m *MockLogger) Slog() *slog.Logger {
	return slog.New(slog.NewTextHandler(nil, nil))
}
