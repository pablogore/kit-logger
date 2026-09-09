package handler_test

import (
	"context"

	"log/slog"
	"testing"

	"github.com/pablogore/kit-logger/pkg/logger/handler"
	"github.com/pablogore/kit-logger/pkg/logger/kitlogtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// flattenAttrs collects every leaf attribute of a record into one map,
// descending into nested groups. WithGroup now correctly nests attrs added
// after it was called -- including the ones this package's decorators add
// inside Handle -- so a flat lookup by key, regardless of depth, is what
// these tests actually mean to assert.
func flattenAttrs(r slog.Record) map[string]any {
	out := map[string]any{}
	var walk func([]slog.Attr)
	walk = func(attrs []slog.Attr) {
		for _, a := range attrs {
			if a.Value.Kind() == slog.KindGroup {
				walk(a.Value.Group())
				continue
			}
			out[a.Key] = a.Value.Any()
		}
	}
	var top []slog.Attr
	r.Attrs(func(a slog.Attr) bool {
		top = append(top, a)
		return true
	})
	walk(top)
	return out
}

func TestGlobalFieldsHandler_AppendsGlobalFields(t *testing.T) {
	var captured slog.Record

	base := kitlogtest.NewTestHandler(func(_ context.Context, r slog.Record) {
		captured = r
	})

	globalHandler := handler.NewGlobalFieldsHandler(base, map[string]string{
		"env": "staging",
	}, false)

	logger := slog.New(globalHandler)
	logger.Info("request received", "method", "GET")

	fields := flattenAttrs(captured)

	require.Equal(t, "staging", fields["env"])
	require.Equal(t, "GET", fields["method"])
}

func TestGlobalFieldsHandler_WithAttrs_AppendsExtraFields(t *testing.T) {
	var captured slog.Record

	base := kitlogtest.NewTestHandler(func(_ context.Context, r slog.Record) {
		captured = r
	})

	globalHandler := handler.NewGlobalFieldsHandler(base, map[string]string{
		"env": "staging",
	}, false)

	handlerWithAttrs := globalHandler.WithAttrs([]slog.Attr{
		slog.String("component", "api"),
	})

	logger := slog.New(handlerWithAttrs)
	logger.Info("request received", "method", "GET")

	fields := flattenAttrs(captured)

	require.Equal(t, "staging", fields["env"])
	require.Equal(t, "api", fields["component"])
	require.Equal(t, "GET", fields["method"])
}

func TestGlobalFieldsHandler_OverrideFalse_PreservesExistingFields(t *testing.T) {
	var captured slog.Record

	base := kitlogtest.NewTestHandler(func(_ context.Context, r slog.Record) {
		captured = r
	})

	globalHandler := handler.NewGlobalFieldsHandler(base, map[string]string{
		"env": "staging",
	}, false)

	logger := slog.New(globalHandler)
	logger.Info("request received", "env", "production")

	fields := flattenAttrs(captured)

	require.Equal(t, "production", fields["env"], "should not override existing field when override=false")
}

func TestGlobalFieldsHandler_OverrideTrue_ReplacesFields(t *testing.T) {
	var captured slog.Record

	base := kitlogtest.NewTestHandler(func(_ context.Context, r slog.Record) {
		captured = r
	})

	globalHandler := handler.NewGlobalFieldsHandler(base, map[string]string{
		"env": "staging",
	}, true)

	logger := slog.New(globalHandler)
	logger.Info("request received", "env", "production")

	fields := flattenAttrs(captured)

	require.Equal(t, "staging", fields["env"], "should override existing field when override=true")
}

func TestGlobalFieldsHandler_WithGroup(t *testing.T) {
	var captured slog.Record
	base := kitlogtest.NewTestHandler(func(_ context.Context, r slog.Record) {
		captured = r
	})

	globalHandler := handler.NewGlobalFieldsHandler(base, map[string]string{
		"service": "auth",
		"version": "1.0",
	}, false)

	// Create a new handler with a group
	newHandler := globalHandler.WithGroup("user")
	assert.NotNil(t, newHandler)
	assert.IsType(t, &handler.GlobalFieldsHandler{}, newHandler)

	// Test that the new handler works correctly
	logger := slog.New(newHandler)
	logger.Info("user login", "user_id", "12345")

	// Verify that the log was captured with global fields
	fields := flattenAttrs(captured)
	assert.Equal(t, "auth", fields["service"], "global service field should be present")
	assert.Equal(t, "1.0", fields["version"], "global version field should be present")
	assert.Equal(t, "12345", fields["user_id"], "user_id field should be present")
	assert.Equal(t, "user login", captured.Message, "message should be captured correctly")
}

func TestGlobalFieldsHandler_WithGroup_EmptyGroup(t *testing.T) {
	var captured slog.Record
	base := kitlogtest.NewTestHandler(func(_ context.Context, r slog.Record) {
		captured = r
	})

	globalHandler := handler.NewGlobalFieldsHandler(base, map[string]string{
		"env": "production",
	}, false)

	// Create a new handler with empty group name
	newHandler := globalHandler.WithGroup("")
	assert.NotNil(t, newHandler)
	assert.IsType(t, &handler.GlobalFieldsHandler{}, newHandler)

	// Test that the new handler works correctly
	logger := slog.New(newHandler)
	logger.Info("test message", "key", "value")

	// Verify that the log was captured with global fields
	fields := flattenAttrs(captured)
	assert.Equal(t, "production", fields["env"], "global env field should be present")
	assert.Equal(t, "value", fields["key"], "key field should be present")
	assert.Equal(t, "test message", captured.Message, "message should be captured correctly")
}

func TestGlobalFieldsHandler_WithGroup_PreservesOverride(t *testing.T) {
	var captured slog.Record
	base := kitlogtest.NewTestHandler(func(_ context.Context, r slog.Record) {
		captured = r
	})

	// Create handler with override=true
	globalHandler := handler.NewGlobalFieldsHandler(base, map[string]string{
		"env": "staging",
	}, true)

	// Create a new handler with a group
	newHandler := globalHandler.WithGroup("api")
	assert.NotNil(t, newHandler)
	assert.IsType(t, &handler.GlobalFieldsHandler{}, newHandler)

	// Test that override behavior is preserved
	logger := slog.New(newHandler)
	logger.Info("api call", "env", "production")

	// Verify that the global field overrides the local one
	fields := flattenAttrs(captured)
	assert.Equal(t, "staging", fields["env"], "global env field should override local env field when override=true")
	assert.Equal(t, "api call", captured.Message, "message should be captured correctly")
}

func TestGlobalFieldsHandler_WithGroup_WithAttrs_Integration(t *testing.T) {
	var captured slog.Record
	base := kitlogtest.NewTestHandler(func(_ context.Context, r slog.Record) {
		captured = r
	})

	globalHandler := handler.NewGlobalFieldsHandler(base, map[string]string{
		"service": "payment",
		"version": "2.0",
	}, false)

	// Create a handler with attributes
	handlerWithAttrs := globalHandler.WithAttrs([]slog.Attr{
		slog.String("component", "processor"),
	})

	// Create a handler with group
	handlerWithGroup := handlerWithAttrs.WithGroup("transaction")

	// Test that the handler works correctly
	logger := slog.New(handlerWithGroup)
	logger.Info("payment processed", "amount", "100.00", "currency", "USD")

	// Verify that all fields are present
	fields := flattenAttrs(captured)
	assert.Equal(t, "payment", fields["service"], "global service field should be present")
	assert.Equal(t, "2.0", fields["version"], "global version field should be present")
	assert.Equal(t, "processor", fields["component"], "component field should be present")
	assert.Equal(t, "100.00", fields["amount"], "amount field should be present")
	assert.Equal(t, "USD", fields["currency"], "currency field should be present")
	assert.Equal(t, "payment processed", captured.Message, "message should be captured correctly")
}

func TestGlobalFieldsHandler_WithGroup_MultipleGroups(t *testing.T) {
	var captured slog.Record
	base := kitlogtest.NewTestHandler(func(_ context.Context, r slog.Record) {
		captured = r
	})

	globalHandler := handler.NewGlobalFieldsHandler(base, map[string]string{
		"env": "development",
	}, false)

	// Create multiple groups
	handlerWithGroup1 := globalHandler.WithGroup("user")
	handlerWithGroup2 := handlerWithGroup1.WithGroup("session")
	handlerWithGroup3 := handlerWithGroup2.WithGroup("request")

	// Test that the handler works correctly
	logger := slog.New(handlerWithGroup3)
	logger.Info("request processed", "method", "POST")

	// Verify that global fields are still present
	fields := flattenAttrs(captured)
	assert.Equal(t, "development", fields["env"], "global env field should be present")
	assert.Equal(t, "POST", fields["method"], "method field should be present")
	assert.Equal(t, "request processed", captured.Message, "message should be captured correctly")
}

func TestGlobalFieldsHandler_WithGroup_ContextLogging(t *testing.T) {
	var captured slog.Record
	base := kitlogtest.NewTestHandler(func(_ context.Context, r slog.Record) {
		captured = r
	})

	globalHandler := handler.NewGlobalFieldsHandler(base, map[string]string{
		"service": "notification",
	}, false)

	// Create a handler with group
	handlerWithGroup := globalHandler.WithGroup("email")

	// Test context logging
	logger := slog.New(handlerWithGroup)
	ctx := context.Background()
	logger.InfoContext(ctx, "email sent", "recipient", "user@example.com")

	// Verify that the log was captured with global fields
	fields := flattenAttrs(captured)
	assert.Equal(t, "notification", fields["service"], "global service field should be present")
	assert.Equal(t, "user@example.com", fields["recipient"], "recipient field should be present")
	assert.Equal(t, "email sent", captured.Message, "message should be captured correctly")
}

func TestGlobalFieldsHandler_WithGroup_DifferentLevels(t *testing.T) {
	var captured slog.Record
	base := kitlogtest.NewTestHandler(func(_ context.Context, r slog.Record) {
		captured = r
	})

	globalHandler := handler.NewGlobalFieldsHandler(base, map[string]string{
		"component": "database",
	}, false)

	// Create a handler with group
	handlerWithGroup := globalHandler.WithGroup("query")

	// Test different log levels
	logger := slog.New(handlerWithGroup)

	// Debug level
	logger.Debug("query executed", "duration", "10ms")
	fields := flattenAttrs(captured)
	assert.Equal(t, "database", fields["component"], "global component field should be present")
	assert.Equal(t, "10ms", fields["duration"], "duration field should be present")
	assert.Equal(t, "query executed", captured.Message, "debug message should be captured correctly")

	// Error level
	logger.Error("query failed", "error", "connection timeout")
	fields = flattenAttrs(captured)
	assert.Equal(t, "database", fields["component"], "global component field should be present")
	assert.Equal(t, "connection timeout", fields["error"], "error field should be present")
	assert.Equal(t, "query failed", captured.Message, "error message should be captured correctly")
}
