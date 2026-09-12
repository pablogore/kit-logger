package handler_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/pablogore/kit-logger/pkg/logger/handler"
	"github.com/pablogore/kit-logger/pkg/logger/kitlogtest"
	"github.com/pablogore/kit-logger/pkg/logger/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMultiHandler_DelegatesToAllHandlers(t *testing.T) {
	var called1, called2 bool

	handler1 := kitlogtest.NewTestHandler(func(_ context.Context, r slog.Record) {
		called1 = true
		require.Equal(t, "multi test", r.Message)
	})

	handler2 := kitlogtest.NewTestHandler(func(_ context.Context, r slog.Record) {
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
	handler1 := kitlogtest.NewTestHandler(func(_ context.Context, r slog.Record) {
		attrs := utils.ExtractAttrs(r)
		require.Equal(t, "value", attrs["key"])
	})

	handler2 := kitlogtest.NewTestHandler(func(_ context.Context, r slog.Record) {
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
	handler1 := kitlogtest.NewTestHandler(func(_ context.Context, r slog.Record) {
		called1 = true
	})

	boom := errors.New("handler2 boom")
	handler2 := &errorHandler{shouldError: true, err: boom} // This handler will return an error

	handler3 := kitlogtest.NewTestHandler(func(_ context.Context, r slog.Record) {
		called3 = true
	})

	multi := handler.NewMultiHandler(handler1, handler2, handler3)

	// slog.Logger.Info discards the Handler.Handle error entirely, so it
	// cannot prove anything about MultiHandler's own error-aggregation
	// behavior. Call Handle directly to observe what it actually returns.
	err := multi.Handle(context.Background(), slog.NewRecord(time.Now(), slog.LevelInfo, "test with errors", 0))

	require.Error(t, err, "an error from any child must be surfaced, not swallowed")
	assert.ErrorIs(t, err, boom)
	assert.True(t, called1, "handler1 should be called")
	assert.True(t, called3, "handler3 should be called")
}

func TestMultiHandler_Handle_NoErrors(t *testing.T) {
	var called1, called2 bool

	handler1 := kitlogtest.NewTestHandler(func(_ context.Context, r slog.Record) {
		called1 = true
	})

	handler2 := kitlogtest.NewTestHandler(func(_ context.Context, r slog.Record) {
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

	handler1 := kitlogtest.NewTestHandler(func(_ context.Context, r slog.Record) {
		captured1 = r
	})

	handler2 := kitlogtest.NewTestHandler(func(_ context.Context, r slog.Record) {
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

	handler1 := kitlogtest.NewTestHandler(func(_ context.Context, r slog.Record) {
		captured1 = r
	})

	handler2 := kitlogtest.NewTestHandler(func(_ context.Context, r slog.Record) {
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

	handler1 := kitlogtest.NewTestHandler(func(_ context.Context, r slog.Record) {
		captured1 = r
	})

	handler2 := kitlogtest.NewTestHandler(func(_ context.Context, r slog.Record) {
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

	// Verify that attributes are present in both handlers. service/version
	// were added before WithGroup, so they stay top-level; user_id was added
	// after, so a correct WithGroup nests it inside "user".
	attrs1 := utils.ExtractAttrs(captured1)
	attrs2 := utils.ExtractAttrs(captured2)

	assert.Equal(t, "auth", attrs1["service"])
	assert.Equal(t, "auth", attrs2["service"])
	assert.Equal(t, int64(1), attrs1["version"])
	assert.Equal(t, int64(1), attrs2["version"])
	assert.Equal(t, "12345", groupAttr(t, attrs1, "user", "user_id"))
	assert.Equal(t, "12345", groupAttr(t, attrs2, "user", "user_id"))
}

func TestMultiHandler_WithGroup_MultipleHandlers(t *testing.T) {
	var captured1, captured2, captured3 slog.Record

	handler1 := kitlogtest.NewTestHandler(func(_ context.Context, r slog.Record) {
		captured1 = r
	})

	handler2 := kitlogtest.NewTestHandler(func(_ context.Context, r slog.Record) {
		captured2 = r
	})

	handler3 := kitlogtest.NewTestHandler(func(_ context.Context, r slog.Record) {
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

	// Verify that method attribute is present in all handlers, nested inside
	// the "api" group -- it was logged after WithGroup("api").
	attrs1 := utils.ExtractAttrs(captured1)
	attrs2 := utils.ExtractAttrs(captured2)
	attrs3 := utils.ExtractAttrs(captured3)

	assert.Equal(t, "GET", groupAttr(t, attrs1, "api", "method"))
	assert.Equal(t, "GET", groupAttr(t, attrs2, "api", "method"))
	assert.Equal(t, "GET", groupAttr(t, attrs3, "api", "method"))
}

func TestMultiHandler_WithGroup_ContextLogging(t *testing.T) {
	var captured1, captured2 slog.Record

	handler1 := kitlogtest.NewTestHandler(func(_ context.Context, r slog.Record) {
		captured1 = r
	})

	handler2 := kitlogtest.NewTestHandler(func(_ context.Context, r slog.Record) {
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

	// Verify that session_id attribute is present in both handlers, nested
	// inside the "session" group -- it was logged after WithGroup("session").
	attrs1 := utils.ExtractAttrs(captured1)
	attrs2 := utils.ExtractAttrs(captured2)

	assert.Equal(t, "abc123", groupAttr(t, attrs1, "session", "session_id"))
	assert.Equal(t, "abc123", groupAttr(t, attrs2, "session", "session_id"))
}

func TestMultiHandler_Handle_SkipsChildrenNotEnabledForRecordLevel(t *testing.T) {
	handler1 := &multiTestLevelHandler{level: slog.LevelDebug} // enabled for everything below
	handler2 := &multiTestLevelHandler{level: slog.LevelError} // only enabled for Error+

	multi := handler.NewMultiHandler(handler1, handler2)

	logger := slog.New(multi)
	logger.Info("info record")
	logger.Error("error record")

	// handler1 is enabled for both Info and Error; handler2 only for Error.
	assert.Equal(t, 2, handler1.calls, "handler1 should receive every record it is enabled for")
	assert.Equal(t, 1, handler2.calls, "handler2 should only receive the record it is enabled for")
}

func TestMultiHandler_Handle_JoinsAllChildErrors(t *testing.T) {
	err0 := errors.New("child 0 failed")
	err2 := errors.New("child 2 failed")

	handler0 := &errorHandler{shouldError: true, err: err0}
	handler1 := kitlogtest.NewTestHandler(func(context.Context, slog.Record) {})
	handler2 := &errorHandler{shouldError: true, err: err2}

	multi := handler.NewMultiHandler(handler0, handler1, handler2)

	record := slog.NewRecord(time.Now(), slog.LevelInfo, "test", 0)
	err := multi.Handle(context.Background(), record)

	require.Error(t, err)
	assert.ErrorIs(t, err, err0, "the joined error should still satisfy errors.Is for child 0's error")
	assert.ErrorIs(t, err, err2, "the joined error should still satisfy errors.Is for child 2's error")
	assert.Contains(t, err.Error(), "child 0", "the error should name which child failed")
	assert.Contains(t, err.Error(), "child 2", "the error should name which child failed")
}

func TestMultiHandler_Handle_InvocationOrder(t *testing.T) {
	var order []int

	makeHandler := func(i int) slog.Handler {
		return kitlogtest.NewTestHandler(func(context.Context, slog.Record) {
			order = append(order, i)
		})
	}

	multi := handler.NewMultiHandler(makeHandler(0), makeHandler(1), makeHandler(2))

	logger := slog.New(multi)
	logger.Info("order test")

	assert.Equal(t, []int{0, 1, 2}, order, "children must be invoked in construction order")
}

func TestMultiHandler_Handle_ChildMutationDoesNotLeakToSiblings(t *testing.T) {
	// A record needs enough overflow attrs for its unexported back slice to
	// carry spare capacity beyond its length; log/slog reserves only 5 attrs
	// inline (front) before spilling into back. Adding these one at a time
	// mirrors a real pipeline where several handlers each call AddAttrs in
	// turn, which is exactly the pattern that leaves spare capacity behind.
	record := slog.NewRecord(time.Now(), slog.LevelInfo, "mutation isolation test", 0)
	for i := 0; i < 8; i++ {
		record.AddAttrs(slog.Int(fmt.Sprintf("k%d", i), i))
	}

	mutating1 := &mutatingHandler{attrKey: "mutated_by_child_0"}
	mutating2 := &mutatingHandler{attrKey: "mutated_by_child_1"}

	// Both children call Record.AddAttrs, the same way ComponentHandler and
	// other real handlers do. Without a per-child Clone, they would share the
	// record's back backing array: with spare capacity available, the second
	// child's AddAttrs call would write into the same slot the first child
	// just wrote, and log/slog's own runtime detects exactly that aliasing by
	// splicing in a "!BUG" sentinel attr rather than silently corrupting the
	// data — so its presence proves the leak.
	multi := handler.NewMultiHandler(mutating1, mutating2)

	err := multi.Handle(context.Background(), record)
	require.NoError(t, err)

	for _, attrs := range [][]slog.Attr{mutating1.observed, mutating2.observed} {
		for _, a := range attrs {
			assert.NotEqual(t, "!BUG", a.Key,
				"child received a Record aliased with a sibling's: %v", a.Value)
		}
	}
}

// groupAttr looks up a key inside a nested slog group captured by
// utils.ExtractAttrs, whose map values are the raw []slog.Attr of the group.
func groupAttr(t *testing.T, attrs map[string]interface{}, groupKey, attrKey string) any {
	t.Helper()
	group, ok := attrs[groupKey].([]slog.Attr)
	if !ok {
		t.Fatalf("%q is not a captured group", groupKey)
	}
	for _, a := range group {
		if a.Key == attrKey {
			return a.Value.Any()
		}
	}
	t.Fatalf("%q not found inside group %q", attrKey, groupKey)
	return nil
}

// multiTestLevelHandler is a simple handler that filters by level and counts
// how many times Handle was actually invoked.
type multiTestLevelHandler struct {
	level slog.Level
	calls int
}

func (h *multiTestLevelHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return level >= h.level
}

func (h *multiTestLevelHandler) Handle(ctx context.Context, r slog.Record) error {
	h.calls++
	return nil
}

func (h *multiTestLevelHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return h
}

func (h *multiTestLevelHandler) WithGroup(name string) slog.Handler {
	return h
}

// errorHandler is a handler that returns err (or assert.AnError if err is nil)
// whenever shouldError is set.
type errorHandler struct {
	shouldError bool
	err         error
}

func (h *errorHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return true
}

func (h *errorHandler) Handle(ctx context.Context, r slog.Record) error {
	if !h.shouldError {
		return nil
	}
	if h.err != nil {
		return h.err
	}
	return assert.AnError
}

func (h *errorHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return h
}

func (h *errorHandler) WithGroup(name string) slog.Handler {
	return h
}

// mutatingHandler calls Record.AddAttrs on the record it receives, the same
// way ComponentHandler and other real handlers do, and records every attr it
// observed afterwards so a test can check for log/slog's own "!BUG" aliasing
// sentinel (see Record.AddAttrs) surfacing from a sibling's mutation.
type mutatingHandler struct {
	attrKey  string
	observed []slog.Attr
}

func (h *mutatingHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *mutatingHandler) Handle(_ context.Context, r slog.Record) error {
	r.AddAttrs(slog.Bool(h.attrKey, true))
	r.Attrs(func(a slog.Attr) bool {
		h.observed = append(h.observed, a)
		return true
	})
	return nil
}

func (h *mutatingHandler) WithAttrs(attrs []slog.Attr) slog.Handler { return h }
func (h *mutatingHandler) WithGroup(name string) slog.Handler       { return h }
