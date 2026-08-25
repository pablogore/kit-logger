package logger

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/pablogore/kit-logger/pkg/logger/handler"
)

func TestSetGlobal(t *testing.T) {
	// Test setting global logger
	mockLogger := NewMockLogger()
	SetGlobal(mockLogger)

	// Verify global logger is set
	assert.Equal(t, mockLogger, defaultLogger)
}

func TestL_WithNilLogger(t *testing.T) {
	// Reset global logger
	defaultLogger = nil

	// Test L() when defaultLogger is nil
	logger := L()
	assert.NotNil(t, logger)
	assert.IsType(t, &SlogLogger{}, logger)
}

func TestL_WithExistingLogger(t *testing.T) {
	// Set existing logger
	mockLogger := NewMockLogger()
	SetGlobal(mockLogger)

	// Test L() returns existing logger
	logger := L()
	assert.Equal(t, mockLogger, logger)
}

func TestNew_WithMinimalConfig(t *testing.T) {
	cfg := Config{}
	logger := New(cfg)

	assert.NotNil(t, logger)
	assert.IsType(t, &SlogLogger{}, logger)
}

func TestNew_WithJSONFormat(t *testing.T) {
	cfg := Config{
		Level:  "info",
		Format: "json",
	}
	logger := New(cfg)

	assert.NotNil(t, logger)
	assert.IsType(t, &SlogLogger{}, logger)
}

func TestNew_WithTextFormat(t *testing.T) {
	cfg := Config{
		Level:  "debug",
		Format: "text",
	}
	logger := New(cfg)

	assert.NotNil(t, logger)
	assert.IsType(t, &SlogLogger{}, logger)
}

func TestNew_WithFilterRules(t *testing.T) {
	cfg := Config{
		Level: "info",
		FilterRules: []handler.FilterRule{
			{Key: "sensitive", Value: "data"},
		},
	}
	logger := New(cfg)

	assert.NotNil(t, logger)
	assert.IsType(t, &SlogLogger{}, logger)
}

func TestNew_WithGlobalFields(t *testing.T) {
	cfg := Config{
		Level: "warn",
		GlobalFields: map[string]string{
			"service": "test-service",
			"version": "1.0.0",
		},
	}
	logger := New(cfg)

	assert.NotNil(t, logger)
	assert.IsType(t, &SlogLogger{}, logger)
}

func TestNew_WithSamplingEnabled(t *testing.T) {
	cfg := Config{
		Level: "error",
		Sampling: SamplingConfig{
			Enabled:     true,
			Interval:    time.Second,
			Probability: 0.5,
			MinLevel:    "info",
		},
	}
	logger := New(cfg)

	assert.NotNil(t, logger)
	assert.IsType(t, &SlogLogger{}, logger)
}

func TestNew_WithBufferSize(t *testing.T) {
	cfg := Config{
		Level:      "debug",
		BufferSize: 100,
	}
	logger := New(cfg)

	assert.NotNil(t, logger)
	assert.IsType(t, &SlogLogger{}, logger)
}

func TestNew_WithHook(t *testing.T) {
	cfg := Config{
		Level: "info",
		Hook: func(ctx context.Context, r slog.Record) (context.Context, bool) {
			return ctx, true
		},
	}
	logger := New(cfg)

	assert.NotNil(t, logger)
	assert.IsType(t, &SlogLogger{}, logger)
}

func TestNew_WithAllHandlers(t *testing.T) {
	cfg := Config{
		Level:  "debug",
		Format: "json",
		FilterRules: []handler.FilterRule{
			{Key: "sensitive", Value: "data"},
		},
		GlobalFields: map[string]string{
			"service": "test-service",
		},
		Sampling: SamplingConfig{
			Enabled:     true,
			Interval:    time.Millisecond * 100,
			Probability: 0.8,
			MinLevel:    "info",
		},
		BufferSize: 50,
		Hook: func(ctx context.Context, r slog.Record) (context.Context, bool) {
			return ctx, true
		},
	}
	logger := New(cfg)

	assert.NotNil(t, logger)
	assert.IsType(t, &SlogLogger{}, logger)
}

func TestParseLevel_Debug(t *testing.T) {
	level := parseLevel("debug")
	assert.Equal(t, slog.LevelDebug, level)

	level = parseLevel("DEBUG")
	assert.Equal(t, slog.LevelDebug, level)
}

func TestParseLevel_Info(t *testing.T) {
	level := parseLevel("info")
	assert.Equal(t, slog.LevelInfo, level)

	level = parseLevel("INFO")
	assert.Equal(t, slog.LevelInfo, level)
}

func TestParseLevel_Warn(t *testing.T) {
	level := parseLevel("warn")
	assert.Equal(t, slog.LevelWarn, level)

	level = parseLevel("WARN")
	assert.Equal(t, slog.LevelWarn, level)

	level = parseLevel("warning")
	assert.Equal(t, slog.LevelWarn, level)

	level = parseLevel("WARNING")
	assert.Equal(t, slog.LevelWarn, level)
}

func TestParseLevel_Error(t *testing.T) {
	level := parseLevel("error")
	assert.Equal(t, slog.LevelError, level)

	level = parseLevel("ERROR")
	assert.Equal(t, slog.LevelError, level)
}

func TestParseLevel_Default(t *testing.T) {
	// Test unknown level returns info
	level := parseLevel("unknown")
	assert.Equal(t, slog.LevelInfo, level)

	level = parseLevel("")
	assert.Equal(t, slog.LevelInfo, level)

	level = parseLevel("invalid")
	assert.Equal(t, slog.LevelInfo, level)
}

func TestNew_Integration(t *testing.T) {
	// Test that logger can actually log
	cfg := Config{
		Level:  "debug",
		Format: "text",
	}
	logger := New(cfg)

	// Test all log levels
	logger.Debug("debug message")
	logger.Info("info message")
	logger.Warn("warn message")
	logger.Error("error message")

	// Test context logging
	ctx := context.Background()
	logger.DebugContext(ctx, "debug context message")
	logger.InfoContext(ctx, "info context message")
	logger.WarnContext(ctx, "warn context message")
	logger.ErrorContext(ctx, "error context message")

	// Test With method
	loggerWith := logger.With("key", "value")
	loggerWith.Info("message with fields")

	// Test WithContext method
	loggerWithCtx := logger.WithContext(ctx)
	loggerWithCtx.Info("message with context")

	// Test SetLevel
	logger.SetLevel(slog.LevelWarn)

	// Test Sync
	err := logger.Sync()
	assert.NoError(t, err)

	// Test Slog
	slogLogger := logger.Slog()
	assert.NotNil(t, slogLogger)
}

func TestNew_WithCustomOutput(t *testing.T) {
	// Create temporary file for output
	tmpFile, err := os.CreateTemp("", "logger_test")
	require.NoError(t, err)
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	// Redirect stdout to temp file
	oldStdout := os.Stdout
	os.Stdout = tmpFile
	defer func() { os.Stdout = oldStdout }()

	cfg := Config{
		Level:  "info",
		Format: "json",
	}
	logger := New(cfg)

	logger.Info("test message", "key", "value")

	// Verify output was written
	content, err := os.ReadFile(tmpFile.Name())
	require.NoError(t, err)
	assert.Contains(t, string(content), "test message")
	assert.Contains(t, string(content), "key")
	assert.Contains(t, string(content), "value")
}
