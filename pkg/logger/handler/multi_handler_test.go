package handler_test

import (
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/getsyntegrity/kit-logger/pkg/logger/handler"
	"github.com/getsyntegrity/kit-logger/pkg/logger/utils"
)

func TestMultiHandler_DelegatesToAllHandlers(t *testing.T) {
	var called1, called2 bool

	handler1 := handler.NewTestHandler(func(_ context.Context, r slog.Record) {
		called1 = true
		require.Equal(t, "multi test", r.Message)
	})

	handler2 := handler.NewTestHandler(func(_ context.Context, r slog.Record) {
		called2 = true
		require.Equal(t, "multi test", r.Message)
	})

	multi := handler.NewMultiHandler(handler1, handler2)

	logger := slog.New(multi)
	logger.Info("multi test")

	require.True(t, called1, "handler1 should be called")
	require.True(t, called2, "handler2 should be called")
}

func TestMultiHandler_WithAttrs_PreservesHandlers(t *testing.T) {
	handler1 := handler.NewTestHandler(func(_ context.Context, r slog.Record) {
		attrs := utils.ExtractAttrs(r)
		require.Equal(t, "value", attrs["key"])
	})

	handler2 := handler.NewTestHandler(func(_ context.Context, r slog.Record) {
		attrs := utils.ExtractAttrs(r)
		require.Equal(t, "value", attrs["key"])
	})

	multi := handler.NewMultiHandler(handler1, handler2)
	logger := slog.New(multi.WithAttrs([]slog.Attr{slog.String("key", "value")}))
	logger.Info("attr test")
}

func TestMultiHandler_Enabled_AllHandlersEnabled(t *testing.T) {
	// Create handlers that are all enabled
	handler1 := &multiTestLevelHandler{level: slog.LevelDebug}
	handler2 := &multiTestLevelHandler{level: slog.LevelInfo}
	handler3 := &multiTestLevelHandler{level: slog.LevelWarn}

	multi := handler.NewMultiHandler(handler1, handler2, handler3)

	// Test with Debug level - should be enabled (handler1 enables it)
	enabled := multi.Enabled(context.Background(), slog.LevelDebug)
	assert.True(t, enabled, "Should be enabled when at least one handler enables Debug")

	// Test with Info level - should be enabled (handler1 and handler2 enable it)
	enabled = multi.Enabled(context.Background(), slog.LevelInfo)
	assert.True(t, enabled, "Should be enabled when at least one handler enables Info")

	// Test with Warn level - should be enabled (all handlers enable it)
	enabled = multi.Enabled(context.Background(), slog.LevelWarn)
	assert.True(t, enabled, "Should be enabled when all handlers enable Warn")
}

func TestMultiHandler_Enabled_SomeHandlersEnabled(t *testing.T) {
	// Create handlers where some are enabled and some are not
	handler1 := &multiTestLevelHandler{level: slog.LevelInfo}  // Only enables Info+
	handler2 := &multiTestLevelHandler{level: slog.LevelWarn}  // Only enables Warn+
	handler3 := &multiTestLevelHandler{level: slog.LevelError} // Only enables Error+

	multi := handler.NewMultiHandler(handler1, handler2, handler3)

	// Test with Debug level - should NOT be enabled (no handler enables it)
	enabled := multi.Enabled(context.Background(), slog.LevelDebug)
	assert.False(t, enabled, "Should NOT be enabled when no handler enables Debug")

	// Test with Info level - should be enabled (handler1 enables it)
	enabled = multi.Enabled(context.Background(), slog.LevelInfo)
	assert.True(t, enabled, "Should be enabled when handler1 enables Info")

	// Test with Warn level - should be enabled (handler1 and handler2 enable it)
	enabled = multi.Enabled(context.Background(), slog.LevelWarn)
	assert.True(t, enabled, "Should be enabled when handler1 and handler2 enable Warn")

	// Test with Error level - should be enabled (all handlers enable it)
	enabled = multi.Enabled(context.Background(), slog.LevelError)
	assert.True(t, enabled, "Should be enabled when all handlers enable Error")
}

func TestMultiHandler_Enabled_NoHandlersEnabled(t *testing.T) {
	// Create handlers where none enable the test level
	handler1 := &multiTestLevelHandler{level: slog.LevelWarn}  // Only enables Warn+
	handler2 := &multiTestLevelHandler{level: slog.LevelError} // Only enables Error+

	multi := handler.NewMultiHandler(handler1, handler2)

	// Test with Debug level - should NOT be enabled
	enabled := multi.Enabled(context.Background(), slog.LevelDebug)
	assert.False(t, enabled, "Should NOT be enabled when no handler enables Debug")

	// Test with Info level - should NOT be enabled
	enabled = multi.Enabled(context.Background(), slog.LevelInfo)
	assert.False(t, enabled, "Should NOT be enabled when no handler enables Info")
}

func TestMultiHandler_Handle_WithErrors(t *testing.T) {
	var called1, called3 bool

	// Create handlers, one of which returns an error
	handler1 := handler.NewTestHandler(func(_ context.Context, r slog.Record) {
		called1 = true
	})

	handler2 := &errorHandler{shouldError: true} // This handler will return an error

	handler3 := handler.NewTestHandler(func(_ context.Context, r slog.Record) {
		called3 = true
	})

	multi := handler.NewMultiHandler(handler1, handler2, handler3)

	logger := slog.New(multi)
	logger.Info("test with errors")

	// Verify that all handlers were called
	assert.True(t, called1, "handler1 should be called")
	assert.True(t, called3, "handler3 should be called")
}

func TestMultiHandler_Handle_NoErrors(t *testing.T) {
	var called1, called2 bool

	handler1 := handler.NewTestHandler(func(_ context.Context, r slog.Record) {
		called1 = true
	})

	handler2 := handler.NewTestHandler(func(_ context.Context, r slog.Record) {
		called2 = true
	})

	multi := handler.NewMultiHandler(handler1, handler2)

	logger := slog.New(multi)
	logger.Info("test without errors")

	// Verify that all handlers were called
	assert.True(t, called1, "handler1 should be called")
	assert.True(t, called2, "handler2 should be called")
}

func TestMultiHandler_Handle_EmptyHandlers(t *testing.T) {
	// Create MultiHandler with no handlers
	multi := handler.NewMultiHandler()

	logger := slog.New(multi)
	logger.Info("test with no handlers")

	// Should not panic
}

func TestMultiHandler_WithGroup(t *testing.T) {
	var captured1, captured2 slog.Record

	handler1 := handler.NewTestHandler(func(_ context.Context, r slog.Record) {
		captured1 = r
	})

	handler2 := handler.NewTestHandler(func(_ context.Context, r slog.Record) {
		captured2 = r
	})

	multi := handler.NewMultiHandler(handler1, handler2)

	// Create a new handler with a group
	newHandler := multi.WithGroup("user")
	assert.NotNil(t, newHandler)
	assert.IsType(t, &handler.MultiHandler{}, newHandler)

	// Test that the new handler works correctly
	logger := slog.New(newHandler)
	logger.Info("test message", "id", "12345")

	// Verify that both handlers received the log
	assert.Equal(t, "test message", captured1.Message)
	assert.Equal(t, "test message", captured2.Message)
}

func TestMultiHandler_WithGroup_EmptyGroup(t *testing.T) {
	var captured1, captured2 slog.Record

	handler1 := handler.NewTestHandler(func(_ context.Context, r slog.Record) {
		captured1 = r
	})

	handler2 := handler.NewTestHandler(func(_ context.Context, r slog.Record) {
		captured2 = r
	})

	multi := handler.NewMultiHandler(handler1, handler2)

	// Create a new handler with empty group name
	newHandler := multi.WithGroup("")
	assert.NotNil(t, newHandler)
	assert.IsType(t, &handler.MultiHandler{}, newHandler)

	// Test that the new handler works correctly
	logger := slog.New(newHandler)
	logger.Info("test message", "key", "value")

	// Verify that both handlers received the log
	assert.Equal(t, "test message", captured1.Message)
	assert.Equal(t, "test message", captured2.Message)
}

func TestMultiHandler_WithGroup_WithAttrs_Integration(t *testing.T) {
	var captured1, captured2 slog.Record

	handler1 := handler.NewTestHandler(func(_ context.Context, r slog.Record) {
		captured1 = r
	})

	handler2 := handler.NewTestHandler(func(_ context.Context, r slog.Record) {
		captured2 = r
	})

	multi := handler.NewMultiHandler(handler1, handler2)

	// Create a handler with attributes
	handlerWithAttrs := multi.WithAttrs([]slog.Attr{
		slog.String("service", "auth"),
		slog.Int("version", 1),
	})

	// Create a handler with group
	handlerWithGroup := handlerWithAttrs.WithGroup("user")

	// Test that the handler works correctly
	logger := slog.New(handlerWithGroup)
	logger.Info("user login", "user_id", "12345")

	// Verify that both handlers received the log with all attributes
	assert.Equal(t, "user login", captured1.Message)
	assert.Equal(t, "user login", captured2.Message)

	// Verify that attributes are present in both handlers
	attrs1 := utils.ExtractAttrs(captured1)
	attrs2 := utils.ExtractAttrs(captured2)

	assert.Equal(t, "auth", attrs1["service"])
	assert.Equal(t, "auth", attrs2["service"])
	assert.Equal(t, int64(1), attrs1["version"])
	assert.Equal(t, int64(1), attrs2["version"])
	assert.Equal(t, "12345", attrs1["user_id"])
	assert.Equal(t, "12345", attrs2["user_id"])
}

func TestMultiHandler_WithGroup_MultipleHandlers(t *testing.T) {
	var captured1, captured2, captured3 slog.Record

	handler1 := handler.NewTestHandler(func(_ context.Context, r slog.Record) {
		captured1 = r
	})

	handler2 := handler.NewTestHandler(func(_ context.Context, r slog.Record) {
		captured2 = r
	})

	handler3 := handler.NewTestHandler(func(_ context.Context, r slog.Record) {
		captured3 = r
	})

	multi := handler.NewMultiHandler(handler1, handler2, handler3)

	// Create a new handler with a group
	newHandler := multi.WithGroup("api")

	// Test that the new handler works correctly
	logger := slog.New(newHandler)
	logger.Info("api call", "method", "GET")

	// Verify that all handlers received the log
	assert.Equal(t, "api call", captured1.Message)
	assert.Equal(t, "api call", captured2.Message)
	assert.Equal(t, "api call", captured3.Message)

	// Verify that method attribute is present in all handlers
	attrs1 := utils.ExtractAttrs(captured1)
	attrs2 := utils.ExtractAttrs(captured2)
	attrs3 := utils.ExtractAttrs(captured3)

	assert.Equal(t, "GET", attrs1["method"])
	assert.Equal(t, "GET", attrs2["method"])
	assert.Equal(t, "GET", attrs3["method"])
}

func TestMultiHandler_WithGroup_ContextLogging(t *testing.T) {
	var captured1, captured2 slog.Record

	handler1 := handler.NewTestHandler(func(_ context.Context, r slog.Record) {
		captured1 = r
	})

	handler2 := handler.NewTestHandler(func(_ context.Context, r slog.Record) {
		captured2 = r
	})

	multi := handler.NewMultiHandler(handler1, handler2)

	// Create a handler with group
	handlerWithGroup := multi.WithGroup("session")

	// Test context logging
	logger := slog.New(handlerWithGroup)
	ctx := context.Background()
	logger.InfoContext(ctx, "session created", "session_id", "abc123")

	// Verify that both handlers received the log
	assert.Equal(t, "session created", captured1.Message)
	assert.Equal(t, "session created", captured2.Message)

	// Verify that session_id attribute is present in both handlers
	attrs1 := utils.ExtractAttrs(captured1)
	attrs2 := utils.ExtractAttrs(captured2)

	assert.Equal(t, "abc123", attrs1["session_id"])
	assert.Equal(t, "abc123", attrs2["session_id"])
}

// multiTestLevelHandler is a simple handler that filters by level
type multiTestLevelHandler struct {
	level slog.Level
}

func (h *multiTestLevelHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return level >= h.level
}

func (h *multiTestLevelHandler) Handle(ctx context.Context, r slog.Record) error {
	return nil
}

func (h *multiTestLevelHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return h
}

func (h *multiTestLevelHandler) WithGroup(name string) slog.Handler {
	return h
}

// errorHandler is a handler that returns an error
type errorHandler struct {
	shouldError bool
}

func (h *errorHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return true
}

func (h *errorHandler) Handle(ctx context.Context, r slog.Record) error {
	if h.shouldError {
		return assert.AnError
	}
	return nil
}

func (h *errorHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return h
}

func (h *errorHandler) WithGroup(name string) slog.Handler {
	return h
}
