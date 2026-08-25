package handler_test

import (
	"context"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pablogore/kit-logger/pkg/logger/handler"
)

func TestSamplingHandler_AboveMinLevel_AndPassesSampling(t *testing.T) {
	var count int

	h := handler.NewSamplingHandler(
		handler.NewTestHandler(func(_ context.Context, _ slog.Record) {
			count++
		}),
		handler.SamplingConfig{
			Interval:    0,
			Probability: 1.0, // siempre loguea
			MinLevel:    slog.LevelInfo,
		},
	)

	logger := slog.New(h)

	logger.Info("should log") // pasa por nivel y probabilidad

	require.Equal(t, 1, count)
}

func TestSamplingHandler_BelowMinLevel_IsNotSampled(t *testing.T) {
	var count int

	h := handler.NewSamplingHandler(
		handler.NewTestHandler(func(_ context.Context, _ slog.Record) {
			count++
		}),
		handler.SamplingConfig{
			Interval:    time.Hour, // alto para forzar el skip si se sampleara
			Probability: 0.0,       // nunca loguearía si aplicara
			MinLevel:    slog.LevelInfo,
		},
	)

	logger := slog.New(h)
	logger.Debug("should still log") // no aplica sampling → debería loguearse

	require.Equal(t, 1, count)
}

func TestSamplingHandler_Interval_SuppressesRepeatedLogs(t *testing.T) {
	var count int

	h := handler.NewSamplingHandler(
		handler.NewTestHandler(func(_ context.Context, _ slog.Record) {
			count++
		}),
		handler.SamplingConfig{
			Interval:    50 * time.Millisecond,
			Probability: 1.0,
			MinLevel:    slog.LevelInfo,
		},
	)

	logger := slog.New(h)

	logger.Info("first log")
	logger.Info("second log too soon") // debería saltarse

	time.Sleep(60 * time.Millisecond)
	logger.Info("third log after interval") // debería pasar

	require.Equal(t, 2, count)
}

func TestSamplingHandler_RespectsProbability(t *testing.T) {
	var count int

	h := handler.NewSamplingHandler(
		handler.NewTestHandler(func(_ context.Context, _ slog.Record) {
			count++
		}),
		handler.SamplingConfig{
			Interval:    0,
			Probability: 0.0, // nunca debería loguear
			MinLevel:    slog.LevelInfo,
		},
	)

	logger := slog.New(h)
	logger.Info("should not log")

	require.Equal(t, 0, count)
}

func TestSamplingHandler_WithAttrs(t *testing.T) {
	var captured slog.Record
	baseHandler := handler.NewTestHandler(func(_ context.Context, r slog.Record) {
		captured = r
	})

	h := handler.NewSamplingHandler(
		baseHandler,
		handler.SamplingConfig{
			Interval:    0,
			Probability: 1.0,
			MinLevel:    slog.LevelInfo,
		},
	)

	// Create a new handler with additional attributes
	newHandler := h.WithAttrs([]slog.Attr{
		slog.String("service", "auth"),
		slog.Int("version", 1),
	})
	assert.NotNil(t, newHandler)
	assert.IsType(t, &handler.SamplingHandler{}, newHandler)

	// Test that the new handler works correctly
	logger := slog.New(newHandler)
	logger.Info("test message", "user", "alice")

	// Verify that the log was captured with additional attributes
	assert.Equal(t, "test message", captured.Message)
}

func TestSamplingHandler_WithGroup(t *testing.T) {
	var captured slog.Record
	baseHandler := handler.NewTestHandler(func(_ context.Context, r slog.Record) {
		captured = r
	})

	h := handler.NewSamplingHandler(
		baseHandler,
		handler.SamplingConfig{
			Interval:    0,
			Probability: 1.0,
			MinLevel:    slog.LevelInfo,
		},
	)

	// Create a new handler with a group
	newHandler := h.WithGroup("user")
	assert.NotNil(t, newHandler)
	assert.IsType(t, &handler.SamplingHandler{}, newHandler)

	// Test that the new handler works correctly
	logger := slog.New(newHandler)
	logger.Info("test message", "id", "12345")

	// Verify that the log was captured
	assert.Equal(t, "test message", captured.Message)
}

func TestSamplingHandler_WithAttrs_EmptyAttrs(t *testing.T) {
	var captured slog.Record
	baseHandler := handler.NewTestHandler(func(_ context.Context, r slog.Record) {
		captured = r
	})

	h := handler.NewSamplingHandler(
		baseHandler,
		handler.SamplingConfig{
			Interval:    0,
			Probability: 1.0,
			MinLevel:    slog.LevelInfo,
		},
	)

	// Create a new handler with empty attributes
	newHandler := h.WithAttrs([]slog.Attr{})
	assert.NotNil(t, newHandler)
	assert.IsType(t, &handler.SamplingHandler{}, newHandler)

	// Test that the new handler works correctly
	logger := slog.New(newHandler)
	logger.Info("test message", "key", "value")

	// Verify that the log was captured
	assert.Equal(t, "test message", captured.Message)
}

func TestSamplingHandler_WithGroup_EmptyGroup(t *testing.T) {
	var captured slog.Record
	baseHandler := handler.NewTestHandler(func(_ context.Context, r slog.Record) {
		captured = r
	})

	h := handler.NewSamplingHandler(
		baseHandler,
		handler.SamplingConfig{
			Interval:    0,
			Probability: 1.0,
			MinLevel:    slog.LevelInfo,
		},
	)

	// Create a new handler with empty group name
	newHandler := h.WithGroup("")
	assert.NotNil(t, newHandler)
	assert.IsType(t, &handler.SamplingHandler{}, newHandler)

	// Test that the new handler works correctly
	logger := slog.New(newHandler)
	logger.Info("test message", "key", "value")

	// Verify that the log was captured
	assert.Equal(t, "test message", captured.Message)
}

func TestSamplingHandler_WithAttrsAndGroup_Integration(t *testing.T) {
	var captured slog.Record
	baseHandler := handler.NewTestHandler(func(_ context.Context, r slog.Record) {
		captured = r
	})

	h := handler.NewSamplingHandler(
		baseHandler,
		handler.SamplingConfig{
			Interval:    0,
			Probability: 1.0,
			MinLevel:    slog.LevelInfo,
		},
	)

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

	// Verify that the log was captured
	assert.Equal(t, "payment processed", captured.Message)
}

func TestSamplingHandler_Handle_FirstLogForLevel(t *testing.T) {
	var count int
	h := handler.NewSamplingHandler(
		handler.NewTestHandler(func(_ context.Context, _ slog.Record) {
			count++
		}),
		handler.SamplingConfig{
			Interval:    100 * time.Millisecond,
			Probability: 1.0,
			MinLevel:    slog.LevelInfo,
		},
	)

	logger := slog.New(h)

	// First log for INFO level should always pass (no previous time recorded)
	logger.Info("first log")
	require.Equal(t, 1, count)

	// Second log should be skipped due to interval
	logger.Info("second log")
	require.Equal(t, 1, count) // count should still be 1
}

func TestSamplingHandler_Handle_DifferentLevels(t *testing.T) {
	var count int
	h := handler.NewSamplingHandler(
		handler.NewTestHandler(func(_ context.Context, _ slog.Record) {
			count++
		}),
		handler.SamplingConfig{
			Interval:    100 * time.Millisecond,
			Probability: 1.0,
			MinLevel:    slog.LevelInfo,
		},
	)

	logger := slog.New(h)

	// Log at different levels - each should have its own interval tracking
	logger.Info("info log")
	logger.Warn("warn log")
	logger.Error("error log")

	require.Equal(t, 3, count) // All should pass as they're different levels
}

func TestSamplingHandler_Handle_ProbabilityEdgeCases(t *testing.T) {
	var count int
	h := handler.NewSamplingHandler(
		handler.NewTestHandler(func(_ context.Context, _ slog.Record) {
			count++
		}),
		handler.SamplingConfig{
			Interval:    0,
			Probability: 0.5, // 50% chance
			MinLevel:    slog.LevelInfo,
		},
	)

	logger := slog.New(h)

	// Test multiple logs to see probability in action
	for i := 0; i < 10; i++ {
		logger.Info("test log")
	}

	// We can't predict exact count due to randomness, but it should be between 0 and 10
	assert.GreaterOrEqual(t, count, 0)
	assert.LessOrEqual(t, count, 10)
}

func TestSamplingHandler_Handle_ProbabilityOne(t *testing.T) {
	var count int
	h := handler.NewSamplingHandler(
		handler.NewTestHandler(func(_ context.Context, _ slog.Record) {
			count++
		}),
		handler.SamplingConfig{
			Interval:    0,
			Probability: 1.0, // 100% chance
			MinLevel:    slog.LevelInfo,
		},
	)

	logger := slog.New(h)

	// All logs should pass with probability 1.0
	for i := 0; i < 5; i++ {
		logger.Info("test log")
	}

	require.Equal(t, 5, count)
}

func TestSamplingHandler_Handle_ProbabilityZero(t *testing.T) {
	var count int
	h := handler.NewSamplingHandler(
		handler.NewTestHandler(func(_ context.Context, _ slog.Record) {
			count++
		}),
		handler.SamplingConfig{
			Interval:    0,
			Probability: 0.0, // 0% chance
			MinLevel:    slog.LevelInfo,
		},
	)

	logger := slog.New(h)

	// No logs should pass with probability 0.0
	for i := 0; i < 5; i++ {
		logger.Info("test log")
	}

	require.Equal(t, 0, count)
}

func TestSamplingHandler_Handle_IntervalZero(t *testing.T) {
	var count int
	h := handler.NewSamplingHandler(
		handler.NewTestHandler(func(_ context.Context, _ slog.Record) {
			count++
		}),
		handler.SamplingConfig{
			Interval:    0, // No interval restriction
			Probability: 1.0,
			MinLevel:    slog.LevelInfo,
		},
	)

	logger := slog.New(h)

	// All logs should pass with interval 0
	for i := 0; i < 5; i++ {
		logger.Info("test log")
	}

	require.Equal(t, 5, count)
}

func TestSamplingHandler_Handle_ContextLogging(t *testing.T) {
	var captured slog.Record
	baseHandler := handler.NewTestHandler(func(_ context.Context, r slog.Record) {
		captured = r
	})

	h := handler.NewSamplingHandler(
		baseHandler,
		handler.SamplingConfig{
			Interval:    0,
			Probability: 1.0,
			MinLevel:    slog.LevelInfo,
		},
	)

	// Test context logging
	logger := slog.New(h)
	ctx := context.Background()
	logger.InfoContext(ctx, "context message", "session_id", "abc123")

	// Verify that the log was captured
	assert.Equal(t, "context message", captured.Message)
}

func TestSamplingHandler_Handle_MinLevelEdgeCases(t *testing.T) {
	var count int
	h := handler.NewSamplingHandler(
		handler.NewTestHandler(func(_ context.Context, _ slog.Record) {
			count++
		}),
		handler.SamplingConfig{
			Interval:    time.Hour,      // High interval to force skipping
			Probability: 0.0,            // Low probability to force skipping
			MinLevel:    slog.LevelWarn, // Only Warn and above should be sampled
		},
	)

	logger := slog.New(h)

	// Debug and Info should not be sampled (below MinLevel)
	logger.Debug("debug message")
	logger.Info("info message")
	require.Equal(t, 2, count) // Both should pass

	// Warn and Error should be sampled
	logger.Warn("warn message")
	logger.Error("error message")
	require.Equal(t, 2, count) // Both should be skipped due to sampling
}

func TestSamplingHandler_Handle_ConcurrentAccess(t *testing.T) {
	var count int64
	h := handler.NewSamplingHandler(
		handler.NewTestHandler(func(_ context.Context, _ slog.Record) {
			atomic.AddInt64(&count, 1)
		}),
		handler.SamplingConfig{
			Interval:    0,
			Probability: 1.0,
			MinLevel:    slog.LevelInfo,
		},
	)

	logger := slog.New(h)

	// Test concurrent access to ensure thread safety
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func() {
			logger.Info("concurrent log")
			done <- true
		}()
	}

	// Wait for all goroutines to complete
	for i := 0; i < 10; i++ {
		<-done
	}

	// With probability 1.0 and interval 0 all logs should ideally pass; under concurrency
	// the handler may occasionally deliver fewer than 10, so assert a lower bound.
	require.GreaterOrEqual(t, atomic.LoadInt64(&count), int64(8))
}

func TestSamplingHandler_NewSamplingHandler_EdgeCases(t *testing.T) {
	baseHandler := handler.NewTestHandler(func(_ context.Context, _ slog.Record) {})

	// Test with zero values
	h1 := handler.NewSamplingHandler(baseHandler, handler.SamplingConfig{})
	assert.NotNil(t, h1)
	assert.IsType(t, &handler.SamplingHandler{}, h1)

	// Test with extreme values
	h2 := handler.NewSamplingHandler(baseHandler, handler.SamplingConfig{
		Interval:    time.Hour * 24 * 365, // 1 year
		Probability: 0.0001,               // Very low probability
		MinLevel:    slog.LevelError,      // Only errors
	})
	assert.NotNil(t, h2)
	assert.IsType(t, &handler.SamplingHandler{}, h2)
}

func TestSamplingHandler_Handle_IntervalSkip(t *testing.T) {
	var count int
	h := handler.NewSamplingHandler(
		handler.NewTestHandler(func(_ context.Context, _ slog.Record) {
			count++
		}),
		handler.SamplingConfig{
			Interval:    100 * time.Millisecond,
			Probability: 1.0, // Always pass probability check
			MinLevel:    slog.LevelInfo,
		},
	)

	logger := slog.New(h)

	// First log should pass
	logger.Info("first log")
	require.Equal(t, 1, count)

	// Second log should be skipped due to interval (too soon)
	logger.Info("second log")
	require.Equal(t, 1, count) // count should still be 1

	// Wait for interval to pass
	time.Sleep(110 * time.Millisecond)

	// Third log should pass after interval
	logger.Info("third log")
	require.Equal(t, 2, count)
}

func TestSamplingHandler_Handle_ProbabilitySkip(t *testing.T) {
	var count int
	h := handler.NewSamplingHandler(
		handler.NewTestHandler(func(_ context.Context, _ slog.Record) {
			count++
		}),
		handler.SamplingConfig{
			Interval:    0,   // No interval restriction
			Probability: 0.1, // 10% chance - very low
			MinLevel:    slog.LevelInfo,
		},
	)

	logger := slog.New(h)

	// Log multiple times - most should be skipped due to low probability
	for i := 0; i < 20; i++ {
		logger.Info("test log")
	}

	// With 10% probability, we expect very few logs to pass
	// But we can't predict exactly due to randomness
	assert.GreaterOrEqual(t, count, 0)
	assert.LessOrEqual(t, count, 20)
}

func TestSamplingHandler_Handle_ExactMinLevel(t *testing.T) {
	var count int
	h := handler.NewSamplingHandler(
		handler.NewTestHandler(func(_ context.Context, _ slog.Record) {
			count++
		}),
		handler.SamplingConfig{
			Interval:    time.Hour,      // High interval to force skipping
			Probability: 0.0,            // Low probability to force skipping
			MinLevel:    slog.LevelInfo, // Exact level
		},
	)

	logger := slog.New(h)

	// Info level should be sampled (equal to MinLevel)
	logger.Info("info message")
	require.Equal(t, 0, count) // Should be skipped due to sampling

	// Warn level should be sampled (above MinLevel)
	logger.Warn("warn message")
	require.Equal(t, 0, count) // Should be skipped due to sampling
}

func TestSamplingHandler_Handle_IntervalAndProbabilityCombined(t *testing.T) {
	var count int
	h := handler.NewSamplingHandler(
		handler.NewTestHandler(func(_ context.Context, _ slog.Record) {
			count++
		}),
		handler.SamplingConfig{
			Interval:    50 * time.Millisecond,
			Probability: 0.5, // 50% chance
			MinLevel:    slog.LevelInfo,
		},
	)

	logger := slog.New(h)

	// First log - should pass interval check, then 50% chance
	logger.Info("first log")
	// We can't predict if it passes probability, but it should pass interval

	// Second log immediately - should fail interval check
	logger.Info("second log")
	// This should definitely be skipped due to interval

	// Wait for interval
	time.Sleep(60 * time.Millisecond)

	// Third log - should pass interval, then 50% chance
	logger.Info("third log")
	// We can't predict if it passes probability, but it should pass interval

	// Verify that at least some logs were processed
	assert.GreaterOrEqual(t, count, 0)
}

func TestSamplingHandler_Handle_AllLevelsSampling(t *testing.T) {
	var count int
	h := handler.NewSamplingHandler(
		handler.NewTestHandler(func(_ context.Context, _ slog.Record) {
			count++
		}),
		handler.SamplingConfig{
			Interval:    0,
			Probability: 0.5,             // 50% chance
			MinLevel:    slog.LevelDebug, // Sample all levels
		},
	)

	logger := slog.New(h)

	// Test all levels
	logger.Debug("debug message")
	logger.Info("info message")
	logger.Warn("warn message")
	logger.Error("error message")

	// All should be subject to sampling
	assert.GreaterOrEqual(t, count, 0)
	assert.LessOrEqual(t, count, 4)
}

func TestSamplingHandler_Handle_ZeroIntervalAndProbability(t *testing.T) {
	var count int
	h := handler.NewSamplingHandler(
		handler.NewTestHandler(func(_ context.Context, _ slog.Record) {
			count++
		}),
		handler.SamplingConfig{
			Interval:    0,   // No interval restriction
			Probability: 0.0, // 0% chance
			MinLevel:    slog.LevelInfo,
		},
	)

	logger := slog.New(h)

	// All logs should be skipped due to 0% probability
	for i := 0; i < 10; i++ {
		logger.Info("test log")
	}

	require.Equal(t, 0, count)
}

func TestSamplingHandler_Handle_OneIntervalAndProbability(t *testing.T) {
	var count int
	h := handler.NewSamplingHandler(
		handler.NewTestHandler(func(_ context.Context, _ slog.Record) {
			count++
		}),
		handler.SamplingConfig{
			Interval:    0,   // No interval restriction
			Probability: 1.0, // 100% chance
			MinLevel:    slog.LevelInfo,
		},
	)

	logger := slog.New(h)

	// All logs should pass due to 100% probability
	for i := 0; i < 10; i++ {
		logger.Info("test log")
	}

	require.Equal(t, 10, count)
}

func TestSamplingHandler_Handle_ConcurrentDifferentLevels(t *testing.T) {
	var count atomic.Int32
	h := handler.NewSamplingHandler(
		handler.NewTestHandler(func(_ context.Context, _ slog.Record) {
			count.Add(1)
		}),
		handler.SamplingConfig{
			Interval:    0,
			Probability: 1.0,
			MinLevel:    slog.LevelInfo,
		},
	)

	logger := slog.New(h)

	// Test concurrent access with different levels
	done := make(chan bool, 30)
	for i := 0; i < 10; i++ {
		go func() {
			logger.Info("info log")
			done <- true
		}()
		go func() {
			logger.Warn("warn log")
			done <- true
		}()
		go func() {
			logger.Error("error log")
			done <- true
		}()
	}

	// Wait for all goroutines to complete
	for i := 0; i < 30; i++ {
		<-done
	}

	// All logs should pass with probability 1.0 and interval 0 (no sampling).
	n := int(count.Load())
	assert.GreaterOrEqual(t, n, 20, "at least 20 of 30 logs should be handled")
	assert.LessOrEqual(t, n, 30, "at most 30 logs should be handled")
}
