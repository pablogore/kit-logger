package logger

import (
	"context"
	"log/slog"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSlogLogger_Debug(t *testing.T) {
	logger := &SlogLogger{
		logger:   slog.New(slog.NewTextHandler(os.Stdout, nil)),
		levelVar: new(slog.LevelVar),
	}

	// Test that Debug doesn't panic
	assert.NotPanics(t, func() {
		logger.Debug("debug message")
	})
}

func TestSlogLogger_Info(t *testing.T) {
	logger := &SlogLogger{
		logger:   slog.New(slog.NewTextHandler(os.Stdout, nil)),
		levelVar: new(slog.LevelVar),
	}

	// Test that Info doesn't panic
	assert.NotPanics(t, func() {
		logger.Info("info message")
	})
}

func TestSlogLogger_Warn(t *testing.T) {
	logger := &SlogLogger{
		logger:   slog.New(slog.NewTextHandler(os.Stdout, nil)),
		levelVar: new(slog.LevelVar),
	}

	// Test that Warn doesn't panic
	assert.NotPanics(t, func() {
		logger.Warn("warn message")
	})
}

func TestSlogLogger_Error(t *testing.T) {
	logger := &SlogLogger{
		logger:   slog.New(slog.NewTextHandler(os.Stdout, nil)),
		levelVar: new(slog.LevelVar),
	}

	// Test that Error doesn't panic
	assert.NotPanics(t, func() {
		logger.Error("error message")
	})
}

func TestSlogLogger_DebugContext(t *testing.T) {
	logger := &SlogLogger{
		logger:   slog.New(slog.NewTextHandler(os.Stdout, nil)),
		levelVar: new(slog.LevelVar),
	}

	ctx := context.Background()

	// Test that DebugContext doesn't panic
	assert.NotPanics(t, func() {
		logger.DebugContext(ctx, "debug context message")
	})
}

func TestSlogLogger_InfoContext(t *testing.T) {
	logger := &SlogLogger{
		logger:   slog.New(slog.NewTextHandler(os.Stdout, nil)),
		levelVar: new(slog.LevelVar),
	}

	ctx := context.Background()

	// Test that InfoContext doesn't panic
	assert.NotPanics(t, func() {
		logger.InfoContext(ctx, "info context message")
	})
}

func TestSlogLogger_WarnContext(t *testing.T) {
	logger := &SlogLogger{
		logger:   slog.New(slog.NewTextHandler(os.Stdout, nil)),
		levelVar: new(slog.LevelVar),
	}

	ctx := context.Background()

	// Test that WarnContext doesn't panic
	assert.NotPanics(t, func() {
		logger.WarnContext(ctx, "warn context message")
	})
}

func TestSlogLogger_ErrorContext(t *testing.T) {
	logger := &SlogLogger{
		logger:   slog.New(slog.NewTextHandler(os.Stdout, nil)),
		levelVar: new(slog.LevelVar),
	}

	ctx := context.Background()

	// Test that ErrorContext doesn't panic
	assert.NotPanics(t, func() {
		logger.ErrorContext(ctx, "error context message")
	})
}

func TestSlogLogger_Log(t *testing.T) {
	logger := &SlogLogger{
		logger:   slog.New(slog.NewTextHandler(os.Stdout, nil)),
		levelVar: new(slog.LevelVar),
	}

	ctx := context.Background()

	// Test that Log doesn't panic
	assert.NotPanics(t, func() {
		logger.Log(ctx, slog.LevelInfo, "log message")
	})

	// Test with different levels
	assert.NotPanics(t, func() {
		logger.Log(ctx, slog.LevelDebug, "debug log")
		logger.Log(ctx, slog.LevelWarn, "warn log")
		logger.Log(ctx, slog.LevelError, "error log")
	})
}

func TestSlogLogger_With(t *testing.T) {
	logger := &SlogLogger{
		logger:   slog.New(slog.NewTextHandler(os.Stdout, nil)),
		levelVar: new(slog.LevelVar),
	}

	// Test With method
	newLogger := logger.With("key", "value", "another", 42)

	// Verify it returns a new SlogLogger
	assert.IsType(t, &SlogLogger{}, newLogger)
	assert.NotEqual(t, logger, newLogger)

	// Test that the new logger can be used
	assert.NotPanics(t, func() {
		newLogger.Info("message with fields")
	})
}

func TestSlogLogger_WithContext_WithExtractor(t *testing.T) {
	logger := &SlogLogger{
		logger:   slog.New(slog.NewTextHandler(os.Stdout, nil)),
		levelVar: new(slog.LevelVar),
	}

	// Set up context field extractor
	SetContextFieldExtractor(func(ctx context.Context) []any {
		if userID, ok := ctx.Value("user_id").(string); ok {
			return []any{"user_id", userID}
		}
		return nil
	})

	// Test with context that has user_id
	ctx := context.WithValue(context.Background(), "user_id", "12345")
	newLogger := logger.WithContext(ctx)

	// Verify it returns a new SlogLogger
	assert.IsType(t, &SlogLogger{}, newLogger)
	assert.NotEqual(t, logger, newLogger)

	// Test that the new logger can be used
	assert.NotPanics(t, func() {
		newLogger.Info("message with context")
	})
}

func TestSlogLogger_WithContext_WithoutExtractor(t *testing.T) {
	logger := &SlogLogger{
		logger:   slog.New(slog.NewTextHandler(os.Stdout, nil)),
		levelVar: new(slog.LevelVar),
	}

	// Clear context field extractor
	SetContextFieldExtractor(nil)

	// Test with context
	ctx := context.Background()
	newLogger := logger.WithContext(ctx)

	// Should return the same logger when no extractor is set
	assert.Equal(t, logger, newLogger)
}

func TestSlogLogger_WithContext_WithNilExtractor(t *testing.T) {
	logger := &SlogLogger{
		logger:   slog.New(slog.NewTextHandler(os.Stdout, nil)),
		levelVar: new(slog.LevelVar),
	}

	// Set nil extractor
	SetContextFieldExtractor(nil)

	// Test with context
	ctx := context.Background()
	newLogger := logger.WithContext(ctx)

	// Should return the same logger
	assert.Equal(t, logger, newLogger)
}

func TestSlogLogger_SetLevel(t *testing.T) {
	levelVar := new(slog.LevelVar)
	logger := &SlogLogger{
		logger:   slog.New(slog.NewTextHandler(os.Stdout, nil)),
		levelVar: levelVar,
	}

	// Test setting different levels
	testCases := []slog.Level{
		slog.LevelDebug,
		slog.LevelInfo,
		slog.LevelWarn,
		slog.LevelError,
	}

	for _, level := range testCases {
		logger.SetLevel(level)
		assert.Equal(t, level, levelVar.Level())
	}
}

func TestSlogLogger_SetLevel_NilLevelVar(t *testing.T) {
	logger := &SlogLogger{
		logger:   slog.New(slog.NewTextHandler(os.Stdout, nil)),
		levelVar: nil,
	}

	// Test that SetLevel doesn't panic when levelVar is nil
	assert.NotPanics(t, func() {
		logger.SetLevel(slog.LevelInfo)
	})
}

func TestSlogLogger_Sync(t *testing.T) {
	logger := &SlogLogger{
		logger:   slog.New(slog.NewTextHandler(os.Stdout, nil)),
		levelVar: new(slog.LevelVar),
	}

	// Test that Sync doesn't panic and returns no error
	err := logger.Sync()
	assert.NoError(t, err)
}

func TestSlogLogger_Sync_WithFlusher(t *testing.T) {
	// The lifecycle is captured when New assembles the pipeline, so the
	// flushable handler has to be reached through New, not by assigning a
	// handler to a hand-built SlogLogger.
	flushHandler := &flushableHandler{}
	logger := New(Config{Handler: flushHandler})

	err := logger.Sync()
	assert.NoError(t, err)
	assert.True(t, flushHandler.flushed)
}

func TestSlogLogger_Slog(t *testing.T) {
	slogLogger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	logger := &SlogLogger{
		logger:   slogLogger,
		levelVar: new(slog.LevelVar),
	}

	// Test that Slog returns the underlying slog.Logger
	result := logger.Slog()
	assert.Equal(t, slogLogger, result)
}

func TestSlogLogger_Integration(t *testing.T) {
	// Create a complete logger
	logger := &SlogLogger{
		logger:   slog.New(slog.NewTextHandler(os.Stdout, nil)),
		levelVar: new(slog.LevelVar),
	}

	// Test all logging methods
	assert.NotPanics(t, func() {
		logger.Debug("debug message", "key", "value")
		logger.Info("info message", "key", "value")
		logger.Warn("warn message", "key", "value")
		logger.Error("error message", "key", "value")
	})

	// Test context logging
	ctx := context.Background()
	assert.NotPanics(t, func() {
		logger.DebugContext(ctx, "debug context", "key", "value")
		logger.InfoContext(ctx, "info context", "key", "value")
		logger.WarnContext(ctx, "warn context", "key", "value")
		logger.ErrorContext(ctx, "error context", "key", "value")
	})

	// Test With method
	loggerWith := logger.With("global_key", "global_value")
	assert.NotPanics(t, func() {
		loggerWith.Info("message with global fields")
	})

	// Test SetLevel
	logger.SetLevel(slog.LevelWarn)
	assert.Equal(t, slog.LevelWarn, logger.levelVar.Level())

	// Test Sync
	err := logger.Sync()
	assert.NoError(t, err)

	// Test Slog
	slogLogger := logger.Slog()
	assert.NotNil(t, slogLogger)
}

// Mock handler that implements the Flusher lifecycle for testing
type flushableHandler struct {
	flushed  bool
	shutdown bool
}

func (h *flushableHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return true
}

func (h *flushableHandler) Handle(ctx context.Context, r slog.Record) error {
	return nil
}

func (h *flushableHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return h
}

func (h *flushableHandler) WithGroup(name string) slog.Handler {
	return h
}

func (h *flushableHandler) Flush(context.Context) error {
	h.flushed = true
	return nil
}

func (h *flushableHandler) Shutdown(context.Context) error {
	h.shutdown = true
	return nil
}
