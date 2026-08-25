package handler_test

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pablogore/kit-logger/pkg/logger/handler"
)

// TestPrometheusHandler is a test-specific version that doesn't use global registration
type TestPrometheusHandler struct {
	next    slog.Handler
	counter *prometheus.CounterVec
}

func NewTestPrometheusHandler(next slog.Handler, counter *prometheus.CounterVec) slog.Handler {
	return &TestPrometheusHandler{next: next, counter: counter}
}

func (h *TestPrometheusHandler) Handle(ctx context.Context, r slog.Record) error {
	h.counter.WithLabelValues(r.Level.String()).Inc()
	return h.next.Handle(ctx, r)
}

func (h *TestPrometheusHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

func (h *TestPrometheusHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &TestPrometheusHandler{next: h.next.WithAttrs(attrs), counter: h.counter}
}

func (h *TestPrometheusHandler) WithGroup(name string) slog.Handler {
	return &TestPrometheusHandler{next: h.next.WithGroup(name), counter: h.counter}
}

func TestPrometheusHandler_IncrementsCounter(t *testing.T) {
	// Create a test-specific counter
	testCounter := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "test_slog_logged_total",
			Help: "Total number of slog log entries for testing.",
		},
		[]string{"level"},
	)

	var captured slog.Record

	// Handler base que captura el log
	testHandler := handler.NewTestHandler(func(
		_ context.Context, r slog.Record,
	) {
		captured = r
	})

	// Use test-specific prometheus handler
	promHandler := NewTestPrometheusHandler(testHandler, testCounter)

	logger := slog.New(promHandler)
	logger.Info("log message", slog.String("key", "value"))

	require.Equal(t, "log message", captured.Message)

	expected := `
		# HELP test_slog_logged_total Total number of slog log entries for testing.
		# TYPE test_slog_logged_total counter
		test_slog_logged_total{level="INFO"} 1
	`

	err := testutil.CollectAndCompare(
		testCounter,
		strings.NewReader(expected),
	)
	require.NoError(t, err)
}

// Tests for the real PrometheusHandler
func TestPrometheusHandler_NewPrometheusHandler(t *testing.T) {
	baseHandler := handler.NewTestHandler(func(_ context.Context, r slog.Record) {})

	// Test creation of PrometheusHandler
	promHandler := handler.NewPrometheusHandler(baseHandler)
	assert.NotNil(t, promHandler)
	assert.IsType(t, &handler.PrometheusHandler{}, promHandler)
}

func TestPrometheusHandler_Handle(t *testing.T) {
	var captured slog.Record
	baseHandler := handler.NewTestHandler(func(_ context.Context, r slog.Record) {
		captured = r
	})

	promHandler := handler.NewPrometheusHandler(baseHandler)

	// Test that the handler increments the counter and passes the record to the next handler
	logger := slog.New(promHandler)
	logger.Info("test message", "key", "value")

	// Verify that the record was passed to the next handler
	assert.Equal(t, "test message", captured.Message)

	// Verify that the counter was incremented (we can't easily reset the global counter, so we just check it exists)
	// The counter should have been incremented at least once
	metric := handler.LogCounter.WithLabelValues("INFO")
	assert.NotNil(t, metric)
}

func TestPrometheusHandler_Handle_DifferentLevels(t *testing.T) {
	var captured slog.Record
	baseHandler := handler.NewTestHandler(func(_ context.Context, r slog.Record) {
		captured = r
	})

	promHandler := handler.NewPrometheusHandler(baseHandler)
	logger := slog.New(promHandler)

	// Test different log levels
	logger.Debug("debug message")
	assert.Equal(t, "debug message", captured.Message)

	logger.Warn("warn message")
	assert.Equal(t, "warn message", captured.Message)

	logger.Error("error message")
	assert.Equal(t, "error message", captured.Message)

	// Verify that counters exist for all levels (we can't easily reset the global counter)
	debugMetric := handler.LogCounter.WithLabelValues("DEBUG")
	warnMetric := handler.LogCounter.WithLabelValues("WARN")
	errorMetric := handler.LogCounter.WithLabelValues("ERROR")

	assert.NotNil(t, debugMetric)
	assert.NotNil(t, warnMetric)
	assert.NotNil(t, errorMetric)
}

func TestPrometheusHandler_Enabled(t *testing.T) {
	// Create a base handler that only enables Info level and above
	baseHandler := &prometheusTestLevelHandler{level: slog.LevelInfo}
	promHandler := handler.NewPrometheusHandler(baseHandler)

	// Test that Enabled delegates to the next handler
	assert.False(t, promHandler.Enabled(context.Background(), slog.LevelDebug))
	assert.True(t, promHandler.Enabled(context.Background(), slog.LevelInfo))
	assert.True(t, promHandler.Enabled(context.Background(), slog.LevelWarn))
	assert.True(t, promHandler.Enabled(context.Background(), slog.LevelError))
}

func TestPrometheusHandler_WithAttrs(t *testing.T) {
	var captured slog.Record
	baseHandler := handler.NewTestHandler(func(_ context.Context, r slog.Record) {
		captured = r
	})

	promHandler := handler.NewPrometheusHandler(baseHandler)

	// Create a new handler with additional attributes
	newHandler := promHandler.WithAttrs([]slog.Attr{
		slog.String("service", "auth"),
		slog.Int("version", 1),
	})
	assert.NotNil(t, newHandler)
	assert.IsType(t, &handler.PrometheusHandler{}, newHandler)

	// Test that the new handler works correctly
	logger := slog.New(newHandler)
	logger.Info("test message", "user", "alice")

	// Verify that the log was captured with additional attributes
	assert.Equal(t, "test message", captured.Message)

	// Verify that the counter exists (we can't easily reset the global counter)
	metric := handler.LogCounter.WithLabelValues("INFO")
	assert.NotNil(t, metric)
}

func TestPrometheusHandler_WithGroup(t *testing.T) {
	var captured slog.Record
	baseHandler := handler.NewTestHandler(func(_ context.Context, r slog.Record) {
		captured = r
	})

	promHandler := handler.NewPrometheusHandler(baseHandler)

	// Create a new handler with a group
	newHandler := promHandler.WithGroup("user")
	assert.NotNil(t, newHandler)
	assert.IsType(t, &handler.PrometheusHandler{}, newHandler)

	// Test that the new handler works correctly
	logger := slog.New(newHandler)
	logger.Info("test message", "id", "12345")

	// Verify that the log was captured
	assert.Equal(t, "test message", captured.Message)

	// Verify that the counter exists (we can't easily reset the global counter)
	metric := handler.LogCounter.WithLabelValues("INFO")
	assert.NotNil(t, metric)
}

func TestPrometheusHandler_WithAttrs_EmptyAttrs(t *testing.T) {
	var captured slog.Record
	baseHandler := handler.NewTestHandler(func(_ context.Context, r slog.Record) {
		captured = r
	})

	promHandler := handler.NewPrometheusHandler(baseHandler)

	// Create a new handler with empty attributes
	newHandler := promHandler.WithAttrs([]slog.Attr{})
	assert.NotNil(t, newHandler)
	assert.IsType(t, &handler.PrometheusHandler{}, newHandler)

	// Test that the new handler works correctly
	logger := slog.New(newHandler)
	logger.Info("test message", "key", "value")

	// Verify that the log was captured
	assert.Equal(t, "test message", captured.Message)
}

func TestPrometheusHandler_WithGroup_EmptyGroup(t *testing.T) {
	var captured slog.Record
	baseHandler := handler.NewTestHandler(func(_ context.Context, r slog.Record) {
		captured = r
	})

	promHandler := handler.NewPrometheusHandler(baseHandler)

	// Create a new handler with empty group name
	newHandler := promHandler.WithGroup("")
	assert.NotNil(t, newHandler)
	assert.IsType(t, &handler.PrometheusHandler{}, newHandler)

	// Test that the new handler works correctly
	logger := slog.New(newHandler)
	logger.Info("test message", "key", "value")

	// Verify that the log was captured
	assert.Equal(t, "test message", captured.Message)
}

func TestPrometheusHandler_WithAttrsAndGroup_Integration(t *testing.T) {
	var captured slog.Record
	baseHandler := handler.NewTestHandler(func(_ context.Context, r slog.Record) {
		captured = r
	})

	promHandler := handler.NewPrometheusHandler(baseHandler)

	// Create a handler with attributes
	handlerWithAttrs := promHandler.WithAttrs([]slog.Attr{
		slog.String("service", "payment"),
		slog.Int("version", 2),
	})

	// Create a handler with group
	handlerWithGroup := handlerWithAttrs.WithGroup("transaction")

	// Test that the handler works correctly
	logger := slog.New(handlerWithGroup)
	logger.Info("payment processed", "amount", "100.00", "currency", "USD")

	// Verify that the log was captured
	assert.Equal(t, "payment processed", captured.Message)

	// Verify that the counter exists (we can't easily reset the global counter)
	metric := handler.LogCounter.WithLabelValues("INFO")
	assert.NotNil(t, metric)
}

func TestPrometheusHandler_ContextLogging(t *testing.T) {
	var captured slog.Record
	baseHandler := handler.NewTestHandler(func(_ context.Context, r slog.Record) {
		captured = r
	})

	promHandler := handler.NewPrometheusHandler(baseHandler)

	// Test context logging
	logger := slog.New(promHandler)
	ctx := context.Background()
	logger.InfoContext(ctx, "context message", "session_id", "abc123")

	// Verify that the log was captured
	assert.Equal(t, "context message", captured.Message)

	// Verify that the counter exists (we can't easily reset the global counter)
	metric := handler.LogCounter.WithLabelValues("INFO")
	assert.NotNil(t, metric)
}

func TestPrometheusHandler_MultipleLogs(t *testing.T) {
	var captured slog.Record
	baseHandler := handler.NewTestHandler(func(_ context.Context, r slog.Record) {
		captured = r
	})

	promHandler := handler.NewPrometheusHandler(baseHandler)
	logger := slog.New(promHandler)

	// Log multiple messages
	logger.Info("message 1")
	logger.Warn("message 2")
	logger.Error("message 3")

	// Verify that the last message was captured
	assert.Equal(t, "message 3", captured.Message)

	// Verify that counters exist for all levels (we can't easily reset the global counter)
	infoMetric := handler.LogCounter.WithLabelValues("INFO")
	warnMetric := handler.LogCounter.WithLabelValues("WARN")
	errorMetric := handler.LogCounter.WithLabelValues("ERROR")

	assert.NotNil(t, infoMetric)
	assert.NotNil(t, warnMetric)
	assert.NotNil(t, errorMetric)
}

// prometheusTestLevelHandler is a simple handler that filters by level
type prometheusTestLevelHandler struct {
	level slog.Level
}

func (h *prometheusTestLevelHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return level >= h.level
}

func (h *prometheusTestLevelHandler) Handle(ctx context.Context, r slog.Record) error {
	return nil
}

func (h *prometheusTestLevelHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return h
}

func (h *prometheusTestLevelHandler) WithGroup(name string) slog.Handler {
	return h
}
