package handler_test

import (
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/getsyntegrity/kit-logger/pkg/logger/handler"
)

type contextKey string

const hookKey contextKey = "hook"

func TestHookHandler_ExecutesHook(t *testing.T) {
	var called bool
	var capturedCtx context.Context

	hook := func(ctx context.Context, _ slog.Record) (context.Context, bool) {
		called = true
		capturedCtx = context.WithValue(ctx, hookKey, true)
		return capturedCtx, true
	}

	h := handler.NewTestHandler(func(ctx context.Context, _ slog.Record) {
		require.Equal(t, true, ctx.Value(hookKey))
	})
	h = handler.NewHookHandler(h, hook)

	logger := slog.New(h)
	logger.Info("hook test")

	require.True(t, called)
}

func TestHookHandler_SkipsLog_WhenHookReturnsFalse(t *testing.T) {
	hook := func(ctx context.Context, _ slog.Record) (context.Context, bool) {
		return ctx, false
	}

	handlerCalled := false

	h := handler.NewTestHandler(func(_ context.Context, _ slog.Record) {
		handlerCalled = true
	})
	h = handler.NewHookHandler(h, hook)

	logger := slog.New(h)
	logger.Info("should be skipped")

	require.False(t, handlerCalled)
}

func TestHookHandler_WithAttrs(t *testing.T) {
	var captured slog.Record
	base := handler.NewTestHandler(func(_ context.Context, r slog.Record) {
		captured = r
	})

	hook := func(ctx context.Context, r slog.Record) (context.Context, bool) {
		// Add a value to context to verify hook is called
		return context.WithValue(ctx, hookKey, "hook_executed"), true
	}

	h := handler.NewHookHandler(base, hook)

	// Create a new handler with additional attributes
	newHandler := h.WithAttrs([]slog.Attr{
		slog.String("service", "auth"),
		slog.Int("version", 1),
	})
	assert.NotNil(t, newHandler)
	assert.IsType(t, &handler.HookHandler{}, newHandler)

	// Test that the new handler works correctly
	logger := slog.New(newHandler)
	logger.Info("test message", "user", "alice")

	// Verify that the log was captured with additional attributes
	assert.Equal(t, "test message", captured.Message)

	var hasService, hasVersion, hasUser bool
	captured.Attrs(func(a slog.Attr) bool {
		switch a.Key {
		case "service":
			hasService = true
		case "version":
			hasVersion = true
		case "user":
			hasUser = true
		}
		return true
	})

	assert.True(t, hasService, "service attribute should be present")
	assert.True(t, hasVersion, "version attribute should be present")
	assert.True(t, hasUser, "user attribute should be present")
}

func TestHookHandler_WithGroup(t *testing.T) {
	var captured slog.Record
	base := handler.NewTestHandler(func(_ context.Context, r slog.Record) {
		captured = r
	})

	hook := func(ctx context.Context, r slog.Record) (context.Context, bool) {
		// Add a value to context to verify hook is called
		return context.WithValue(ctx, hookKey, "hook_executed"), true
	}

	h := handler.NewHookHandler(base, hook)

	// Create a new handler with a group
	newHandler := h.WithGroup("user")
	assert.NotNil(t, newHandler)
	assert.IsType(t, &handler.HookHandler{}, newHandler)

	// Test that the new handler works correctly
	logger := slog.New(newHandler)
	logger.Info("test message", "id", "12345")

	// Verify that the log was captured
	assert.Equal(t, "test message", captured.Message)

	var hasID bool
	captured.Attrs(func(a slog.Attr) bool {
		if a.Key == "id" {
			hasID = true
		}
		return true
	})

	assert.True(t, hasID, "id attribute should be present")
}

func TestHookHandler_WithAttrs_EmptyAttrs(t *testing.T) {
	var captured slog.Record
	base := handler.NewTestHandler(func(_ context.Context, r slog.Record) {
		captured = r
	})

	hook := func(ctx context.Context, r slog.Record) (context.Context, bool) {
		return ctx, true
	}

	h := handler.NewHookHandler(base, hook)

	// Create a new handler with empty attributes
	newHandler := h.WithAttrs([]slog.Attr{})
	assert.NotNil(t, newHandler)
	assert.IsType(t, &handler.HookHandler{}, newHandler)

	// Test that the new handler works correctly
	logger := slog.New(newHandler)
	logger.Info("test message", "key", "value")

	// Verify that the log was captured
	assert.Equal(t, "test message", captured.Message)

	var hasKey bool
	captured.Attrs(func(a slog.Attr) bool {
		if a.Key == "key" {
			hasKey = true
		}
		return true
	})

	assert.True(t, hasKey, "key attribute should be present")
}

func TestHookHandler_WithGroup_EmptyGroup(t *testing.T) {
	var captured slog.Record
	base := handler.NewTestHandler(func(_ context.Context, r slog.Record) {
		captured = r
	})

	hook := func(ctx context.Context, r slog.Record) (context.Context, bool) {
		return ctx, true
	}

	h := handler.NewHookHandler(base, hook)

	// Create a new handler with empty group name
	newHandler := h.WithGroup("")
	assert.NotNil(t, newHandler)
	assert.IsType(t, &handler.HookHandler{}, newHandler)

	// Test that the new handler works correctly
	logger := slog.New(newHandler)
	logger.Info("test message", "key", "value")

	// Verify that the log was captured
	assert.Equal(t, "test message", captured.Message)

	var hasKey bool
	captured.Attrs(func(a slog.Attr) bool {
		if a.Key == "key" {
			hasKey = true
		}
		return true
	})

	assert.True(t, hasKey, "key attribute should be present")
}

func TestHookHandler_WithAttrsAndGroup_Integration(t *testing.T) {
	var captured slog.Record
	base := handler.NewTestHandler(func(_ context.Context, r slog.Record) {
		captured = r
	})

	hook := func(ctx context.Context, r slog.Record) (context.Context, bool) {
		// Add a value to context to verify hook is called
		return context.WithValue(ctx, hookKey, "hook_executed"), true
	}

	h := handler.NewHookHandler(base, hook)

	// Create a handler with attributes
	handlerWithAttrs := h.WithAttrs([]slog.Attr{
		slog.String("service", "payment"),
		slog.Int("version", 2),
	})

	// Create a handler with group
	handlerWithGroup := handlerWithAttrs.WithGroup("transaction")

	// Test that the handler works correctly
	logger := slog.New(handlerWithGroup)
	logger.Info("payment processed", "amount", "100.00", "currency", "USD")

	// Verify that the log was captured with all attributes
	assert.Equal(t, "payment processed", captured.Message)

	var hasService, hasVersion, hasAmount, hasCurrency bool
	captured.Attrs(func(a slog.Attr) bool {
		switch a.Key {
		case "service":
			hasService = true
		case "version":
			hasVersion = true
		case "amount":
			hasAmount = true
		case "currency":
			hasCurrency = true
		}
		return true
	})

	assert.True(t, hasService, "service attribute should be present")
	assert.True(t, hasVersion, "version attribute should be present")
	assert.True(t, hasAmount, "amount attribute should be present")
	assert.True(t, hasCurrency, "currency attribute should be present")
}

func TestHookHandler_WithAttrs_PreservesHook(t *testing.T) {
	var hookCalled bool
	var capturedCtx context.Context
	base := handler.NewTestHandler(func(ctx context.Context, r slog.Record) {
		// Verify that hook was called and context was modified
		require.Equal(t, "hook_executed", ctx.Value(hookKey))
	})

	hook := func(ctx context.Context, r slog.Record) (context.Context, bool) {
		hookCalled = true
		capturedCtx = context.WithValue(ctx, hookKey, "hook_executed")
		return capturedCtx, true
	}

	h := handler.NewHookHandler(base, hook)

	// Create a new handler with attributes
	newHandler := h.WithAttrs([]slog.Attr{
		slog.String("service", "auth"),
	})

	// Test that the hook is still executed
	logger := slog.New(newHandler)
	logger.Info("test message")

	// Verify that the hook was called
	assert.True(t, hookCalled, "Hook should be executed")
}

func TestHookHandler_WithGroup_PreservesHook(t *testing.T) {
	var hookCalled bool
	var capturedCtx context.Context
	base := handler.NewTestHandler(func(ctx context.Context, r slog.Record) {
		// Verify that hook was called and context was modified
		require.Equal(t, "hook_executed", ctx.Value(hookKey))
	})

	hook := func(ctx context.Context, r slog.Record) (context.Context, bool) {
		hookCalled = true
		capturedCtx = context.WithValue(ctx, hookKey, "hook_executed")
		return capturedCtx, true
	}

	h := handler.NewHookHandler(base, hook)

	// Create a new handler with group
	newHandler := h.WithGroup("user")

	// Test that the hook is still executed
	logger := slog.New(newHandler)
	logger.Info("test message")

	// Verify that the hook was called
	assert.True(t, hookCalled, "Hook should be executed")
}

func TestHookHandler_WithAttrs_HookReturnsFalse(t *testing.T) {
	var captured slog.Record
	base := handler.NewTestHandler(func(_ context.Context, r slog.Record) {
		captured = r
	})

	hook := func(ctx context.Context, r slog.Record) (context.Context, bool) {
		return ctx, false // Hook returns false, should skip log
	}

	h := handler.NewHookHandler(base, hook)

	// Create a new handler with attributes
	newHandler := h.WithAttrs([]slog.Attr{
		slog.String("service", "auth"),
	})

	// Test that the log is skipped when hook returns false
	logger := slog.New(newHandler)
	logger.Info("test message")

	// Verify that the log was NOT captured (hook returned false)
	assert.Empty(t, captured.Message, "Log should be skipped when hook returns false")
}

func TestHookHandler_WithGroup_HookReturnsFalse(t *testing.T) {
	var captured slog.Record
	base := handler.NewTestHandler(func(_ context.Context, r slog.Record) {
		captured = r
	})

	hook := func(ctx context.Context, r slog.Record) (context.Context, bool) {
		return ctx, false // Hook returns false, should skip log
	}

	h := handler.NewHookHandler(base, hook)

	// Create a new handler with group
	newHandler := h.WithGroup("user")

	// Test that the log is skipped when hook returns false
	logger := slog.New(newHandler)
	logger.Info("test message")

	// Verify that the log was NOT captured (hook returned false)
	assert.Empty(t, captured.Message, "Log should be skipped when hook returns false")
}

func TestHookHandler_WithAttrs_MultipleAttrs(t *testing.T) {
	var captured slog.Record
	base := handler.NewTestHandler(func(_ context.Context, r slog.Record) {
		captured = r
	})

	hook := func(ctx context.Context, r slog.Record) (context.Context, bool) {
		return ctx, true
	}

	h := handler.NewHookHandler(base, hook)

	// Create a new handler with multiple attributes
	newHandler := h.WithAttrs([]slog.Attr{
		slog.String("service", "auth"),
		slog.Int("version", 1),
		slog.Bool("debug", true),
		slog.Float64("score", 95.5),
	})

	// Test that the new handler works correctly
	logger := slog.New(newHandler)
	logger.Info("test message", "user", "alice")

	// Verify that all attributes are present
	var hasService, hasVersion, hasDebug, hasScore, hasUser bool
	captured.Attrs(func(a slog.Attr) bool {
		switch a.Key {
		case "service":
			hasService = true
		case "version":
			hasVersion = true
		case "debug":
			hasDebug = true
		case "score":
			hasScore = true
		case "user":
			hasUser = true
		}
		return true
	})

	assert.True(t, hasService, "service attribute should be present")
	assert.True(t, hasVersion, "version attribute should be present")
	assert.True(t, hasDebug, "debug attribute should be present")
	assert.True(t, hasScore, "score attribute should be present")
	assert.True(t, hasUser, "user attribute should be present")
}
