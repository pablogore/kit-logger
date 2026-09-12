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

func TestL_WithNilLogger(t *testing.T) {
	// Reset global logger
	resetGlobalsForTest()

	// Test L() when no global logger is installed
	logger := L()
	assert.NotNil(t, logger)
	assert.IsType(t, &SlogLogger{}, logger)
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

// argValue finds the value following the first occurrence of key in a flat
// args slice, as capturedEntry.Args stores them.
func argValue(args []any, key string) (any, bool) {
	for i := 0; i+1 < len(args); i += 2 {
		if args[i] == key {
			return args[i+1], true
		}
	}
	return nil, false
}

// TestConfig_Sink_IsDecoratedLikeTheDefaultHandler pins the core fix for
// KITLOG-GO-011: a custom Sink is a terminal handler for the same pipeline
// the default Text/JSON handler goes through, not a total bypass of it.
func TestConfig_Sink_IsDecoratedLikeTheDefaultHandler(t *testing.T) {
	t.Run("FilterRules drop matching records", func(t *testing.T) {
		cap := newCapturingHandler()
		logger := New(Config{
			Sink:        cap,
			FilterRules: []handler.FilterRule{{Key: "secret"}},
		})
		logger.Info("kept")
		logger.Info("dropped", "secret", "x")

		entries := cap.getEntries()
		require.Len(t, entries, 1)
		assert.Equal(t, "kept", entries[0].Message)
	})

	t.Run("GlobalFields are attached", func(t *testing.T) {
		cap := newCapturingHandler()
		logger := New(Config{Sink: cap, GlobalFields: map[string]string{"service": "svc"}})
		logger.Info("hi")

		entries := cap.getEntries()
		require.Len(t, entries, 1)
		v, ok := argValue(entries[0].Args, "service")
		require.True(t, ok, "GlobalFields must be attached to a Sink-built logger")
		assert.Equal(t, "svc", v)
	})

	t.Run("component attribution is added", func(t *testing.T) {
		cap := newCapturingHandler()
		logger := New(Config{Sink: cap})
		logger.Info("hi")

		entries := cap.getEntries()
		require.Len(t, entries, 1)
		_, ok := argValue(entries[0].Args, "component")
		assert.True(t, ok, "Sink must receive component attribution like the default handler")
	})

	t.Run("Sampling is applied", func(t *testing.T) {
		cap := newCapturingHandler()
		logger := New(Config{
			Sink: cap,
			Sampling: SamplingConfig{
				Enabled:     true,
				Interval:    time.Hour,
				Probability: 1,
			},
		})
		logger.Info("first")
		logger.Info("first")

		require.Len(t, cap.getEntries(), 1, "the sampling interval must suppress the immediate repeat")
	})

	t.Run("Hook runs", func(t *testing.T) {
		cap := newCapturingHandler()
		called := false
		logger := New(Config{
			Sink: cap,
			Hook: func(ctx context.Context, r slog.Record) (context.Context, bool) {
				called = true
				return ctx, true
			},
		})
		logger.Info("hi")

		assert.True(t, called, "Hook must run for a Sink-built logger")
	})

	t.Run("BufferSize buffers until Sync", func(t *testing.T) {
		cap := newCapturingHandler()
		logger := New(Config{Sink: cap, BufferSize: 10})
		logger.Info("buffered")

		assert.Empty(t, cap.getEntries(), "must not reach the sink before Sync")
		require.NoError(t, logger.Sync())
		assert.Len(t, cap.getEntries(), 1)
	})
}

// TestConfig_Sink_SetLevelWorks is the exact no-op case from the issue: before
// the fix, SetLevel had no effect on a logger built with a custom Handler
// because the shared LevelVar was never wired into it.
func TestConfig_Sink_SetLevelWorks(t *testing.T) {
	cap := newCapturingHandler()
	logger := New(Config{Sink: cap, Level: "error"})

	logger.Info("suppressed before SetLevel")
	require.Empty(t, cap.getEntries())

	logger.SetLevel(slog.LevelDebug)
	logger.Debug("now visible")

	entries := cap.getEntries()
	require.Len(t, entries, 1)
	assert.Equal(t, "now visible", entries[0].Message)
}

// TestConfig_HandlerAndSink_BehaveIdentically pins Handler as a pure
// deprecated alias for Sink.
func TestConfig_HandlerAndSink_BehaveIdentically(t *testing.T) {
	viaHandler := newCapturingHandler()
	viaSink := newCapturingHandler()

	New(Config{Handler: viaHandler, GlobalFields: map[string]string{"k": "v"}}).Info("msg")
	New(Config{Sink: viaSink, GlobalFields: map[string]string{"k": "v"}}).Info("msg")

	handlerEntries := viaHandler.getEntries()
	sinkEntries := viaSink.getEntries()
	require.Len(t, handlerEntries, 1)
	require.Len(t, sinkEntries, 1)

	hv, hok := argValue(handlerEntries[0].Args, "k")
	sv, sok := argValue(sinkEntries[0].Args, "k")
	require.True(t, hok)
	require.True(t, sok)
	assert.Equal(t, sv, hv)
}

func TestConfig_Validate_HandlerAndSinkBothSet(t *testing.T) {
	cfg := Config{Handler: discardHandler{}, Sink: discardHandler{}}

	err := cfg.Validate()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "Handler")
	assert.Contains(t, err.Error(), "Sink")
}

func TestConfig_Validate_PipelineOverrideNamesIgnoredFields(t *testing.T) {
	cfg := Config{
		PipelineOverride: discardHandler{},
		GlobalFields:     map[string]string{"a": "b"},
		BufferSize:       10,
	}

	err := cfg.Validate()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "GlobalFields")
	assert.Contains(t, err.Error(), "BufferSize")
}

// TestConfig_Validate_PipelineOverrideNamesEveryIgnoredField is the
// regression net for the class of bug this fixes: PipelineOverride silently
// ignoring a field that Validate did not know to name. One sub-test per
// field the doc comment on PipelineOverride claims is ignored.
func TestConfig_Validate_PipelineOverrideNamesEveryIgnoredField(t *testing.T) {
	tests := []struct {
		name  string
		field string
		cfg   Config
	}{
		{"Handler", "Handler", Config{Handler: discardHandler{}}},
		{"Sink", "Sink", Config{Sink: discardHandler{}}},
		{"FilterRules", "FilterRules", Config{FilterRules: []handler.FilterRule{{Key: "x"}}}},
		{"GlobalFields", "GlobalFields", Config{GlobalFields: map[string]string{"a": "b"}}},
		{"Sampling", "Sampling", Config{Sampling: SamplingConfig{Enabled: true}}},
		{"BufferSize", "BufferSize", Config{BufferSize: 10}},
		{"Hook", "Hook", Config{Hook: func(ctx context.Context, r slog.Record) (context.Context, bool) { return ctx, true }}},
		{"Writer", "Writer", Config{Writer: &bytes.Buffer{}}},
		{"Format", "Format", Config{Format: "json"}},
		{"Level", "Level", Config{Level: "debug"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := tt.cfg
			cfg.PipelineOverride = discardHandler{}

			err := cfg.Validate()

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.field)
		})
	}
}

// TestConfig_Validate_PipelineOverrideDoesNotFlagFieldsThatStillApply pins
// the other half of the contract: RateLimit, ContextFields and
// ContextHandler are not routed through the ignored decorator chain, so
// PipelineOverride must not name them.
func TestConfig_Validate_PipelineOverrideDoesNotFlagFieldsThatStillApply(t *testing.T) {
	cfg := Config{
		PipelineOverride: discardHandler{},
		RateLimit:        RateLimitConfig{MaxKeys: 10},
		ContextFields:    func(ctx context.Context) []any { return nil },
		ContextHandler:   func(h slog.Handler) slog.Handler { return h },
	}

	err := cfg.Validate()

	assert.NoError(t, err)
}

func TestNewWithError_ReturnsValidateError(t *testing.T) {
	logger, err := NewWithError(Config{Handler: discardHandler{}, Sink: discardHandler{}})

	require.Error(t, err)
	assert.NotNil(t, logger, "an invalid Config must still produce a usable logger")
}

// TestNew_InvalidConfigWritesNothingToStdoutOrStderr pins New as I/O-free
// regardless of Validate's outcome: a library constructor must not perform
// I/O the caller did not ask for, including reporting its own misuse.
// NewWithError is the seam for a caller that wants the error.
func TestNew_InvalidConfigWritesNothingToStdoutOrStderr(t *testing.T) {
	outFile, err := os.CreateTemp("", "logger_silent_stdout")
	require.NoError(t, err)
	defer os.Remove(outFile.Name())
	defer outFile.Close()

	errFile, err := os.CreateTemp("", "logger_silent_stderr")
	require.NoError(t, err)
	defer os.Remove(errFile.Name())
	defer errFile.Close()

	oldStdout, oldStderr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = outFile, errFile
	defer func() { os.Stdout, os.Stderr = oldStdout, oldStderr }()

	cfg := Config{Handler: discardHandler{}, Sink: discardHandler{}}
	require.Error(t, cfg.Validate(), "test setup: cfg must actually be invalid")

	_ = New(cfg)

	stdoutContent, err := os.ReadFile(outFile.Name())
	require.NoError(t, err)
	assert.Empty(t, stdoutContent, "New must not write to stdout even for an invalid Config")

	stderrContent, err := os.ReadFile(errFile.Name())
	require.NoError(t, err)
	assert.Empty(t, stderrContent, "New must not write to stderr even for an invalid Config; use NewWithError to observe the error")
}

// TestConfig_PipelineOverride_BypassesDecoration pins the escape hatch: unlike
// Sink, PipelineOverride skips every decorator, exactly as Handler used to.
func TestConfig_PipelineOverride_BypassesDecoration(t *testing.T) {
	cap := newCapturingHandler()
	logger := New(Config{
		PipelineOverride: cap,
		FilterRules:      []handler.FilterRule{{Key: "x"}},
		GlobalFields:     map[string]string{"service": "svc"},
	})
	logger.Info("msg", "x", "should-not-be-filtered")

	entries := cap.getEntries()
	require.Len(t, entries, 1, "PipelineOverride must skip FilterRules entirely")
	_, hasGlobal := argValue(entries[0].Args, "service")
	assert.False(t, hasGlobal, "PipelineOverride must skip GlobalFields entirely")
}

// TestNew_DefaultConfigWritesNothingToStdout pins the LogInitialization gate:
// a library constructor must not write to stdout as a side effect of being
// called.
func TestNew_DefaultConfigWritesNothingToStdout(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "logger_silent_default")
	require.NoError(t, err)
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	oldStdout := os.Stdout
	os.Stdout = tmpFile
	defer func() { os.Stdout = oldStdout }()

	_ = New(Config{})

	content, err := os.ReadFile(tmpFile.Name())
	require.NoError(t, err)
	assert.Empty(t, content, "New must not write anything before the caller logs")
}
