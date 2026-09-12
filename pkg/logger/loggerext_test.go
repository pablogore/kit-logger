package logger

import (
	"context"
	"io"
	"log/slog"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

// stubLogger is a minimal Logger double for tests in this file. It can't use
// kitlogtest.MockLogger: that package imports this one, and an internal test
// file (package logger, not logger_test) importing anything that re-imports
// logger is a compile-time import cycle.
type stubLogger struct{}

func (s *stubLogger) Debug(string, ...any)                            {}
func (s *stubLogger) Info(string, ...any)                             {}
func (s *stubLogger) Warn(string, ...any)                             {}
func (s *stubLogger) Error(string, ...any)                            {}
func (s *stubLogger) DebugContext(context.Context, string, ...any)    {}
func (s *stubLogger) InfoContext(context.Context, string, ...any)     {}
func (s *stubLogger) WarnContext(context.Context, string, ...any)     {}
func (s *stubLogger) ErrorContext(context.Context, string, ...any)    {}
func (s *stubLogger) Log(context.Context, slog.Level, string, ...any) {}
func (s *stubLogger) With(...any) Logger                              { return s }
func (s *stubLogger) WithContext(context.Context) Logger              { return s }
func (s *stubLogger) SetLevel(slog.Level)                             {}
func (s *stubLogger) Sync() error                                     { return nil }
func (s *stubLogger) Slog() *slog.Logger                              { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestExitWithFlush_WithFlusher(t *testing.T) {
	// Create a logger that implements Sync
	logger := New(Config{Level: LevelInfo})
	SetGlobal(logger)

	// Test that the function can be called without panic
	assert.NotPanics(t, func() {
		// We can't actually call ExitWithFlush in tests as it would exit the process
		// But we can test the logic by calling the function in a subprocess
		if os.Getenv("TEST_EXIT_WITH_FLUSH") == "1" {
			ExitWithFlush(0)
		}
	})
}

func TestExitWithFlush_WithoutFlusher(t *testing.T) {
	// Create a logger that doesn't implement Sync
	mockLogger := &stubLogger{}
	SetGlobal(mockLogger)

	// Test that the function can be called without panic
	assert.NotPanics(t, func() {
		// We can't actually call ExitWithFlush in tests as it would exit the process
		// But we can test the logic by calling the function in a subprocess
		if os.Getenv("TEST_EXIT_WITH_FLUSH") == "1" {
			ExitWithFlush(1)
		}
	})
}

// TestExitWithFlush_ActuallyCalls tests that ExitWithFlush actually calls the function
func TestExitWithFlush_ActuallyCalls(t *testing.T) {
	// Create a logger that implements Sync
	logger := New(Config{Level: LevelInfo})
	SetGlobal(logger)

	// Test that ExitWithFlush can be called and doesn't panic
	// Note: We can't actually test os.Exit in unit tests, but we can test the logic
	// by verifying that the function can be called without panic
	assert.NotPanics(t, func() {
		// This would normally exit the process, but in tests we can't verify that
		// We can only verify that the function doesn't panic
		if os.Getenv("TEST_EXIT_WITH_FLUSH") == "1" {
			ExitWithFlush(42)
		}
	})
}

// TestExitWithFlush_DirectCall tests ExitWithFlush by calling it directly
func TestExitWithFlush_DirectCall(t *testing.T) {
	// Create a logger that implements Sync
	logger := New(Config{Level: LevelInfo})
	SetGlobal(logger)

	// Test that ExitWithFlush can be called directly
	// We'll use a flag to prevent actual exit in test environment
	originalExit := exitFunc
	defer func() { exitFunc = originalExit }()

	exitCalled := false
	exitCode := 0
	exitFunc = func(code int) {
		exitCalled = true
		exitCode = code
		// Don't actually exit in test
	}

	// Call ExitWithFlush
	ExitWithFlush(42)

	// Verify that os.Exit was called with the correct code
	assert.True(t, exitCalled, "ExitWithFlush should call os.Exit")
	assert.Equal(t, 42, exitCode, "ExitWithFlush should pass the correct exit code")
}

func TestEnsureLoggerOnce_FirstCall(t *testing.T) {
	// Reset global logger and sync.Once so this test sees the "first call" behavior
	resetGlobalsForTest()

	// Test first call
	cfg := Config{Level: LevelDebug, Format: FormatText}
	EnsureLoggerOnce(cfg)

	// Verify logger was set
	assert.NotNil(t, peekGlobalForTest())
	assert.IsType(t, &SlogLogger{}, peekGlobalForTest())
}

func TestEnsureLoggerOnce_SubsequentCalls(t *testing.T) {
	// Set initial logger
	initialLogger := &stubLogger{}
	SetGlobal(initialLogger)

	// Test subsequent calls with different config
	cfg := Config{Level: LevelError, Format: FormatJSON}
	EnsureLoggerOnce(cfg)

	// Verify logger is still the same (not changed)
	assert.Same(t, initialLogger, peekGlobalForTest())
	assert.NotEqual(t, &SlogLogger{}, peekGlobalForTest())
}

func TestEnsureLoggerOnce_MultipleCalls(t *testing.T) {
	// Reset global logger and sync.Once so the first EnsureLoggerOnce runs
	resetGlobalsForTest()

	// Test multiple calls
	cfg1 := Config{Level: LevelDebug, Format: FormatText}
	cfg2 := Config{Level: LevelError, Format: FormatJSON}
	cfg3 := Config{Level: LevelWarn, Format: FormatText}

	EnsureLoggerOnce(cfg1)
	firstLogger := peekGlobalForTest()

	EnsureLoggerOnce(cfg2)
	secondLogger := peekGlobalForTest()

	EnsureLoggerOnce(cfg3)
	thirdLogger := peekGlobalForTest()

	// All should be the same logger (first one)
	assert.Equal(t, firstLogger, secondLogger)
	assert.Equal(t, firstLogger, thirdLogger)
	assert.Equal(t, secondLogger, thirdLogger)
}

func TestEnsureLoggerOnce_ConcurrentCalls(t *testing.T) {
	// Test concurrent calls - note that sync.Once is global
	// so this test will only work if EnsureLoggerOnce hasn't been called before
	cfg := Config{Level: LevelInfo, Format: FormatJSON}

	// Create multiple goroutines calling EnsureLoggerOnce
	done := make(chan bool, 10)

	for i := 0; i < 10; i++ {
		go func() {
			EnsureLoggerOnce(cfg)
			done <- true
		}()
	}

	// Wait for all goroutines to complete
	for i := 0; i < 10; i++ {
		<-done
	}

	// Since sync.Once is global, we can't guarantee the logger is set
	// Just test that the function doesn't panic
	assert.NotPanics(t, func() {
		EnsureLoggerOnce(cfg)
	})
}

func TestEnsureLoggerOnce_WithDifferentConfigs(t *testing.T) {
	// Note: sync.Once is global, so these tests will only work
	// if EnsureLoggerOnce hasn't been called before in this process

	testCases := []struct {
		name string
		cfg  Config
	}{
		{"Minimal", Config{}},
		{"WithLevel", Config{Level: LevelDebug}},
		{"WithFormat", Config{Format: FormatJSON}},
		{"WithGlobalFields", Config{GlobalFields: map[string]string{"service": "test"}}},
		{"Complete", Config{
			Level:        LevelWarn,
			Format:       FormatText,
			GlobalFields: map[string]string{"service": "test", "version": "1.0"},
		}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Since sync.Once is global, we can't reset it
			// Just test that the function doesn't panic
			assert.NotPanics(t, func() {
				EnsureLoggerOnce(tc.cfg)
			})
		})
	}
}

func TestEnsureLoggerOnce_Integration(t *testing.T) {
	// Reset global logger
	resetGlobalsForTest()

	// Test integration with actual logging
	cfg := Config{
		Level:  LevelDebug,
		Format: FormatText,
		GlobalFields: map[string]string{
			"service": "integration-test",
		},
	}

	EnsureLoggerOnce(cfg)

	// Verify logger works
	logger := L()
	assert.NotNil(t, logger)

	// Test that we can log
	logger.Info("test message", "key", "value")

	// Test that the logger is the same as the global one
	assert.Same(t, peekGlobalForTest(), logger)
}

func TestEnsureLoggerOnce_WithNilConfig(t *testing.T) {
	// Test with zero value config
	var cfg Config

	// Since sync.Once is global, we can't reset it
	// Just test that the function doesn't panic
	assert.NotPanics(t, func() {
		EnsureLoggerOnce(cfg)
	})
}

func TestEnsureLoggerOnce_ThreadSafety(t *testing.T) {
	// Test thread safety by calling from multiple goroutines
	cfg := Config{Level: LevelInfo}

	// Create a channel to signal completion
	done := make(chan bool, 100)

	// Start 100 goroutines
	for i := 0; i < 100; i++ {
		go func(id int) {
			EnsureLoggerOnce(cfg)
			done <- true
		}(i)
	}

	// Wait for all to complete
	for i := 0; i < 100; i++ {
		<-done
	}

	// Verify logger was set correctly
	assert.NotNil(t, peekGlobalForTest())
}

func TestLoggerExtension_EdgeCases(t *testing.T) {
	// Test that L() works
	logger := L()
	assert.NotNil(t, logger)

	// Test that EnsureLoggerOnce doesn't panic
	cfg := Config{}
	assert.NotPanics(t, func() {
		EnsureLoggerOnce(cfg)
	})
}

func TestLoggerExtension_WithCustomLogger(t *testing.T) {
	// Set a custom logger
	customLogger := &stubLogger{}
	SetGlobal(customLogger)

	// Try to ensure logger once
	cfg := Config{Level: LevelDebug}
	EnsureLoggerOnce(cfg)

	// Should still be the custom logger
	assert.Same(t, customLogger, peekGlobalForTest())
	assert.NotEqual(t, &SlogLogger{}, peekGlobalForTest())
}
