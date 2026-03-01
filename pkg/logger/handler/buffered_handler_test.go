package handler_test

import (
	"context"

	"github.com/getsyntegrity/kit-logger/pkg/logger/handler"

	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBufferedHandler_EnqueuesAndFlushes(t *testing.T) {
	var mu sync.Mutex
	received := make([]string, 0)

	testHandler := handler.NewTestHandler(func(_ context.Context, r slog.Record) {
		mu.Lock()
		defer mu.Unlock()
		received = append(received, r.Message)
	})

	buffered := handler.NewBufferedHandler(testHandler, 10)

	for i := 0; i < 5; i++ {
		r := slog.NewRecord(time.Now(), slog.LevelInfo, "msg-"+string(rune('A'+i)), 0)
		err := buffered.Handle(context.Background(), r)
		require.NoError(t, err)
	}

	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, received, 5)
	require.Equal(t, "msg-A", received[0])
	require.Equal(t, "msg-E", received[4])
}

func TestBufferedHandler_BufferOverflow(t *testing.T) {
	// Slow handler so the buffer fills when we send faster than the worker drains.
	testHandler := handler.NewTestHandler(func(_ context.Context, _ slog.Record) {
		time.Sleep(50 * time.Millisecond)
	})

	buffered := handler.NewBufferedHandler(testHandler, 2)

	var overflowErr error
	for i := 0; i < 20; i++ {
		r := slog.NewRecord(time.Now(), slog.LevelInfo, "msg", 0)
		if err := buffered.Handle(context.Background(), r); err != nil {
			require.ErrorContains(t, err, "buffer full")
			overflowErr = err
			break
		}
	}
	require.Error(t, overflowErr, "expected at least one Handle to return buffer full")
}

func TestBufferedHandler_ShutdownSafe(_ *testing.T) {
	testHandler := handler.NewTestHandler(func(_ context.Context, _ slog.Record) {
		time.Sleep(10 * time.Millisecond)
	})

	buffered := handler.NewBufferedHandler(testHandler, 5)

	for i := 0; i < 5; i++ {
		_ = buffered.Handle(context.Background(), slog.NewRecord(time.Now(), slog.LevelInfo, "msg", 0))
	}

	time.Sleep(200 * time.Millisecond)
}

func TestBufferedHandler_Enabled(t *testing.T) {
	// Create a test handler
	testHandler := handler.NewTestHandler(func(_ context.Context, _ slog.Record) {})
	buffered := handler.NewBufferedHandler(testHandler, 5)

	// Test different log levels
	levels := []slog.Level{slog.LevelDebug, slog.LevelInfo, slog.LevelWarn, slog.LevelError}

	for _, level := range levels {
		// Test that Enabled delegates to the next handler
		enabled := buffered.Enabled(context.Background(), level)
		// TestHandler.Enabled always returns true
		assert.True(t, enabled, "BufferedHandler.Enabled should delegate to next handler")
	}
}

func TestBufferedHandler_WithAttrs(t *testing.T) {
	var receivedAttrs []slog.Attr
	testHandler := handler.NewTestHandler(func(_ context.Context, r slog.Record) {
		r.Attrs(func(a slog.Attr) bool {
			receivedAttrs = append(receivedAttrs, a)
			return true
		})
	})

	buffered := handler.NewBufferedHandler(testHandler, 5)

	// Create a new handler with additional attributes
	attrs := []slog.Attr{
		slog.String("key1", "value1"),
		slog.Int("key2", 42),
		slog.Bool("key3", true),
	}

	newHandler := buffered.WithAttrs(attrs)
	assert.NotNil(t, newHandler)
	assert.IsType(t, &handler.BufferedHandler{}, newHandler)

	// Test that the new handler works correctly
	record := slog.NewRecord(time.Now(), slog.LevelInfo, "test message", 0)
	err := newHandler.Handle(context.Background(), record)
	require.NoError(t, err)

	// Wait for processing
	time.Sleep(100 * time.Millisecond)

	// Verify that the attributes were passed through
	assert.Len(t, receivedAttrs, len(attrs))
	for i, attr := range attrs {
		assert.Equal(t, attr.Key, receivedAttrs[i].Key)
		assert.Equal(t, attr.Value, receivedAttrs[i].Value)
	}
}

func TestBufferedHandler_WithGroup(t *testing.T) {
	var receivedGroups []string
	testHandler := handler.NewTestHandler(func(_ context.Context, r slog.Record) {
		// Track group names by checking the record structure
		// This is a simplified test since we can't easily extract group info from slog.Record
		receivedGroups = append(receivedGroups, "group_test")
	})

	buffered := handler.NewBufferedHandler(testHandler, 5)

	// Create a new handler with a group
	groupName := "test_group"
	newHandler := buffered.WithGroup(groupName)
	assert.NotNil(t, newHandler)
	assert.IsType(t, &handler.BufferedHandler{}, newHandler)

	// Test that the new handler works correctly
	record := slog.NewRecord(time.Now(), slog.LevelInfo, "test message", 0)
	err := newHandler.Handle(context.Background(), record)
	require.NoError(t, err)

	// Wait for processing
	time.Sleep(100 * time.Millisecond)

	// Verify that the handler processed the record
	assert.Len(t, receivedGroups, 1)
}

func TestBufferedHandler_WithAttrsAndGroup_Integration(t *testing.T) {
	var receivedRecords []slog.Record
	testHandler := handler.NewTestHandler(func(_ context.Context, r slog.Record) {
		receivedRecords = append(receivedRecords, r)
	})

	buffered := handler.NewBufferedHandler(testHandler, 10)

	// Create a handler with attributes
	attrs := []slog.Attr{
		slog.String("service", "test-service"),
		slog.Int("version", 1),
	}
	handlerWithAttrs := buffered.WithAttrs(attrs)

	// Create a handler with a group
	handlerWithGroup := handlerWithAttrs.WithGroup("user")

	// Test that the handler works correctly
	record := slog.NewRecord(time.Now(), slog.LevelInfo, "user action", 0)
	err := handlerWithGroup.Handle(context.Background(), record)
	require.NoError(t, err)

	// Wait for processing
	time.Sleep(100 * time.Millisecond)

	// Verify that the record was processed
	assert.Len(t, receivedRecords, 1)
	assert.Equal(t, "user action", receivedRecords[0].Message)
}

func TestBufferedHandler_WithAttrs_EmptyAttrs(t *testing.T) {
	var receivedCount int
	testHandler := handler.NewTestHandler(func(_ context.Context, _ slog.Record) {
		receivedCount++
	})

	buffered := handler.NewBufferedHandler(testHandler, 5)

	// Create a handler with empty attributes
	newHandler := buffered.WithAttrs([]slog.Attr{})
	assert.NotNil(t, newHandler)

	// Test that it still works
	record := slog.NewRecord(time.Now(), slog.LevelInfo, "test", 0)
	err := newHandler.Handle(context.Background(), record)
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)
	assert.Equal(t, 1, receivedCount)
}

func TestBufferedHandler_WithGroup_EmptyGroup(t *testing.T) {
	var receivedCount int
	testHandler := handler.NewTestHandler(func(_ context.Context, _ slog.Record) {
		receivedCount++
	})

	buffered := handler.NewBufferedHandler(testHandler, 5)

	// Create a handler with empty group name
	newHandler := buffered.WithGroup("")
	assert.NotNil(t, newHandler)

	// Test that it still works
	record := slog.NewRecord(time.Now(), slog.LevelInfo, "test", 0)
	err := newHandler.Handle(context.Background(), record)
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)
	assert.Equal(t, 1, receivedCount)
}
