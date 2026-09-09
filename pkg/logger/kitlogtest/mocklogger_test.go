package kitlogtest

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	kitlog "github.com/pablogore/kit-logger/pkg/logger"
)

func TestNewMockLogger(t *testing.T) {
	logger := NewMockLogger()
	assert.NotNil(t, logger)
	assert.IsType(t, &MockLogger{}, logger)
	assert.Empty(t, logger.Entries)
}

func TestMockLogger_Debug(t *testing.T) {
	logger := NewMockLogger()

	logger.Debug("debug message", "key", "value")

	assert.Len(t, logger.Entries, 1)
	entry := logger.Entries[0]
	assert.Equal(t, slog.LevelDebug, entry.Level)
	assert.Equal(t, "debug message", entry.Message)
	assert.Equal(t, []any{"key", "value"}, entry.Args)
	assert.Nil(t, entry.Context)
}

func TestMockLogger_Info(t *testing.T) {
	logger := NewMockLogger()

	logger.Info("info message", "key", "value")

	assert.Len(t, logger.Entries, 1)
	entry := logger.Entries[0]
	assert.Equal(t, slog.LevelInfo, entry.Level)
	assert.Equal(t, "info message", entry.Message)
	assert.Equal(t, []any{"key", "value"}, entry.Args)
	assert.Nil(t, entry.Context)
}

func TestMockLogger_Warn(t *testing.T) {
	logger := NewMockLogger()

	logger.Warn("warn message", "key", "value")

	assert.Len(t, logger.Entries, 1)
	entry := logger.Entries[0]
	assert.Equal(t, slog.LevelWarn, entry.Level)
	assert.Equal(t, "warn message", entry.Message)
	assert.Equal(t, []any{"key", "value"}, entry.Args)
	assert.Nil(t, entry.Context)
}

func TestMockLogger_Error(t *testing.T) {
	logger := NewMockLogger()

	logger.Error("error message", "key", "value")

	assert.Len(t, logger.Entries, 1)
	entry := logger.Entries[0]
	assert.Equal(t, slog.LevelError, entry.Level)
	assert.Equal(t, "error message", entry.Message)
	assert.Equal(t, []any{"key", "value"}, entry.Args)
	assert.Nil(t, entry.Context)
}

func TestMockLogger_DebugContext(t *testing.T) {
	logger := NewMockLogger()
	ctx := context.WithValue(context.Background(), "test_key", "test_value")

	logger.DebugContext(ctx, "debug context message", "key", "value")

	assert.Len(t, logger.Entries, 1)
	entry := logger.Entries[0]
	assert.Equal(t, slog.LevelDebug, entry.Level)
	assert.Equal(t, "debug context message", entry.Message)
	assert.Equal(t, []any{"key", "value"}, entry.Args)
	assert.Equal(t, ctx, entry.Context)
}

func TestMockLogger_InfoContext(t *testing.T) {
	logger := NewMockLogger()
	ctx := context.WithValue(context.Background(), "test_key", "test_value")

	logger.InfoContext(ctx, "info context message", "key", "value")

	assert.Len(t, logger.Entries, 1)
	entry := logger.Entries[0]
	assert.Equal(t, slog.LevelInfo, entry.Level)
	assert.Equal(t, "info context message", entry.Message)
	assert.Equal(t, []any{"key", "value"}, entry.Args)
	assert.Equal(t, ctx, entry.Context)
}

func TestMockLogger_WarnContext(t *testing.T) {
	logger := NewMockLogger()
	ctx := context.WithValue(context.Background(), "test_key", "test_value")

	logger.WarnContext(ctx, "warn context message", "key", "value")

	assert.Len(t, logger.Entries, 1)
	entry := logger.Entries[0]
	assert.Equal(t, slog.LevelWarn, entry.Level)
	assert.Equal(t, "warn context message", entry.Message)
	assert.Equal(t, []any{"key", "value"}, entry.Args)
	assert.Equal(t, ctx, entry.Context)
}

func TestMockLogger_ErrorContext(t *testing.T) {
	logger := NewMockLogger()
	ctx := context.WithValue(context.Background(), "test_key", "test_value")

	logger.ErrorContext(ctx, "error context message", "key", "value")

	assert.Len(t, logger.Entries, 1)
	entry := logger.Entries[0]
	assert.Equal(t, slog.LevelError, entry.Level)
	assert.Equal(t, "error context message", entry.Message)
	assert.Equal(t, []any{"key", "value"}, entry.Args)
	assert.Equal(t, ctx, entry.Context)
}

func TestMockLogger_Log(t *testing.T) {
	logger := NewMockLogger()
	ctx := context.WithValue(context.Background(), "test_key", "test_value")

	logger.Log(ctx, slog.LevelInfo, "log message", "key", "value")

	assert.Len(t, logger.Entries, 1)
	entry := logger.Entries[0]
	assert.Equal(t, slog.LevelInfo, entry.Level)
	assert.Equal(t, "log message", entry.Message)
	assert.Equal(t, []any{"key", "value"}, entry.Args)
	assert.Equal(t, ctx, entry.Context)
}

func TestMockLogger_With(t *testing.T) {
	logger := NewMockLogger()

	// With should return the same logger
	newLogger := logger.With("key", "value")
	assert.Equal(t, logger, newLogger)
}

func TestMockLogger_WithContext(t *testing.T) {
	logger := NewMockLogger()
	ctx := context.Background()

	// WithContext should return the same logger
	newLogger := logger.WithContext(ctx)
	assert.Equal(t, logger, newLogger)
}

func TestMockLogger_SetLevel(t *testing.T) {
	logger := NewMockLogger()

	// SetLevel should not panic
	assert.NotPanics(t, func() {
		logger.SetLevel(slog.LevelDebug)
	})
}

func TestMockLogger_Sync(t *testing.T) {
	logger := NewMockLogger()

	// Sync should return no error
	err := logger.Sync()
	assert.NoError(t, err)
}

func TestMockLogger_HasMessage_True(t *testing.T) {
	logger := NewMockLogger()

	logger.Info("test message")

	assert.True(t, logger.HasMessage("test message"))
}

func TestMockLogger_HasMessage_False(t *testing.T) {
	logger := NewMockLogger()

	logger.Info("test message")

	assert.False(t, logger.HasMessage("different message"))
}

func TestMockLogger_HasMessage_Empty(t *testing.T) {
	logger := NewMockLogger()

	assert.False(t, logger.HasMessage("any message"))
}

func TestMockLogger_HasMessage_MultipleEntries(t *testing.T) {
	logger := NewMockLogger()

	logger.Info("first message")
	logger.Warn("second message")
	logger.Error("third message")

	assert.True(t, logger.HasMessage("first message"))
	assert.True(t, logger.HasMessage("second message"))
	assert.True(t, logger.HasMessage("third message"))
	assert.False(t, logger.HasMessage("fourth message"))
}

func TestMockLogger_Slog(t *testing.T) {
	logger := NewMockLogger()

	slogLogger := logger.Slog()
	assert.NotNil(t, slogLogger)
	assert.IsType(t, &slog.Logger{}, slogLogger)
}

// TestMockLogger_Slog_DoesNotPanic pins KITLOG-GO-017: Slog() used to return
// slog.New(slog.NewTextHandler(nil, nil)), a handler with a nil io.Writer that
// panics on its first write. A logger installed via SetGlobalAndSlogDefault
// must be safe for any code that logs through the standard library.
func TestMockLogger_Slog_DoesNotPanic(t *testing.T) {
	logger := NewMockLogger()

	assert.NotPanics(t, func() {
		logger.Slog().Info("via slog", "key", "value")
	})
}

// TestMockLogger_Slog_SharesEntriesWithDirectCalls asserts that Slog() and the
// Logger methods observe the same log stream, so a test cannot get a
// different answer depending on which face of MockLogger it exercises.
func TestMockLogger_Slog_SharesEntriesWithDirectCalls(t *testing.T) {
	logger := NewMockLogger()

	logger.Info("direct call")
	logger.Slog().Info("via slog", "key", "value")

	require.Len(t, logger.Entries, 2)
	assert.Equal(t, "direct call", logger.Entries[0].Message)

	entry := logger.Entries[1]
	assert.Equal(t, slog.LevelInfo, entry.Level)
	assert.Equal(t, "via slog", entry.Message)
	require.Len(t, entry.Args, 1)
	assert.Equal(t, slog.String("key", "value"), entry.Args[0])
}

// TestMockLogger_LifecycleIsHonest asserts that MockLogger implements
// ManagedLogger honestly: it records that Flush and Shutdown were called,
// lets a test inject the error each should return, and rejects records
// logged after Shutdown instead of quietly accepting them.
func TestMockLogger_LifecycleIsHonest(t *testing.T) {
	mock := NewMockLogger()

	require.NoError(t, mock.Flush(context.Background()))
	require.NoError(t, mock.Sync())
	assert.Equal(t, 2, mock.FlushCalls(), "Sync must count as a Flush")

	mock.FlushErr = errors.New("flush failed")
	assert.ErrorIs(t, mock.Flush(context.Background()), mock.FlushErr)

	mock.Info("before shutdown")
	require.Len(t, mock.Entries, 1)

	require.NoError(t, mock.Shutdown(context.Background()))
	assert.Equal(t, 1, mock.ShutdownCalls())

	mock.Info("after shutdown")
	assert.Len(t, mock.Entries, 1, "a shut-down mock must not record new entries")
	assert.Equal(t, []error{kitlog.ErrLoggerShutdown}, mock.RejectedErrors())
	assert.ErrorIs(t, mock.Flush(context.Background()), kitlog.ErrLoggerShutdown)
}

func TestMockLogger_ConcurrentAccess(t *testing.T) {
	logger := NewMockLogger()

	// Test concurrent access
	done := make(chan bool, 10)

	for i := 0; i < 10; i++ {
		go func(id int) {
			logger.Info("concurrent message", "id", id)
			done <- true
		}(i)
	}

	// Wait for all goroutines to complete
	for i := 0; i < 10; i++ {
		<-done
	}

	// Should have 10 entries
	assert.Len(t, logger.Entries, 10)

	// All should have the same message
	for _, entry := range logger.Entries {
		assert.Equal(t, "concurrent message", entry.Message)
	}
}

func TestMockLogger_LogEntry_Structure(t *testing.T) {
	logger := NewMockLogger()
	ctx := context.WithValue(context.Background(), "user_id", "12345")

	logger.InfoContext(ctx, "structured message", "level", "info", "service", "test")

	assert.Len(t, logger.Entries, 1)
	entry := logger.Entries[0]

	// Verify LogEntry structure
	assert.Equal(t, slog.LevelInfo, entry.Level)
	assert.Equal(t, "structured message", entry.Message)
	assert.Equal(t, []any{"level", "info", "service", "test"}, entry.Args)
	assert.Equal(t, ctx, entry.Context)
}

func TestMockLogger_Integration(t *testing.T) {
	logger := NewMockLogger()

	// Test all logging methods
	logger.Debug("debug message", "debug_key", "debug_value")
	logger.Info("info message", "info_key", "info_value")
	logger.Warn("warn message", "warn_key", "warn_value")
	logger.Error("error message", "error_key", "error_value")

	// Test context logging
	ctx := context.Background()
	logger.DebugContext(ctx, "debug context", "ctx_key", "ctx_value")
	logger.InfoContext(ctx, "info context", "ctx_key", "ctx_value")
	logger.WarnContext(ctx, "warn context", "ctx_key", "ctx_value")
	logger.ErrorContext(ctx, "error context", "ctx_key", "ctx_value")

	// Test Log method
	logger.Log(ctx, slog.LevelInfo, "log method", "log_key", "log_value")

	// Verify all entries were recorded
	assert.Len(t, logger.Entries, 9)

	// Verify message presence
	assert.True(t, logger.HasMessage("debug message"))
	assert.True(t, logger.HasMessage("info message"))
	assert.True(t, logger.HasMessage("warn message"))
	assert.True(t, logger.HasMessage("error message"))
	assert.True(t, logger.HasMessage("debug context"))
	assert.True(t, logger.HasMessage("info context"))
	assert.True(t, logger.HasMessage("warn context"))
	assert.True(t, logger.HasMessage("error context"))
	assert.True(t, logger.HasMessage("log method"))

	// Test other methods
	loggerWith := logger.With("with_key", "with_value")
	assert.Equal(t, logger, loggerWith)

	loggerWithCtx := logger.WithContext(ctx)
	assert.Equal(t, logger, loggerWithCtx)

	logger.SetLevel(slog.LevelDebug)

	err := logger.Sync()
	assert.NoError(t, err)

	slogLogger := logger.Slog()
	assert.NotNil(t, slogLogger)
}

func TestMockLogger_EdgeCases(t *testing.T) {
	logger := NewMockLogger()

	// Test with no arguments
	logger.Info("no args message")
	assert.Len(t, logger.Entries, 1)
	assert.Empty(t, logger.Entries[0].Args)

	// Test with nil context
	logger.InfoContext(nil, "nil context message")
	assert.Len(t, logger.Entries, 2)
	assert.Nil(t, logger.Entries[1].Context)

	// Test with empty message
	logger.Info("")
	assert.Len(t, logger.Entries, 3)
	assert.Equal(t, "", logger.Entries[2].Message)

	// Test with complex arguments
	logger.Info("complex args", "map", map[string]string{"key": "value"}, "slice", []int{1, 2, 3})
	assert.Len(t, logger.Entries, 4)
	assert.Len(t, logger.Entries[3].Args, 4)
}
