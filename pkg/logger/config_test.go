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
		Level:  LevelInfo,
		Format: FormatJSON,
	}
	logger := New(cfg)

	assert.NotNil(t, logger)
	assert.IsType(t, &SlogLogger{}, logger)
}

func TestNew_WithTextFormat(t *testing.T) {
	cfg := Config{
		Level:  LevelDebug,
		Format: FormatText,
	}
	logger := New(cfg)

	assert.NotNil(t, logger)
	assert.IsType(t, &SlogLogger{}, logger)
}

func TestNew_WithFilterRules(t *testing.T) {
	cfg := Config{
		Level: LevelInfo,
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
		Level: LevelWarn,
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
		Level: LevelError,
		Sampling: SamplingConfig{
			Enabled:     true,
			Interval:    time.Second,
			Probability: 0.5,
			MinLevel:    LevelInfo,
		},
	}
	logger := New(cfg)

	assert.NotNil(t, logger)
	assert.IsType(t, &SlogLogger{}, logger)
}

func TestNew_WithBufferSize(t *testing.T) {
	cfg := Config{
		Level:      LevelDebug,
		BufferSize: 100,
	}
	logger := New(cfg)

	assert.NotNil(t, logger)
	assert.IsType(t, &SlogLogger{}, logger)
}

func TestNew_WithHook(t *testing.T) {
	cfg := Config{
		Level: LevelInfo,
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
		Level:  LevelDebug,
		Format: FormatJSON,
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
			MinLevel:    LevelInfo,
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
	level, err := ParseLevel("debug")
	require.NoError(t, err)
	assert.Equal(t, LevelDebug, level)

	level, err = ParseLevel("DEBUG")
	require.NoError(t, err)
	assert.Equal(t, LevelDebug, level)
}

func TestParseLevel_Info(t *testing.T) {
	level, err := ParseLevel("info")
	require.NoError(t, err)
	assert.Equal(t, LevelInfo, level)

	level, err = ParseLevel("INFO")
	require.NoError(t, err)
	assert.Equal(t, LevelInfo, level)
}

func TestParseLevel_Warn(t *testing.T) {
	level, err := ParseLevel("warn")
	require.NoError(t, err)
	assert.Equal(t, LevelWarn, level)

	level, err = ParseLevel("WARN")
	require.NoError(t, err)
	assert.Equal(t, LevelWarn, level)

	level, err = ParseLevel("warning")
	require.NoError(t, err)
	assert.Equal(t, LevelWarn, level)

	level, err = ParseLevel("WARNING")
	require.NoError(t, err)
	assert.Equal(t, LevelWarn, level)
}

func TestParseLevel_Error(t *testing.T) {
	level, err := ParseLevel("error")
	require.NoError(t, err)
	assert.Equal(t, LevelError, level)

	level, err = ParseLevel("ERROR")
	require.NoError(t, err)
	assert.Equal(t, LevelError, level)
}

// TestParseLevel_EmptyIsExplicitDefault pins the "unset" case: the empty
// string is not a typo, it means "use the default," so it must return
// LevelInfo with a nil error -- unlike any other unrecognized input.
func TestParseLevel_EmptyIsExplicitDefault(t *testing.T) {
	level, err := ParseLevel("")
	require.NoError(t, err)
	assert.Equal(t, LevelInfo, level)
}

// TestParseLevel_RejectsUnknownInput is the regression net for the defect
// this issue exists to close: an unrecognized level name must be reported,
// never silently coerced into LevelInfo.
func TestParseLevel_RejectsUnknownInput(t *testing.T) {
	for _, in := range []string{"unknown", "invalid", "debgu", "trace", "fatal", "panic", "notice"} {
		t.Run(in, func(t *testing.T) {
			_, err := ParseLevel(in)
			require.Error(t, err, "ParseLevel(%q) must return an error, not silently default", in)
			assert.Contains(t, err.Error(), in)
		})
	}
}

func TestParseFormat_JSON(t *testing.T) {
	for _, in := range []string{"json", "JSON", "Json"} {
		t.Run(in, func(t *testing.T) {
			format, err := ParseFormat(in)
			require.NoError(t, err)
			assert.Equal(t, FormatJSON, format)
		})
	}
}

func TestParseFormat_Text(t *testing.T) {
	for _, in := range []string{"text", "TEXT"} {
		t.Run(in, func(t *testing.T) {
			format, err := ParseFormat(in)
			require.NoError(t, err)
			assert.Equal(t, FormatText, format)
		})
	}
}

// TestParseFormat_EmptyIsExplicitDefault mirrors
// TestParseLevel_EmptyIsExplicitDefault for Format.
func TestParseFormat_EmptyIsExplicitDefault(t *testing.T) {
	format, err := ParseFormat("")
	require.NoError(t, err)
	assert.Equal(t, FormatText, format)
}

func TestParseFormat_RejectsUnknownInput(t *testing.T) {
	for _, in := range []string{"logfmt", "jsno", "yaml"} {
		t.Run(in, func(t *testing.T) {
			_, err := ParseFormat(in)
			require.Error(t, err, "ParseFormat(%q) must return an error, not silently default", in)
			assert.Contains(t, err.Error(), in)
		})
	}
}

func TestNew_Integration(t *testing.T) {
	// Test that logger can actually log
	cfg := Config{
		Level:  LevelDebug,
		Format: FormatText,
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
		Level:  LevelInfo,
		Format: FormatJSON,
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

	logger := New(Config{Format: FormatJSON})
	logger.Info("default writer message")

	content, err := os.ReadFile(tmpFile.Name())
	require.NoError(t, err)
	assert.Contains(t, string(content), "default writer message")
}

func TestConfig_Writer_WritesIntoBuffer(t *testing.T) {
	var buf bytes.Buffer
	logger := New(Config{Writer: &buf, Format: FormatJSON})
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
		Format: FormatJSON,
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
	logger := New(Config{Writer: &buf, Format: FormatJSON})

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
		logger := New(Config{Writer: os.Stderr, Format: FormatJSON})
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
	logger := New(Config{Sink: cap, Level: LevelError})

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
		{"Format", "Format", Config{Format: FormatJSON}},
		{"Level", "Level", Config{Level: LevelDebug}},
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

// TestConfig_ZeroValueValidatesAndBehavesLikeBefore pins the non-breaking
// half of the retyping: a zero-value Config still means "text to stdout at
// info," and still passes Validate.
func TestConfig_ZeroValueValidatesAndBehavesLikeBefore(t *testing.T) {
	var cfg Config
	require.NoError(t, cfg.Validate())
	assert.Equal(t, LevelInfo, cfg.Level)
	assert.Equal(t, FormatText, cfg.Format)
}

func TestConfig_Validate_NegativeBufferSize(t *testing.T) {
	err := Config{BufferSize: -1}.Validate()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "BufferSize")
}

func TestConfig_Validate_OutOfRangeFormat(t *testing.T) {
	err := Config{Format: Format(7)}.Validate()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "Format")
}

// TestConfig_Validate_InvalidLevelString is the regression net at the Config
// level for the defect ParseLevel already guards at the parser level: a typo
// in the deprecated bridge must fail Validate, not silently resolve to info.
func TestConfig_Validate_InvalidLevelString(t *testing.T) {
	err := Config{LevelString: "debgu"}.Validate()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "LevelString")

	_, newErr := NewWithError(Config{LevelString: "debgu"})
	require.Error(t, newErr, "NewWithError must surface the bad LevelString rather than silently succeeding at info")
}

func TestConfig_Validate_InvalidFormatString(t *testing.T) {
	err := Config{FormatString: "logfmt"}.Validate()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "FormatString")
}

// TestConfig_Validate_TypedAndLegacyEquivalentIsValid covers the case where
// both Level and LevelString are set but agree: no conflict, no error.
func TestConfig_Validate_TypedAndLegacyEquivalentIsValid(t *testing.T) {
	err := Config{Level: LevelWarn, LevelString: "warn"}.Validate()
	assert.NoError(t, err)

	err = Config{Format: FormatJSON, FormatString: "json"}.Validate()
	assert.NoError(t, err)
}

// TestConfig_Validate_TypedAndLegacyConflictIsAnError covers the opposite
// case: both set, disagreeing -- Validate must report it rather than picking
// one of the two silently.
func TestConfig_Validate_TypedAndLegacyConflictIsAnError(t *testing.T) {
	err := Config{Level: LevelWarn, LevelString: "debug"}.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Level")

	err = Config{Format: FormatJSON, FormatString: "text"}.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Format")
}

// TestConfig_ZeroValueLevelCannotDetectLegacyConflict pins a documented
// limitation of the typed/legacy bridge, not a bug: LevelInfo is Level's
// zero value, so Config{Level: LevelInfo, LevelString: "debug"} is the exact
// same struct value as Config{LevelString: "debug"} -- Validate has no way
// to know the caller "explicitly" chose LevelInfo rather than leaving the
// field untouched, so it cannot flag this as a conflict. LevelString wins.
// See resolveLevel's doc comment. If this test ever starts failing because
// Validate began rejecting the combination, that's a deliberate contract
// change, not a regression -- update this test's expectations along with it.
func TestConfig_ZeroValueLevelCannotDetectLegacyConflict(t *testing.T) {
	cfg := Config{Level: LevelInfo, LevelString: "debug"}

	assert.NoError(t, cfg.Validate())

	resolved, err := resolveLevel("Level", cfg.Level, cfg.LevelString)
	require.NoError(t, err)
	assert.Equal(t, LevelDebug, resolved, "LevelString must still take effect despite the explicit zero-value Level")
}

func TestConfig_ZeroValueLevelAgreesWithLegacy(t *testing.T) {
	cfg := Config{Level: LevelInfo, LevelString: "info"}

	assert.NoError(t, cfg.Validate())

	resolved, err := resolveLevel("Level", cfg.Level, cfg.LevelString)
	require.NoError(t, err)
	assert.Equal(t, LevelInfo, resolved)
}

// TestConfig_ZeroValueFormatCannotDetectLegacyConflict is
// TestConfig_ZeroValueLevelCannotDetectLegacyConflict's counterpart for
// Format/FormatString, with FormatText standing in for LevelInfo.
func TestConfig_ZeroValueFormatCannotDetectLegacyConflict(t *testing.T) {
	cfg := Config{Format: FormatText, FormatString: "json"}

	assert.NoError(t, cfg.Validate())

	resolved, err := resolveFormat("Format", cfg.Format, cfg.FormatString)
	require.NoError(t, err)
	assert.Equal(t, FormatJSON, resolved, "FormatString must still take effect despite the explicit zero-value Format")
}

func TestConfig_ZeroValueFormatAgreesWithLegacy(t *testing.T) {
	cfg := Config{Format: FormatText, FormatString: "text"}

	assert.NoError(t, cfg.Validate())

	resolved, err := resolveFormat("Format", cfg.Format, cfg.FormatString)
	require.NoError(t, err)
	assert.Equal(t, FormatText, resolved)
}

func TestConfig_Validate_SamplingMinLevelStringConflict(t *testing.T) {
	err := Config{Sampling: SamplingConfig{MinLevel: LevelWarn, MinLevelString: "error"}}.Validate()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "Sampling.MinLevel")
}

// TestConfig_Validate_ReportsEveryFailureAtOnce pins errors.Join being used
// instead of fail-fast: a Config with several independent problems must name
// all of them in one error, not just the first one Validate happens to hit.
func TestConfig_Validate_ReportsEveryFailureAtOnce(t *testing.T) {
	err := Config{
		BufferSize:  -1,
		LevelString: "debgu",
		Sampling:    SamplingConfig{MinLevelString: "logfmt"},
	}.Validate()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "BufferSize")
	assert.Contains(t, err.Error(), "LevelString")
	assert.Contains(t, err.Error(), "Sampling.MinLevel")
}

// TestConfig_LevelString_LegacyBridgeWorksEndToEnd proves the deprecated
// string bridge actually drives behavior, not just Validate: a Config built
// only through LevelString/FormatString must gate/encode exactly like the
// typed equivalent.
func TestConfig_LevelString_LegacyBridgeWorksEndToEnd(t *testing.T) {
	cap := newCapturingHandler()
	logger := New(Config{Sink: cap, LevelString: "warn"})

	logger.Info("suppressed at warn")
	assert.Empty(t, cap.getEntries())

	logger.Warn("visible at warn")
	assert.Len(t, cap.getEntries(), 1)
}

func TestConfig_FormatString_LegacyBridgeSelectsJSON(t *testing.T) {
	var buf bytes.Buffer
	logger := New(Config{Writer: &buf, FormatString: "JSON"})
	logger.Info("hi")

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &decoded), "FormatString bridge must select the JSON handler")
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

// invalidSamplingConfig is a SamplingConfig that fails SamplingConfig.Validate
// (Probability out of [0,1]), used to drive decorate's sampling branch down
// its error path without going through the Handler/Sink validation checked by
// the tests above.
func invalidSamplingConfig() SamplingConfig {
	return SamplingConfig{Enabled: true, Probability: -1}
}

// TestNew_InvalidSamplingWritesNothingToStdoutOrStderr pins decorate's
// sampling branch to the same I/O-free contract as New itself: constructing
// the handler chain must never report a problem on its own, even when the
// invalid field lives inside SamplingConfig rather than Config directly.
func TestNew_InvalidSamplingWritesNothingToStdoutOrStderr(t *testing.T) {
	outFile, err := os.CreateTemp("", "logger_silent_sampling_stdout")
	require.NoError(t, err)
	defer os.Remove(outFile.Name())
	defer outFile.Close()

	errFile, err := os.CreateTemp("", "logger_silent_sampling_stderr")
	require.NoError(t, err)
	defer os.Remove(errFile.Name())
	defer errFile.Close()

	oldStdout, oldStderr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = outFile, errFile
	defer func() { os.Stdout, os.Stderr = oldStdout, oldStderr }()

	cfg := Config{Sampling: invalidSamplingConfig()}
	require.Error(t, cfg.Validate(), "test setup: cfg must actually be invalid")

	logger := New(cfg)
	require.NotNil(t, logger, "an invalid Sampling config must still produce a usable logger")

	stdoutContent, err := os.ReadFile(outFile.Name())
	require.NoError(t, err)
	assert.Empty(t, stdoutContent, "New must not write to stdout for invalid Sampling")

	stderrContent, err := os.ReadFile(errFile.Name())
	require.NoError(t, err)
	assert.Empty(t, stderrContent, "New must not write to stderr for invalid Sampling; use NewWithError to observe the error")
}

// TestNewWithError_InvalidSamplingReturnsErrorWithoutWriting proves the
// error still reaches a caller who asks for it (via NewWithError) while
// decorate itself performs no I/O -- the two channels the previous review
// found conflated.
func TestNewWithError_InvalidSamplingReturnsErrorWithoutWriting(t *testing.T) {
	errFile, err := os.CreateTemp("", "logger_silent_sampling_stderr_2")
	require.NoError(t, err)
	defer os.Remove(errFile.Name())
	defer errFile.Close()

	oldStderr := os.Stderr
	os.Stderr = errFile
	defer func() { os.Stderr = oldStderr }()

	cap := newCapturingHandler()
	logger, err := NewWithError(Config{Sink: cap, Sampling: invalidSamplingConfig()})

	require.Error(t, err, "NewWithError must surface the invalid Sampling config")
	assert.Contains(t, err.Error(), "Probability")
	require.NotNil(t, logger)

	logger.Info("still emits under the safe, emitting-default sampler")
	assert.Len(t, cap.getEntries(), 1, "the documented safe/emitting fallback must still emit records")

	stderrContent, readErr := os.ReadFile(errFile.Name())
	require.NoError(t, readErr)
	assert.Empty(t, stderrContent, "NewWithError must not also write the error to stderr")
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
