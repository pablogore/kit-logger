package logger

import (
	"context"
	"log/slog"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// callerSkipTestFile is the base name of this file, the expected attribution
// target for every call made from it.
var callerSkipTestFile = func() string {
	_, self, _, _ := runtime.Caller(0)
	return filepath.Base(self)
}()

// adapter stands in for a wrapper that implements some other logging
// interface on top of Logger: every call goes through one extra frame.
type adapter struct{ log Logger }

func newAdapter(log Logger) *adapter {
	if s, ok := log.(CallerSkipper); ok {
		log = s.WithCallerSkip(1)
	}
	return &adapter{log: log}
}

func (a *adapter) Debug(msg string, args ...any) { a.log.Debug(msg, args...) }
func (a *adapter) Info(msg string, args ...any)  { a.log.Info(msg, args...) }
func (a *adapter) Warn(msg string, args ...any)  { a.log.Warn(msg, args...) }
func (a *adapter) Error(msg string, args ...any) { a.log.Error(msg, args...) }
func (a *adapter) DebugContext(ctx context.Context, msg string, args ...any) {
	a.log.DebugContext(ctx, msg, args...)
}
func (a *adapter) InfoContext(ctx context.Context, msg string, args ...any) {
	a.log.InfoContext(ctx, msg, args...)
}
func (a *adapter) WarnContext(ctx context.Context, msg string, args ...any) {
	a.log.WarnContext(ctx, msg, args...)
}
func (a *adapter) ErrorContext(ctx context.Context, msg string, args ...any) {
	a.log.ErrorContext(ctx, msg, args...)
}
func (a *adapter) Log(ctx context.Context, level slog.Level, msg string, args ...any) {
	a.log.Log(ctx, level, msg, args...)
}
func (a *adapter) With(args ...any) Logger { return &adapter{log: a.log.With(args...)} }
func (a *adapter) WithContext(ctx context.Context) Logger {
	return &adapter{log: a.log.WithContext(ctx)}
}
func (a *adapter) SetLevel(level slog.Level) { a.log.SetLevel(level) }
func (a *adapter) Sync() error               { return a.log.Sync() }
func (a *adapter) Slog() *slog.Logger        { return a.log.Slog() }

// WithCallerSkip forwards the skip, which is what lets a wrapper around this
// wrapper attribute correctly: without it the outer layer would find no
// CallerSkipper and the record would name this adapter.
func (a *adapter) WithCallerSkip(n int) Logger {
	if s, ok := a.log.(CallerSkipper); ok {
		return &adapter{log: s.WithCallerSkip(n)}
	}
	return a
}

// TestSlogLogger_WithCallerSkipAttributesThroughAnAdapter proves that every
// exported method, called through one adapter frame, attributes the record to
// the adapter's caller rather than to the adapter -- synchronously and behind
// a buffer.
func TestSlogLogger_WithCallerSkipAttributesThroughAnAdapter(t *testing.T) {
	for _, bufferSize := range []int{0, 1024} {
		for _, tc := range facadeCalls() {
			t.Run(tc.name, func(t *testing.T) {
				log, sink, flush := newFacade(t, bufferSize)
				wrapped := newAdapter(log)

				expectedLine := tc.call(wrapped)
				flush()

				records := sink.drain()
				require.Len(t, records, 1)
				got := componentGroupOf(records[0])
				require.True(t, got.found, "component group not found")
				assert.Equal(t, thisFile, got.file, "facadeCalls live in the neighbouring test file")
				assert.Equal(t, expectedLine, got.line, "record must name the adapter's caller, not the adapter")
				assert.NotContains(t, got.fn, "adapter")
			})
		}
	}
}

// TestSlogLogger_WithCallerSkipComposes proves that skips add up, so a wrapper
// around a wrapper attributes correctly without either knowing about the other.
func TestSlogLogger_WithCallerSkipComposes(t *testing.T) {
	log, sink, _ := newFacade(t, 0)
	outer := newAdapter(newAdapter(log))

	_, _, line, _ := runtime.Caller(0)
	outer.Info("two layers up")

	records := sink.drain()
	require.Len(t, records, 1)
	got := componentGroupOf(records[0])
	require.True(t, got.found)
	assert.Equal(t, callerSkipTestFile, got.file)
	assert.Equal(t, line+1, got.line)
}

// TestSlogLogger_WithCallerSkipSurvivesWithAndWithContext proves a derived
// logger keeps the skip: an adapter that calls With on its wrapped logger must
// not silently lose attribution.
func TestSlogLogger_WithCallerSkipSurvivesWithAndWithContext(t *testing.T) {
	log, sink, _ := newFacade(t, 0)
	wrapped := newAdapter(log).With("k", "v").WithContext(context.Background())

	_, _, line, _ := runtime.Caller(0)
	wrapped.Info("derived")

	records := sink.drain()
	require.Len(t, records, 1)
	got := componentGroupOf(records[0])
	require.True(t, got.found)
	assert.Equal(t, line+1, got.line)
}

// TestSlogLogger_WithCallerSkipSharesStateAndRejectsNonPositive pins the
// derivation contract: a non-positive skip is the receiver itself, and a real
// skip shares rate-limit state and lifecycle exactly like With does.
func TestSlogLogger_WithCallerSkipSharesStateAndRejectsNonPositive(t *testing.T) {
	log := New(Config{Sink: discardHandler{}}).(*SlogLogger)

	assert.Same(t, log, log.WithCallerSkip(0))
	assert.Same(t, log, log.WithCallerSkip(-1))

	derived, ok := log.WithCallerSkip(2).(*SlogLogger)
	require.True(t, ok)
	assert.Equal(t, 2, derived.callerSkip)
	assert.Same(t, log.rateState, derived.rateState)
	assert.Same(t, log.lifecycle, derived.lifecycle)
	assert.Same(t, log.levelVar, derived.levelVar)
	assert.Equal(t, 0, log.callerSkip, "the receiver is not mutated")
}

// TestSlogLogger_WithCallerSkipBeyondTheStackAddsNoGroup proves that a skip
// that runs past the top of the stack degrades to "no attribution" rather
// than a wrong frame or a panic.
func TestSlogLogger_WithCallerSkipBeyondTheStackAddsNoGroup(t *testing.T) {
	log, sink, _ := newFacade(t, 0)

	log.(CallerSkipper).WithCallerSkip(1 << 16).Info("nowhere")

	records := sink.drain()
	require.Len(t, records, 1)
	assert.False(t, componentGroupOf(records[0]).found)
}
