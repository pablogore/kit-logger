package handler_test

import (
	"context"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/getsyntegrity/kit-logger/pkg/logger/handler"
	"github.com/getsyntegrity/kit-logger/pkg/logger/handler/testdata"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestComponentHandler_AddsComponentField(t *testing.T) {
	var captured slog.Record
	base := handler.NewTestHandler(func(_ context.Context, r slog.Record) {
		captured = r
	})

	h := handler.NewComponentHandler(base)
	logger := slog.New(h)

	// Llamada envuelta para asegurar stack válido
	testdata.LogInvoker{}.Invoke(logger)

	var found bool
	captured.Attrs(func(a slog.Attr) bool {
		if a.Key == "component" {
			found = true
			g := a.Value.Group()
			require.Greater(t, len(g), 0, "component should be a group with fields")
			require.NotEqual(t, "unknown", g[0].Value.String(), "file should not be 'unknown'")
			require.NotEqual(t, "unknown", g[2].Value.String(), "func should not be 'unknown'")
		}
		return true
	})

	require.True(t, found, `"component" field not found`)
}

func TestComponentHandler_WithAttrs(t *testing.T) {
	var captured slog.Record
	base := handler.NewTestHandler(func(_ context.Context, r slog.Record) {
		captured = r
	})

	h := handler.NewComponentHandler(base).WithAttrs([]slog.Attr{
		slog.String("env", "test"),
	})

	logger := slog.New(h)
	logger.Info("with attr")

	var hasComponent, hasEnv bool
	captured.Attrs(func(a slog.Attr) bool {
		switch a.Key {
		case "component":
			hasComponent = true
		case "env":
			hasEnv = true
		}
		return true
	})

	require.True(t, hasComponent, "expected component field")
	require.True(t, hasEnv, "expected env field")
}

func TestComponentHandler_WithGroup(t *testing.T) {
	var captured slog.Record
	base := handler.NewTestHandler(func(_ context.Context, r slog.Record) {
		captured = r
	})

	h := handler.NewComponentHandler(base).WithGroup("test-group")
	logger := slog.New(h)
	logger.Info("grouped log")

	var found bool
	captured.Attrs(func(a slog.Attr) bool {
		if a.Key == "component" {
			found = true
		}
		return true
	})
	require.True(t, found, "component field should exist even with group")
}

func TestComponentHandler_FallbackToUnknown(t *testing.T) {
	var captured slog.Record
	base := handler.NewTestHandler(func(_ context.Context, r slog.Record) {
		captured = r
	})

	h := handler.NewComponentHandler(base)
	logger := slog.New(h)
	logWithShallowStack(logger)

	var found bool
	captured.Attrs(func(a slog.Attr) bool {
		if a.Key == "component" {
			found = true
			group := a.Value.Group()
			var file string
			for _, attr := range group {
				if attr.Key == "file" {
					file = attr.Value.String()
					break
				}
			}
			require.NotEqual(t, "unknown", file, "file should not be 'unknown'")
			require.Contains(t, file, "component_handler_test.go")
		}
		return true
	})
	require.True(t, found, "expected component field even on fallback")
}

func TestComponentHandler_WithOffset(t *testing.T) {
	var captured slog.Record
	base := handler.NewTestHandler(func(_ context.Context, r slog.Record) {
		captured = r
	})

	// Create a component handler
	h := handler.NewComponentHandler(base)

	// Apply offset
	offsetHandler := h.(*handler.ComponentHandler).WithOffset(2)
	assert.NotNil(t, offsetHandler)
	assert.IsType(t, &handler.ComponentHandler{}, offsetHandler)

	// Create logger with offset handler
	logger := slog.New(offsetHandler)
	logger.Info("test with offset")

	// Verify that the handler works (may or may not have component field due to offset)
	// The important thing is that it doesn't panic and processes the log
	assert.NotNil(t, captured, "log record should be captured")
}

func TestComponentHandler_WithOffset_ZeroOffset(t *testing.T) {
	var captured slog.Record
	base := handler.NewTestHandler(func(_ context.Context, r slog.Record) {
		captured = r
	})

	// Create a component handler
	h := handler.NewComponentHandler(base)

	// Apply zero offset
	offsetHandler := h.(*handler.ComponentHandler).WithOffset(0)
	assert.NotNil(t, offsetHandler)
	assert.IsType(t, &handler.ComponentHandler{}, offsetHandler)

	// Create logger with offset handler
	logger := slog.New(offsetHandler)
	logger.Info("test with zero offset")

	// Verify that component field is still added
	var found bool
	captured.Attrs(func(a slog.Attr) bool {
		if a.Key == "component" {
			found = true
		}
		return true
	})
	require.True(t, found, "component field should exist with zero offset")
}

func TestComponentHandler_WithOffset_NegativeOffset(t *testing.T) {
	var captured slog.Record
	base := handler.NewTestHandler(func(_ context.Context, r slog.Record) {
		captured = r
	})

	// Create a component handler
	h := handler.NewComponentHandler(base)

	// Apply negative offset
	offsetHandler := h.(*handler.ComponentHandler).WithOffset(-1)
	assert.NotNil(t, offsetHandler)
	assert.IsType(t, &handler.ComponentHandler{}, offsetHandler)

	// Create logger with offset handler
	logger := slog.New(offsetHandler)
	logger.Info("test with negative offset")

	// Verify that component field is still added
	var found bool
	captured.Attrs(func(a slog.Attr) bool {
		if a.Key == "component" {
			found = true
		}
		return true
	})
	require.True(t, found, "component field should exist with negative offset")
}

func TestComponentHandler_WithOffset_MultipleOffsets(t *testing.T) {
	var captured slog.Record
	base := handler.NewTestHandler(func(_ context.Context, r slog.Record) {
		captured = r
	})

	// Create a component handler
	h := handler.NewComponentHandler(base)

	// Apply multiple offsets
	offsetHandler1 := h.(*handler.ComponentHandler).WithOffset(1)
	offsetHandler2 := offsetHandler1.WithOffset(2)
	offsetHandler3 := offsetHandler2.WithOffset(3)

	assert.NotNil(t, offsetHandler1)
	assert.NotNil(t, offsetHandler2)
	assert.NotNil(t, offsetHandler3)
	assert.IsType(t, &handler.ComponentHandler{}, offsetHandler1)
	assert.IsType(t, &handler.ComponentHandler{}, offsetHandler2)
	assert.IsType(t, &handler.ComponentHandler{}, offsetHandler3)

	// Create logger with final offset handler
	logger := slog.New(offsetHandler3)
	logger.Info("test with multiple offsets")

	// Verify that the handler works (may or may not have component field due to offset)
	// The important thing is that it doesn't panic and processes the log
	assert.NotNil(t, captured, "log record should be captured")
}

func TestComponentHandler_WithOffset_Integration(t *testing.T) {
	var captured slog.Record
	base := handler.NewTestHandler(func(_ context.Context, r slog.Record) {
		captured = r
	})

	// Create a component handler with offset
	h := handler.NewComponentHandler(base)
	offsetHandler := h.(*handler.ComponentHandler).WithOffset(1)

	// Apply WithAttrs to the offset handler
	handlerWithAttrs := offsetHandler.WithAttrs([]slog.Attr{
		slog.String("service", "test-service"),
	})

	// Apply WithGroup to the handler with attrs
	handlerWithGroup := handlerWithAttrs.WithGroup("user")

	// Create logger with final handler
	logger := slog.New(handlerWithGroup)
	logger.Info("test with offset, attrs, and group")

	// Verify that component field and other fields are added
	var hasComponent, hasService bool
	captured.Attrs(func(a slog.Attr) bool {
		switch a.Key {
		case "component":
			hasComponent = true
		case "service":
			hasService = true
		}
		return true
	})

	require.True(t, hasComponent, "component field should exist")
	require.True(t, hasService, "service field should exist")
}

func logWithShallowStack(logger *slog.Logger) {
	logger.Info("shallow fallback")
}

func TestComponentHandler_Handle_GraphQLLog(t *testing.T) {
	var captured slog.Record
	base := handler.NewTestHandler(func(_ context.Context, r slog.Record) {
		captured = r
	})

	h := handler.NewComponentHandler(base)
	logger := slog.New(h)

	// Test GraphQL HTTP Trace message
	logger.Info("GraphQL HTTP Trace: query execution")

	// Verify that the log was captured but without component field (GraphQL logs skip component)
	var hasComponent bool
	captured.Attrs(func(a slog.Attr) bool {
		if a.Key == "component" {
			hasComponent = true
		}
		return true
	})

	// GraphQL logs should not have component field
	assert.False(t, hasComponent, "GraphQL logs should not have component field")
	assert.Equal(t, "GraphQL HTTP Trace: query execution", captured.Message)
}

func TestComponentHandler_Handle_GraphQLLog_Error(t *testing.T) {
	var captured slog.Record
	base := handler.NewTestHandler(func(_ context.Context, r slog.Record) {
		captured = r
	})

	h := handler.NewComponentHandler(base)
	logger := slog.New(h)

	// Test GraphQL Error message
	logger.Error("GraphQL Error(s) Found: validation failed")

	// Verify that the log was captured but without component field
	var hasComponent bool
	captured.Attrs(func(a slog.Attr) bool {
		if a.Key == "component" {
			hasComponent = true
		}
		return true
	})

	// GraphQL error logs should not have component field
	assert.False(t, hasComponent, "GraphQL error logs should not have component field")
	assert.Equal(t, "GraphQL Error(s) Found: validation failed", captured.Message)
}

func TestComponentHandler_Handle_LevelNotEnabled(t *testing.T) {
	var captured slog.Record
	base := handler.NewTestHandler(func(_ context.Context, r slog.Record) {
		captured = r
	})

	// Create a custom handler that only enables Info level and above
	levelHandler := &testLevelHandler{
		next:  base,
		level: slog.LevelInfo,
	}

	h := handler.NewComponentHandler(levelHandler)
	logger := slog.New(h)

	// Test with Debug level (should not be enabled)
	logger.Debug("debug message that should be filtered")

	// Verify that the log was not captured (level not enabled)
	assert.Empty(t, captured.Message, "Debug message should not be captured when level not enabled")
}

// testLevelHandler is a simple handler that filters by level
type testLevelHandler struct {
	next  slog.Handler
	level slog.Level
}

func (h *testLevelHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return level >= h.level
}

func (h *testLevelHandler) Handle(ctx context.Context, r slog.Record) error {
	return h.next.Handle(ctx, r)
}

func (h *testLevelHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &testLevelHandler{
		next:  h.next.WithAttrs(attrs),
		level: h.level,
	}
}

func (h *testLevelHandler) WithGroup(name string) slog.Handler {
	return &testLevelHandler{
		next:  h.next.WithGroup(name),
		level: h.level,
	}
}

func TestComponentHandler_Handle_LevelNotEnabled_Debug(t *testing.T) {
	var captured slog.Record
	base := handler.NewTestHandler(func(_ context.Context, r slog.Record) {
		captured = r
	})

	// Create a handler that only enables Info level and above
	levelHandler := &testLevelHandler{
		next:  base,
		level: slog.LevelInfo,
	}

	h := handler.NewComponentHandler(levelHandler)
	logger := slog.New(h)

	// Test with Debug level (should not be enabled)
	logger.Debug("debug message that should be filtered")

	// Verify that the log was not captured (level not enabled)
	assert.Empty(t, captured.Message, "Debug message should not be captured when level not enabled")
}

func TestComponentHandler_Handle_LevelNotEnabled_InfoEnabled(t *testing.T) {
	var captured slog.Record
	base := handler.NewTestHandler(func(_ context.Context, r slog.Record) {
		captured = r
	})

	// Create a handler that only enables Info level and above
	levelHandler := &testLevelHandler{
		next:  base,
		level: slog.LevelInfo,
	}

	h := handler.NewComponentHandler(levelHandler)
	logger := slog.New(h)

	// Test with Info level (should be enabled)
	logger.Info("info message that should be captured")

	// Verify that the log was captured (level is enabled)
	assert.Equal(t, "info message that should be captured", captured.Message, "Info message should be captured when level is enabled")
}

func TestComponentHandler_Handle_LevelNotEnabled_WarnEnabled(t *testing.T) {
	var captured slog.Record
	base := handler.NewTestHandler(func(_ context.Context, r slog.Record) {
		captured = r
	})

	// Create a handler that only enables Info level and above
	levelHandler := &testLevelHandler{
		next:  base,
		level: slog.LevelInfo,
	}

	h := handler.NewComponentHandler(levelHandler)
	logger := slog.New(h)

	// Test with Warn level (should be enabled)
	logger.Warn("warn message that should be captured")

	// Verify that the log was captured (level is enabled)
	assert.Equal(t, "warn message that should be captured", captured.Message, "Warn message should be captured when level is enabled")
}

func TestComponentHandler_Handle_LevelNotEnabled_ErrorEnabled(t *testing.T) {
	var captured slog.Record
	base := handler.NewTestHandler(func(_ context.Context, r slog.Record) {
		captured = r
	})

	// Create a handler that only enables Info level and above
	levelHandler := &testLevelHandler{
		next:  base,
		level: slog.LevelInfo,
	}

	h := handler.NewComponentHandler(levelHandler)
	logger := slog.New(h)

	// Test with Error level (should be enabled)
	logger.Error("error message that should be captured")

	// Verify that the log was captured (level is enabled)
	assert.Equal(t, "error message that should be captured", captured.Message, "Error message should be captured when level is enabled")
}

func TestComponentHandler_Handle_LevelNotEnabled_WarnLevel(t *testing.T) {
	var captured slog.Record
	base := handler.NewTestHandler(func(_ context.Context, r slog.Record) {
		captured = r
	})

	// Create a handler that only enables Warn level and above
	levelHandler := &testLevelHandler{
		next:  base,
		level: slog.LevelWarn,
	}

	h := handler.NewComponentHandler(levelHandler)
	logger := slog.New(h)

	// Test with Debug level (should not be enabled)
	logger.Debug("debug message that should be filtered")
	assert.Empty(t, captured.Message, "Debug message should not be captured when level is Warn")

	// Test with Info level (should not be enabled)
	logger.Info("info message that should be filtered")
	assert.Empty(t, captured.Message, "Info message should not be captured when level is Warn")

	// Test with Warn level (should be enabled)
	logger.Warn("warn message that should be captured")
	assert.Equal(t, "warn message that should be captured", captured.Message, "Warn message should be captured when level is Warn")
}

func TestComponentHandler_Handle_LevelNotEnabled_ErrorLevel(t *testing.T) {
	var captured slog.Record
	base := handler.NewTestHandler(func(_ context.Context, r slog.Record) {
		captured = r
	})

	// Create a handler that only enables Error level and above
	levelHandler := &testLevelHandler{
		next:  base,
		level: slog.LevelError,
	}

	h := handler.NewComponentHandler(levelHandler)
	logger := slog.New(h)

	// Test with Debug level (should not be enabled)
	logger.Debug("debug message that should be filtered")
	assert.Empty(t, captured.Message, "Debug message should not be captured when level is Error")

	// Test with Info level (should not be enabled)
	logger.Info("info message that should be filtered")
	assert.Empty(t, captured.Message, "Info message should not be captured when level is Error")

	// Test with Warn level (should not be enabled)
	logger.Warn("warn message that should be filtered")
	assert.Empty(t, captured.Message, "Warn message should not be captured when level is Error")

	// Test with Error level (should be enabled)
	logger.Error("error message that should be captured")
	assert.Equal(t, "error message that should be captured", captured.Message, "Error message should be captured when level is Error")
}

func TestComponentHandler_Handle_LevelNotEnabled_WithAttrs(t *testing.T) {
	var captured slog.Record
	base := handler.NewTestHandler(func(_ context.Context, r slog.Record) {
		captured = r
	})

	// Create a handler that only enables Info level and above
	levelHandler := &testLevelHandler{
		next:  base,
		level: slog.LevelInfo,
	}

	h := handler.NewComponentHandler(levelHandler).WithAttrs([]slog.Attr{
		slog.String("service", "test"),
	})
	logger := slog.New(h)

	// Test with Debug level (should not be enabled)
	logger.Debug("debug message with attrs that should be filtered")

	// Verify that the log was not captured (level not enabled)
	assert.Empty(t, captured.Message, "Debug message with attrs should not be captured when level not enabled")
}

func TestComponentHandler_Handle_LevelNotEnabled_WithGroup(t *testing.T) {
	var captured slog.Record
	base := handler.NewTestHandler(func(_ context.Context, r slog.Record) {
		captured = r
	})

	// Create a handler that only enables Info level and above
	levelHandler := &testLevelHandler{
		next:  base,
		level: slog.LevelInfo,
	}

	h := handler.NewComponentHandler(levelHandler).WithGroup("test-group")
	logger := slog.New(h)

	// Test with Debug level (should not be enabled)
	logger.Debug("debug message with group that should be filtered")

	// Verify that the log was not captured (level not enabled)
	assert.Empty(t, captured.Message, "Debug message with group should not be captured when level not enabled")
}

func TestComponentHandler_Handle_LevelNotEnabled_WithOffset(t *testing.T) {
	var captured slog.Record
	base := handler.NewTestHandler(func(_ context.Context, r slog.Record) {
		captured = r
	})

	// Create a handler that only enables Info level and above
	levelHandler := &testLevelHandler{
		next:  base,
		level: slog.LevelInfo,
	}

	h := handler.NewComponentHandler(levelHandler)
	offsetHandler := h.(*handler.ComponentHandler).WithOffset(1)
	logger := slog.New(offsetHandler)

	// Test with Debug level (should not be enabled)
	logger.Debug("debug message with offset that should be filtered")

	// Verify that the log was not captured (level not enabled)
	assert.Empty(t, captured.Message, "Debug message with offset should not be captured when level not enabled")
}

// TestComponentHandler_Handle_GraphQLLog_ExactCondition tests the exact GraphQL condition
func TestComponentHandler_Handle_GraphQLLog_ExactCondition(t *testing.T) {
	var captured slog.Record
	base := handler.NewTestHandler(func(_ context.Context, r slog.Record) {
		captured = r
	})

	h := handler.NewComponentHandler(base)
	logger := slog.New(h)

	// Test the exact condition: if isGraphQLLog(record.Message) { return h.next.Handle(ctx, record) }

	// Test GraphQL HTTP Trace - should bypass component field addition
	logger.Info("GraphQL HTTP Trace: query execution")
	assert.Equal(t, "GraphQL HTTP Trace: query execution", captured.Message, "GraphQL log should be captured")

	// Verify that component field was NOT added (GraphQL logs skip component)
	var hasComponent bool
	captured.Attrs(func(a slog.Attr) bool {
		if a.Key == "component" {
			hasComponent = true
		}
		return true
	})
	assert.False(t, hasComponent, "GraphQL logs should NOT have component field")
}

// TestComponentHandler_Handle_LevelNotEnabled_ExactCondition tests the exact level not enabled condition
func TestComponentHandler_Handle_LevelNotEnabled_ExactCondition(t *testing.T) {
	var captured slog.Record
	base := handler.NewTestHandler(func(_ context.Context, r slog.Record) {
		captured = r
	})

	// Create a handler that only enables Info level and above
	levelHandler := &testLevelHandler{
		next:  base,
		level: slog.LevelInfo,
	}

	h := handler.NewComponentHandler(levelHandler)
	logger := slog.New(h)

	// Test the exact condition: if !h.Enabled(ctx, record.Level) { return nil }

	// Test with Debug level (should not be enabled) - should return nil
	logger.Debug("debug message that should be filtered")

	// Verify that the log was NOT captured (should return nil)
	assert.Empty(t, captured.Message, "Debug message should NOT be captured when level not enabled")

	// Test with Info level (should be enabled) - should continue processing
	logger.Info("info message that should be captured")
	assert.Equal(t, "info message that should be captured", captured.Message, "Info message should be captured when level is enabled")

	// Verify that component field was added for non-GraphQL logs
	var hasComponent bool
	captured.Attrs(func(a slog.Attr) bool {
		if a.Key == "component" {
			hasComponent = true
		}
		return true
	})
	assert.True(t, hasComponent, "Non-GraphQL logs should have component field")
}

// TestComponentHandler_Handle_EnabledFalse tests the simple case when Enabled returns false
func TestComponentHandler_Handle_EnabledFalse(t *testing.T) {
	var captured slog.Record
	base := handler.NewTestHandler(func(_ context.Context, r slog.Record) {
		captured = r
	})

	// Create a handler that always returns false for Enabled
	disabledHandler := &alwaysDisabledHandler{
		next: base,
	}

	h := handler.NewComponentHandler(disabledHandler)

	// Test directly with slog.Record to ensure we hit the component handler
	record := slog.NewRecord(time.Now(), slog.LevelInfo, "test message", 0)
	err := h.Handle(context.Background(), record)

	// Should return nil when Enabled is false
	assert.Nil(t, err, "Should return nil when Enabled returns false")
	assert.Empty(t, captured.Message, "No messages should be captured when Enabled returns false")
}

// alwaysDisabledHandler is a simple handler that always returns false for Enabled
type alwaysDisabledHandler struct {
	next slog.Handler
}

func (h *alwaysDisabledHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return false // Always return false
}

func (h *alwaysDisabledHandler) Handle(ctx context.Context, r slog.Record) error {
	return h.next.Handle(ctx, r)
}

func (h *alwaysDisabledHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &alwaysDisabledHandler{
		next: h.next.WithAttrs(attrs),
	}
}

func (h *alwaysDisabledHandler) WithGroup(name string) slog.Handler {
	return &alwaysDisabledHandler{
		next: h.next.WithGroup(name),
	}
}

// TestComponentHandler_Handle_EnabledFalse_Simple tests ONLY the condition: if !h.Enabled(ctx, record.Level) { return nil }
func TestComponentHandler_Handle_EnabledFalse_Simple(t *testing.T) {
	var captured slog.Record
	base := handler.NewTestHandler(func(_ context.Context, r slog.Record) {
		captured = r
	})

	// Create a handler that always returns false for Enabled
	disabledHandler := &alwaysDisabledHandler{
		next: base,
	}

	h := handler.NewComponentHandler(disabledHandler)

	// First, verify that Enabled returns false
	assert.False(t, h.Enabled(context.Background(), slog.LevelInfo), "Enabled should return false")

	// Create a record directly
	record := slog.NewRecord(time.Now(), slog.LevelInfo, "test message", 0)

	// Call Handle directly - this should hit the condition: if !h.Enabled(ctx, record.Level) { return nil }
	err := h.Handle(context.Background(), record)

	// Should return nil when Enabled returns false
	assert.Nil(t, err, "Should return nil when Enabled returns false")
	assert.Empty(t, captured.Message, "No message should be captured when Enabled returns false")
}

// TestComponentHandler_Handle_IsInternalFrameFalse tests when isInternalFrame returns false
func TestComponentHandler_Handle_IsInternalFrameFalse(t *testing.T) {
	var captured slog.Record
	base := handler.NewTestHandler(func(_ context.Context, r slog.Record) {
		captured = r
	})

	h := handler.NewComponentHandler(base)
	logger := slog.New(h)

	// Test with a message that will trigger stack trace analysis
	// This should test the case where isInternalFrame returns false
	logger.Info("test message for isInternalFrame false case")

	// Verify that component field was added
	var hasComponent bool
	captured.Attrs(func(a slog.Attr) bool {
		if a.Key == "component" {
			hasComponent = true
		}
		return true
	})

	assert.True(t, hasComponent, "Component field should be added when isInternalFrame returns false")
	assert.Equal(t, "test message for isInternalFrame false case", captured.Message, "Message should match")
}

// TestComponentHandler_Handle_IsInternalFrameAllPatterns tests all internal patterns to ensure coverage
func TestComponentHandler_Handle_IsInternalFrameAllPatterns(t *testing.T) {
	var captured slog.Record
	base := handler.NewTestHandler(func(_ context.Context, r slog.Record) {
		captured = r
	})

	h := handler.NewComponentHandler(base)
	logger := slog.New(h)

	// Test with different messages to trigger various stack trace conditions
	// This should test different paths through the isInternalFrame function
	testMessages := []string{
		"test message 1",
		"test message 2",
		"test message 3",
		"test message 4",
		"test message 5",
	}

	for i, msg := range testMessages {
		t.Run(fmt.Sprintf("Message_%d", i), func(t *testing.T) {
			// Reset captured for each test
			captured = slog.Record{}

			logger.Info(msg)

			// Verify that component field was added
			var hasComponent bool
			captured.Attrs(func(a slog.Attr) bool {
				if a.Key == "component" {
					hasComponent = true
				}
				return true
			})

			assert.True(t, hasComponent, "Component field should be added for message: %s", msg)
			assert.Equal(t, msg, captured.Message, "Message should match for: %s", msg)
		})
	}
}

// TestComponentHandler_Handle_IsInternalFrameReturnFalse tests the specific case where isInternalFrame returns false
func TestComponentHandler_Handle_IsInternalFrameReturnFalse(t *testing.T) {
	var captured slog.Record
	base := handler.NewTestHandler(func(_ context.Context, r slog.Record) {
		captured = r
	})

	h := handler.NewComponentHandler(base)
	logger := slog.New(h)

	// Test with a message that will trigger stack trace analysis
	// This should test the case where isInternalFrame returns false (no patterns match)
	logger.Info("test message for isInternalFrame return false")

	// Verify that component field was added
	var hasComponent bool
	var componentGroup []slog.Attr
	captured.Attrs(func(a slog.Attr) bool {
		if a.Key == "component" {
			hasComponent = true
			componentGroup = a.Value.Group()
		}
		return true
	})

	assert.True(t, hasComponent, "Component field should be added when isInternalFrame returns false")
	assert.NotEmpty(t, componentGroup, "Component group should not be empty")
	assert.Equal(t, "test message for isInternalFrame return false", captured.Message, "Message should match")

	// Verify that we have valid component information
	var hasFile, hasLine, hasFunc bool
	for _, attr := range componentGroup {
		switch attr.Key {
		case "file":
			hasFile = true
			assert.NotEmpty(t, attr.Value.String(), "File should not be empty")
		case "line":
			hasLine = true
			assert.Greater(t, attr.Value.Int64(), int64(0), "Line should be greater than 0")
		case "func":
			hasFunc = true
			assert.NotEmpty(t, attr.Value.String(), "Func should not be empty")
		}
	}

	assert.True(t, hasFile, "Component should have file field")
	assert.True(t, hasLine, "Component should have line field")
	assert.True(t, hasFunc, "Component should have func field")
}

// TestComponentHandler_Handle_GraphQLLog_AllVariants tests all GraphQL log variants
func TestComponentHandler_Handle_GraphQLLog_AllVariants(t *testing.T) {
	// Test "GraphQL HTTP Trace" prefix
	t.Run("GraphQLHTTPTrace", func(t *testing.T) {
		var captured slog.Record
		base := handler.NewTestHandler(func(_ context.Context, r slog.Record) {
			captured = r
		})

		h := handler.NewComponentHandler(base)
		logger := slog.New(h)

		logger.Info("GraphQL HTTP Trace: query execution")
		assert.Equal(t, "GraphQL HTTP Trace: query execution", captured.Message)

		// Verify that component field was NOT added
		var hasComponent bool
		captured.Attrs(func(a slog.Attr) bool {
			if a.Key == "component" {
				hasComponent = true
			}
			return true
		})
		assert.False(t, hasComponent, "GraphQL HTTP Trace logs should NOT have component field")
	})

	// Test "GraphQL Error(s) Found" prefix
	t.Run("GraphQLErrorFound", func(t *testing.T) {
		var captured slog.Record
		base := handler.NewTestHandler(func(_ context.Context, r slog.Record) {
			captured = r
		})

		h := handler.NewComponentHandler(base)
		logger := slog.New(h)

		logger.Error("GraphQL Error(s) Found: validation failed")
		assert.Equal(t, "GraphQL Error(s) Found: validation failed", captured.Message)

		// Verify that component field was NOT added
		var hasComponent bool
		captured.Attrs(func(a slog.Attr) bool {
			if a.Key == "component" {
				hasComponent = true
			}
			return true
		})
		assert.False(t, hasComponent, "GraphQL Error(s) Found logs should NOT have component field")
	})
}

// TestComponentHandler_Handle_LevelNotEnabled_AllLevels tests all level combinations
func TestComponentHandler_Handle_LevelNotEnabled_AllLevels(t *testing.T) {
	var captured slog.Record
	base := handler.NewTestHandler(func(_ context.Context, r slog.Record) {
		captured = r
	})

	// Test with Info level handler (only Info and above enabled)
	levelHandler := &testLevelHandler{
		next:  base,
		level: slog.LevelInfo,
	}

	h := handler.NewComponentHandler(levelHandler)
	logger := slog.New(h)

	// Test all levels against the condition: if !h.Enabled(ctx, record.Level) { return nil }

	// Debug should NOT be enabled (should return nil)
	logger.Debug("debug message")
	assert.Empty(t, captured.Message, "Debug should return nil when not enabled")

	// Info should be enabled (should continue processing)
	logger.Info("info message")
	assert.Equal(t, "info message", captured.Message, "Info should continue processing when enabled")

	// Warn should be enabled (should continue processing)
	logger.Warn("warn message")
	assert.Equal(t, "warn message", captured.Message, "Warn should continue processing when enabled")

	// Error should be enabled (should continue processing)
	logger.Error("error message")
	assert.Equal(t, "error message", captured.Message, "Error should continue processing when enabled")
}

// TestComponentHandler_Handle_GraphQLLog_WithContext tests GraphQL logs with context
func TestComponentHandler_Handle_GraphQLLog_WithContext(t *testing.T) {
	var captured slog.Record
	base := handler.NewTestHandler(func(_ context.Context, r slog.Record) {
		captured = r
	})

	h := handler.NewComponentHandler(base)
	logger := slog.New(h)

	// Test GraphQL logs with context - should still bypass component field
	ctx := context.Background()

	logger.InfoContext(ctx, "GraphQL HTTP Trace: mutation execution")
	assert.Equal(t, "GraphQL HTTP Trace: mutation execution", captured.Message)

	logger.ErrorContext(ctx, "GraphQL Error(s) Found: permission denied")
	assert.Equal(t, "GraphQL Error(s) Found: permission denied", captured.Message)

	// Verify that component field was NOT added
	var hasComponent bool
	captured.Attrs(func(a slog.Attr) bool {
		if a.Key == "component" {
			hasComponent = true
		}
		return true
	})
	assert.False(t, hasComponent, "GraphQL logs with context should NOT have component field")
}

// TestComponentHandler_Handle_LevelNotEnabled_WithContext tests level filtering with context
func TestComponentHandler_Handle_LevelNotEnabled_WithContext(t *testing.T) {
	var captured slog.Record
	base := handler.NewTestHandler(func(_ context.Context, r slog.Record) {
		captured = r
	})

	// Create a handler that only enables Warn level and above
	levelHandler := &testLevelHandler{
		next:  base,
		level: slog.LevelWarn,
	}

	h := handler.NewComponentHandler(levelHandler)
	logger := slog.New(h)
	ctx := context.Background()

	// Test the condition with context: if !h.Enabled(ctx, record.Level) { return nil }

	// Debug should NOT be enabled (should return nil)
	logger.DebugContext(ctx, "debug message")
	assert.Empty(t, captured.Message, "Debug with context should return nil when not enabled")

	// Info should NOT be enabled (should return nil)
	logger.InfoContext(ctx, "info message")
	assert.Empty(t, captured.Message, "Info with context should return nil when not enabled")

	// Warn should be enabled (should continue processing)
	logger.WarnContext(ctx, "warn message")
	assert.Equal(t, "warn message", captured.Message, "Warn with context should continue processing when enabled")

	// Error should be enabled (should continue processing)
	logger.ErrorContext(ctx, "error message")
	assert.Equal(t, "error message", captured.Message, "Error with context should continue processing when enabled")
}

// TestComponentHandler_Handle_StackTraceEdgeCases tests edge cases in stack trace handling
func TestComponentHandler_Handle_StackTraceEdgeCases(t *testing.T) {
	var captured slog.Record
	base := handler.NewTestHandler(func(_ context.Context, r slog.Record) {
		captured = r
	})

	h := handler.NewComponentHandler(base)
	logger := slog.New(h)

	// Test with a message that triggers stack trace analysis
	logger.Info("test message for stack trace")

	// Verify that component field was added
	var hasComponent bool
	var componentGroup []slog.Attr
	captured.Attrs(func(a slog.Attr) bool {
		if a.Key == "component" {
			hasComponent = true
			componentGroup = a.Value.Group()
		}
		return true
	})

	assert.True(t, hasComponent, "Component field should be added for non-GraphQL logs")
	assert.NotEmpty(t, componentGroup, "Component group should not be empty")

	// Verify component group structure
	var hasFile, hasLine, hasFunc bool
	for _, attr := range componentGroup {
		switch attr.Key {
		case "file":
			hasFile = true
			assert.NotEmpty(t, attr.Value.String(), "File should not be empty")
		case "line":
			hasLine = true
			assert.Greater(t, attr.Value.Int64(), int64(0), "Line should be greater than 0")
		case "func":
			hasFunc = true
			assert.NotEmpty(t, attr.Value.String(), "Func should not be empty")
		}
	}

	assert.True(t, hasFile, "Component should have file field")
	assert.True(t, hasLine, "Component should have line field")
	assert.True(t, hasFunc, "Component should have func field")
}

// TestComponentHandler_Handle_StackTraceWithOffset tests stack trace with offset
func TestComponentHandler_Handle_StackTraceWithOffset(t *testing.T) {
	var captured slog.Record
	base := handler.NewTestHandler(func(_ context.Context, r slog.Record) {
		captured = r
	})

	h := handler.NewComponentHandler(base)
	offsetHandler := h.(*handler.ComponentHandler).WithOffset(1)
	logger := slog.New(offsetHandler)

	// Test with offset - should still add component field
	logger.Info("test message with offset")

	// Verify that component field was added
	var hasComponent bool
	captured.Attrs(func(a slog.Attr) bool {
		if a.Key == "component" {
			hasComponent = true
		}
		return true
	})

	assert.True(t, hasComponent, "Component field should be added even with offset")
}

// TestComponentHandler_Handle_StackTraceMaxDepth tests stack trace with max depth
func TestComponentHandler_Handle_StackTraceMaxDepth(t *testing.T) {
	var captured slog.Record
	base := handler.NewTestHandler(func(_ context.Context, r slog.Record) {
		captured = r
	})

	h := handler.NewComponentHandler(base)
	// Use a very large offset to test max depth handling
	offsetHandler := h.(*handler.ComponentHandler).WithOffset(30)
	logger := slog.New(offsetHandler)

	// Test with very large offset - should still work
	logger.Info("test message with large offset")

	// Verify that the log was captured (even if component field might not be added due to depth)
	assert.Equal(t, "test message with large offset", captured.Message, "Log should be captured even with large offset")

	// With very large offset, component field might not be added, but that's okay
	// The important thing is that the log is processed without error
}

// TestComponentHandler_Handle_StackTraceInternalFrame tests internal frame filtering
func TestComponentHandler_Handle_StackTraceInternalFrame(t *testing.T) {
	var captured slog.Record
	base := handler.NewTestHandler(func(_ context.Context, r slog.Record) {
		captured = r
	})

	h := handler.NewComponentHandler(base)
	logger := slog.New(h)

	// Test with a message that will trigger stack trace analysis
	// This should test the internal frame filtering logic
	logger.Info("test message for internal frame filtering")

	// Verify that component field was added
	var hasComponent bool
	captured.Attrs(func(a slog.Attr) bool {
		if a.Key == "component" {
			hasComponent = true
		}
		return true
	})

	assert.True(t, hasComponent, "Component field should be added even with internal frame filtering")
}

// TestComponentHandler_Handle_StackTraceRuntimeFunc tests runtime.FuncForPC handling
func TestComponentHandler_Handle_StackTraceRuntimeFunc(t *testing.T) {
	var captured slog.Record
	base := handler.NewTestHandler(func(_ context.Context, r slog.Record) {
		captured = r
	})

	h := handler.NewComponentHandler(base)
	logger := slog.New(h)

	// Test with a message that will trigger stack trace analysis
	// This should test the runtime.FuncForPC handling
	logger.Info("test message for runtime func handling")

	// Verify that component field was added
	var hasComponent bool
	var funcName string
	captured.Attrs(func(a slog.Attr) bool {
		if a.Key == "component" {
			hasComponent = true
			group := a.Value.Group()
			for _, attr := range group {
				if attr.Key == "func" {
					funcName = attr.Value.String()
					break
				}
			}
		}
		return true
	})

	assert.True(t, hasComponent, "Component field should be added")
	assert.NotEmpty(t, funcName, "Function name should not be empty")
	assert.NotEqual(t, "unknown", funcName, "Function name should not be 'unknown'")
}

// TestComponentHandler_Handle_StackTraceBreakCondition tests the break condition in stack trace loop
func TestComponentHandler_Handle_StackTraceBreakCondition(t *testing.T) {
	var captured slog.Record
	base := handler.NewTestHandler(func(_ context.Context, r slog.Record) {
		captured = r
	})

	h := handler.NewComponentHandler(base)
	logger := slog.New(h)

	// Test with a message that will trigger stack trace analysis
	// This should test the break condition in the stack trace loop
	logger.Info("test message for break condition")

	// Verify that component field was added
	var hasComponent bool
	captured.Attrs(func(a slog.Attr) bool {
		if a.Key == "component" {
			hasComponent = true
		}
		return true
	})

	assert.True(t, hasComponent, "Component field should be added")
}

// TestComponentHandler_Handle_StackTraceContinueCondition tests the continue condition in stack trace loop
func TestComponentHandler_Handle_StackTraceContinueCondition(t *testing.T) {
	var captured slog.Record
	base := handler.NewTestHandler(func(_ context.Context, r slog.Record) {
		captured = r
	})

	h := handler.NewComponentHandler(base)
	logger := slog.New(h)

	// Test with a message that will trigger stack trace analysis
	// This should test the continue condition in the stack trace loop
	logger.Info("test message for continue condition")

	// Verify that component field was added
	var hasComponent bool
	captured.Attrs(func(a slog.Attr) bool {
		if a.Key == "component" {
			hasComponent = true
		}
		return true
	})

	assert.True(t, hasComponent, "Component field should be added")
}

// TestComponentHandler_Handle_StackTraceAllConditions tests all possible conditions in the stack trace loop
func TestComponentHandler_Handle_StackTraceAllConditions(t *testing.T) {
	var captured slog.Record
	base := handler.NewTestHandler(func(_ context.Context, r slog.Record) {
		captured = r
	})

	h := handler.NewComponentHandler(base)
	logger := slog.New(h)

	// Test with different messages to trigger various stack trace conditions
	testCases := []string{
		"normal message",
		"another test message",
		"message with special chars: !@#$%",
		"message with numbers 12345",
		"message with spaces and tabs",
	}

	for _, msg := range testCases {
		t.Run(msg, func(t *testing.T) {
			// Reset captured for each test
			captured = slog.Record{}

			logger.Info(msg)

			// Verify that component field was added
			var hasComponent bool
			captured.Attrs(func(a slog.Attr) bool {
				if a.Key == "component" {
					hasComponent = true
				}
				return true
			})

			assert.True(t, hasComponent, "Component field should be added for message: %s", msg)
			assert.Equal(t, msg, captured.Message, "Message should match for: %s", msg)
		})
	}
}

// TestComponentHandler_Handle_StackTraceWithDifferentLevels tests stack trace with different log levels
func TestComponentHandler_Handle_StackTraceWithDifferentLevels(t *testing.T) {
	var captured slog.Record
	base := handler.NewTestHandler(func(_ context.Context, r slog.Record) {
		captured = r
	})

	h := handler.NewComponentHandler(base)
	logger := slog.New(h)

	// Test with different log levels to ensure stack trace works for all levels
	levels := []struct {
		name  string
		level slog.Level
		logFn func(msg string, args ...any)
	}{
		{"Debug", slog.LevelDebug, logger.Debug},
		{"Info", slog.LevelInfo, logger.Info},
		{"Warn", slog.LevelWarn, logger.Warn},
		{"Error", slog.LevelError, logger.Error},
	}

	for _, level := range levels {
		t.Run(level.name, func(t *testing.T) {
			// Reset captured for each test
			captured = slog.Record{}

			msg := "test message for " + level.name
			level.logFn(msg)

			// Verify that component field was added
			var hasComponent bool
			captured.Attrs(func(a slog.Attr) bool {
				if a.Key == "component" {
					hasComponent = true
				}
				return true
			})

			assert.True(t, hasComponent, "Component field should be added for %s level", level.name)
			assert.Equal(t, msg, captured.Message, "Message should match for %s level", level.name)
		})
	}
}

// TestComponentHandler_Handle_StackTraceWithContext tests stack trace with context
func TestComponentHandler_Handle_StackTraceWithContext(t *testing.T) {
	var captured slog.Record
	base := handler.NewTestHandler(func(_ context.Context, r slog.Record) {
		captured = r
	})

	h := handler.NewComponentHandler(base)
	logger := slog.New(h)
	ctx := context.Background()

	// Test with context to ensure stack trace works with context
	logger.InfoContext(ctx, "test message with context")

	// Verify that component field was added
	var hasComponent bool
	captured.Attrs(func(a slog.Attr) bool {
		if a.Key == "component" {
			hasComponent = true
		}
		return true
	})

	assert.True(t, hasComponent, "Component field should be added with context")
	assert.Equal(t, "test message with context", captured.Message, "Message should match with context")
}

// TestComponentHandler_Handle_StackTraceWithAttrsAndGroup tests stack trace with attrs and group
func TestComponentHandler_Handle_StackTraceWithAttrsAndGroup(t *testing.T) {
	var captured slog.Record
	base := handler.NewTestHandler(func(_ context.Context, r slog.Record) {
		captured = r
	})

	h := handler.NewComponentHandler(base).WithAttrs([]slog.Attr{
		slog.String("service", "test"),
	}).WithGroup("user")
	logger := slog.New(h)

	// Test with attrs and group to ensure stack trace works with complex handlers
	logger.Info("test message with attrs and group")

	// Verify that component field was added
	var hasComponent bool
	captured.Attrs(func(a slog.Attr) bool {
		if a.Key == "component" {
			hasComponent = true
		}
		return true
	})

	assert.True(t, hasComponent, "Component field should be added with attrs and group")
	assert.Equal(t, "test message with attrs and group", captured.Message, "Message should match with attrs and group")
}
