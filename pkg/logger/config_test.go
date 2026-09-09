package logger

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pablogore/kit-logger/pkg/logger/handler"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetGlobal(t *testing.T) {
	// Test setting global logger
	mockLogger := NewMockLogger()
	SetGlobal(mockLogger)

	// Verify global logger is set -- by identity, so a copy or a torn value
	// would not pass.
	assert.Same(t, mockLogger, L())
}

func TestL_WithNilLogger(t *testing.T) {
	// Reset global logger
	resetGlobalsForTest()

	// Test L() when no global logger is installed
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

func TestConfig_Writer_DefaultsToStdout(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "logger_writer_default")
	require.NoError(t, err)
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	oldStdout := os.Stdout
	os.Stdout = tmpFile
	defer func() { os.Stdout = oldStdout }()

	logger := New(Config{Format: "json"})
	logger.Info("default writer message")

	content, err := os.ReadFile(tmpFile.Name())
	require.NoError(t, err)
	assert.Contains(t, string(content), "default writer message")
}

func TestConfig_Writer_WritesIntoBuffer(t *testing.T) {
	var buf bytes.Buffer
	logger := New(Config{Writer: &buf, Format: "json"})
	logger.Info("buffered message", "key", "value")

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	require.NotEmpty(t, lines)
	for _, line := range lines {
		var decoded map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &decoded), "line must be valid JSON: %s", line)
	}
	assert.Contains(t, buf.String(), "buffered message")
}

func TestConfig_Writer_ComposesWithPipeline(t *testing.T) {
	var buf bytes.Buffer
	hookCalled := false
	cfg := Config{
		Format: "json",
		Writer: &buf,
		GlobalFields: map[string]string{
			"service": "test-service",
		},
		FilterRules: []handler.FilterRule{
			{Key: "secret", Value: "should-be-discarded"},
		},
		Sampling:   SamplingConfig{Enabled: false},
		BufferSize: 10,
		Hook: func(ctx context.Context, r slog.Record) (context.Context, bool) {
			hookCalled = true
			return ctx, true
		},
	}
	logger := New(cfg)
	logger.Info("composed message", "secret", "kept")
	require.NoError(t, logger.Sync())

	out := buf.String()
	assert.Contains(t, out, "composed message")
	assert.Contains(t, out, `"service":"test-service"`)
	assert.True(t, hookCalled)
}

func TestConfig_Writer_ConcurrentWritesAreNonInterleaved(t *testing.T) {
	var buf syncBuffer
	logger := New(Config{Writer: &buf, Format: "json"})

	const n = 100
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			logger.Info("concurrent message", "i", i)
		}(i)
	}
	wg.Wait()
	require.NoError(t, logger.Sync())

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	seen := make(map[int]bool, n)
	for _, line := range lines {
		var decoded map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &decoded), "line must be complete, non-interleaved JSON: %s", line)
		if msg, _ := decoded["msg"].(string); msg == "concurrent message" {
			seen[int(decoded["i"].(float64))] = true
		}
	}
	assert.Len(t, seen, n, "all 100 concurrent log lines must be present and complete")
}

// syncBuffer wraps bytes.Buffer with a mutex so the -race detector can
// confirm slog's own locking, not just tolerate an unguarded buffer.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

// TestMain intercepts the KITLOGGER_WRITER_SUBPROCESS re-exec below and exits
// before testing.Main runs, so its own "PASS"/timing output never touches
// stdout — leaving stdout to carry only what the logger itself writes there.
func TestMain(m *testing.M) {
	if os.Getenv("KITLOGGER_WRITER_SUBPROCESS") == "1" {
		logger := New(Config{Writer: os.Stderr, Format: "json"})
		logger.Info("subprocess stderr message")
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestConfig_Writer_StderrSubprocess(t *testing.T) {
	cmd := exec.Command(os.Args[0])
	cmd.Env = append(os.Environ(), "KITLOGGER_WRITER_SUBPROCESS=1")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	require.NoError(t, cmd.Run())

	assert.Empty(t, stdout.Bytes(), "stdout must be byte-empty when Config.Writer is os.Stderr")
	assert.Contains(t, stderr.String(), "subprocess stderr message")
}
