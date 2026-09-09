// Package kitlogtest holds kit-logger's test doubles: MockLogger and
// TestHandler. They exist to be imported from tests, never from production
// code, so they live outside pkg/logger and its sibling packages rather than
// shipping as part of the library's public API surface.
package kitlogtest

import (
	"context"
	"log/slog"
	"sync"

	kitlog "github.com/pablogore/kit-logger/pkg/logger"
)

// LogEntry represents a log captured by the MockLogger.
type LogEntry struct {
	Level   slog.Level
	Message string
	Args    []any
	Context context.Context
}

// MockLogger is a test logger that stores logs in memory.
//
// It implements ManagedLogger honestly: it records that Flush and Shutdown were
// called, lets a test inject the error each should return, and rejects records
// logged after Shutdown instead of quietly accepting them. A mock that always
// succeeds proves nothing about the code under test.
type MockLogger struct {
	mu      sync.Mutex
	Entries []LogEntry

	// FlushErr and ShutdownErr are returned by Flush and Shutdown. Set them to
	// exercise a caller's error path.
	FlushErr    error
	ShutdownErr error

	flushCalls    int
	shutdownCalls int
	shutdown      bool
	rejected      []error
}

var (
	_ kitlog.Logger        = (*MockLogger)(nil)
	_ kitlog.ManagedLogger = (*MockLogger)(nil)
)

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
	if m.shutdown {
		// Mirror SlogLogger: a shut-down logger records nothing and reports
		// the rejection instead of pretending the write succeeded.
		m.rejected = append(m.rejected, kitlog.ErrLoggerShutdown)
		return
	}
	m.Entries = append(m.Entries, LogEntry{
		Level:   level,
		Message: msg,
		Args:    args,
		Context: ctx,
	})
}

func (m *MockLogger) With(_ ...any) kitlog.Logger { return m }

func (m *MockLogger) WithContext(_ context.Context) kitlog.Logger { return m }

func (m *MockLogger) SetLevel(_ slog.Level) {}

// Flush records the call and returns FlushErr, or ErrLoggerShutdown once
// Shutdown has been called.
func (m *MockLogger) Flush(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.flushCalls++
	if m.shutdown {
		return kitlog.ErrLoggerShutdown
	}
	return m.FlushErr
}

// Shutdown records the call, stops accepting records and returns ShutdownErr.
// It is idempotent: a later call changes nothing but the call count and returns
// ShutdownErr again.
func (m *MockLogger) Shutdown(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.shutdownCalls++
	m.shutdown = true
	return m.ShutdownErr
}

// Sync is Flush with a background context.
//
// Deprecated: use Flush or Shutdown, which take a context.
func (m *MockLogger) Sync() error { return m.Flush(context.Background()) }

// FlushCalls is the number of times Flush (including Sync) was called.
func (m *MockLogger) FlushCalls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.flushCalls
}

// ShutdownCalls is the number of times Shutdown was called.
func (m *MockLogger) ShutdownCalls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.shutdownCalls
}

// RejectedErrors is one error per record submitted after Shutdown.
func (m *MockLogger) RejectedErrors() []error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]error(nil), m.rejected...)
}

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

// Slog returns a *slog.Logger backed by the same MockLogger: records emitted
// through it land in Entries exactly like a direct Info/Error call, so a test
// cannot observe divergent behavior depending on which face it logs through.
//
// The previous implementation handed back slog.New(slog.NewTextHandler(nil,
// nil)) -- a handler with a nil io.Writer that panics on its first write, a
// landmine for any code that calls MockLogger.Slog() (or SetGlobalAndSlogDefault
// with a MockLogger) and then logs through the standard library.
func (m *MockLogger) Slog() *slog.Logger {
	return slog.New(NewTestHandler(func(ctx context.Context, r slog.Record) {
		args := make([]any, 0, r.NumAttrs())
		r.Attrs(func(a slog.Attr) bool {
			args = append(args, a)
			return true
		})
		m.log(ctx, r.Level, r.Message, args...)
	}))
}
