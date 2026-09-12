package logger

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// capturingHandler captures log records for tests.
// It accumulates attrs from WithAttrs so that Handle sees Record attrs plus handler attrs.
// All handlers (base and from WithAttrs) share the same entries slice and mutex.
type capturingHandler struct {
	sharedMu      *sync.Mutex
	sharedEntries *[]capturedEntry
	attrs         []slog.Attr
}

type capturedEntry struct {
	Level   slog.Level
	Message string
	Args    []any
}

func (c *capturingHandler) Enabled(_ context.Context, _ slog.Level) bool { return true }

func (c *capturingHandler) Handle(ctx context.Context, r slog.Record) error {
	if c.sharedMu != nil {
		c.sharedMu.Lock()
		defer c.sharedMu.Unlock()
	}
	var args []any
	for _, a := range c.attrs {
		args = append(args, a.Key, a.Value.Any())
	}
	r.Attrs(func(a slog.Attr) bool {
		args = append(args, a.Key, a.Value.Any())
		return true
	})
	*c.sharedEntries = append(*c.sharedEntries, capturedEntry{Level: r.Level, Message: r.Message, Args: args})
	return nil
}

func (c *capturingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	newAttrs := make([]slog.Attr, 0, len(c.attrs)+len(attrs))
	newAttrs = append(newAttrs, c.attrs...)
	newAttrs = append(newAttrs, attrs...)
	return &capturingHandler{sharedMu: c.sharedMu, sharedEntries: c.sharedEntries, attrs: newAttrs}
}

func (c *capturingHandler) WithGroup(name string) slog.Handler { return c }

func (c *capturingHandler) getEntries() []capturedEntry {
	if c.sharedMu != nil {
		c.sharedMu.Lock()
		defer c.sharedMu.Unlock()
	}
	if c.sharedEntries == nil {
		return nil
	}
	return append([]capturedEntry(nil), *c.sharedEntries...)
}

func newCapturingHandler() *capturingHandler {
	var mu sync.Mutex
	return &capturingHandler{sharedMu: &mu, sharedEntries: &[]capturedEntry{}}
}

// stripComponent drops a trailing "component" key/value pair from args.
// Config.Handler is now decorated like any other sink, so every record also
// carries the "component" group ComponentHandler adds -- tests that assert
// their own args verbatim strip it first rather than hardcoding its value.
func stripComponent(args []any) []any {
	for i := 0; i+1 < len(args); i += 2 {
		if args[i] == "component" {
			return append(append([]any{}, args[:i]...), args[i+2:]...)
		}
	}
	return args
}

func TestRateLimit_AllowsFirstEvent(t *testing.T) {
	cap := newCapturingHandler()
	log := New(Config{Handler: cap}).(*SlogLogger)
	interval := 10 * time.Second

	log.Warn("first", WithRateLimit("key1", interval))

	entries := cap.getEntries()
	require.Len(t, entries, 1)
	assert.Equal(t, "first", entries[0].Message)
	assert.Equal(t, slog.LevelWarn, entries[0].Level)
}

func TestRateLimit_SuppressesWithinInterval(t *testing.T) {
	cap := newCapturingHandler()
	log := New(Config{Handler: cap}).(*SlogLogger)
	interval := 100 * time.Millisecond

	key := "suppress_key"
	log.Warn("one", WithRateLimit(key, interval))
	log.Warn("two", WithRateLimit(key, interval))
	log.Warn("three", WithRateLimit(key, interval))

	entries := cap.getEntries()
	require.Len(t, entries, 1)
	assert.Equal(t, "one", entries[0].Message)
}

func TestRateLimit_EmitsSuppressedCount(t *testing.T) {
	cap := newCapturingHandler()
	log := New(Config{Handler: cap}).(*SlogLogger)
	interval := 50 * time.Millisecond

	key := "count_key"
	log.Warn("first", WithRateLimit(key, interval))
	log.Warn("x", WithRateLimit(key, interval))
	log.Warn("x", WithRateLimit(key, interval))
	log.Warn("x", WithRateLimit(key, interval))
	require.Len(t, cap.getEntries(), 1)

	time.Sleep(interval + 10*time.Millisecond)
	log.Warn("second", WithRateLimit(key, interval))

	entries := cap.getEntries()
	require.Len(t, entries, 2)
	assert.Equal(t, "second", entries[1].Message)
	args := entries[1].Args
	var found bool
	for i := 0; i < len(args)-1; i += 2 {
		if k, ok := args[i].(string); ok && k == "suppressed_count" {
			assert.Equal(t, int64(3), args[i+1])
			found = true
			break
		}
	}
	assert.True(t, found, "suppressed_count field not found in args")
}

func TestCounterHook_IsCalledOnEmit(t *testing.T) {
	cap := newCapturingHandler()
	var incName atomic.Value
	hook := &mockCounterHook{inc: func(name string) { incName.Store(name) }}
	log := New(Config{Handler: cap}, WithCounterHook(hook)).(*SlogLogger)

	log.Info("msg", WithCounter("my_metric"))

	entries := cap.getEntries()
	require.Len(t, entries, 1)
	assert.Equal(t, "msg", entries[0].Message)
	assert.Equal(t, "my_metric", incName.Load())
}

func TestCounterHook_NotCalledWhenSuppressed(t *testing.T) {
	cap := newCapturingHandler()
	var count int32
	hook := &mockCounterHook{inc: func(string) { atomic.AddInt32(&count, 1) }}
	log := New(Config{Handler: cap}, WithCounterHook(hook)).(*SlogLogger)

	key := "hook_key"
	interval := 100 * time.Millisecond
	log.Info("first", WithRateLimit(key, interval), WithCounter("c"))
	log.Info("second", WithRateLimit(key, interval), WithCounter("c"))
	log.Info("third", WithRateLimit(key, interval), WithCounter("c"))

	require.Len(t, cap.getEntries(), 1)
	assert.Equal(t, int32(1), atomic.LoadInt32(&count))
}

func TestNoOptions_BehaviorUnchanged(t *testing.T) {
	cap := newCapturingHandler()
	log := New(Config{Handler: cap}).(*SlogLogger)

	log.Info("a")
	log.WarnContext(context.Background(), "b", "k", "v")
	log.Error("c", "x", 1)

	entries := cap.getEntries()
	require.Len(t, entries, 3)
	assert.Equal(t, "a", entries[0].Message)
	assert.Equal(t, "b", entries[1].Message)
	assert.Equal(t, "c", entries[2].Message)
	assert.Equal(t, []any{"k", "v"}, stripComponent(entries[1].Args))
	assert.Equal(t, []any{"x", int64(1)}, stripComponent(entries[2].Args))
}

func TestThreadSafety_NoRace(t *testing.T) {
	cap := newCapturingHandler()
	log := New(Config{Handler: cap}).(*SlogLogger)

	const goroutines = 20
	const logsPerGoroutine = 50
	interval := 200 * time.Millisecond

	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			key := "concurrent_key"
			for i := 0; i < logsPerGoroutine; i++ {
				log.Warn("msg", WithRateLimit(key, interval), "id", id, "i", i)
			}
		}(g)
	}
	wg.Wait()

	require.GreaterOrEqual(t, len(cap.getEntries()), 1)
}

// TestNormalPath_WithoutRateLimit verifies the logger works normally when WithRateLimit is never used.
func TestNormalPath_WithoutRateLimit(t *testing.T) {
	cap := newCapturingHandler()
	log := New(Config{Handler: cap}).(*SlogLogger)

	log.Info("hello")
	log.Warn("world", "key", "value")

	got := cap.getEntries()
	require.Len(t, got, 2)
	assert.Equal(t, "hello", got[0].Message)
	assert.Equal(t, slog.LevelInfo, got[0].Level)
	assert.Equal(t, "world", got[1].Message)
	assert.Equal(t, slog.LevelWarn, got[1].Level)
	assert.Equal(t, []any{"key", "value"}, stripComponent(got[1].Args))
}

// TestExtractLogOptions_NormalFieldsNotLost verifies that normal key-value args are never dropped when options are present.
func TestExtractLogOptions_NormalFieldsNotLost(t *testing.T) {
	cap := newCapturingHandler()
	log := New(Config{Handler: cap}).(*SlogLogger)

	log.Info("msg",
		"a", 1,
		WithRateLimit("r", time.Minute),
		"b", 2,
		WithCounter("c"),
		"d", 3,
	)

	entries := cap.getEntries()
	require.Len(t, entries, 1)
	args := stripComponent(entries[0].Args)
	// Must contain exactly the normal fields a=1, b=2, d=3 (options stripped, order preserved).
	assert.Equal(t, []any{"a", int64(1), "b", int64(2), "d", int64(3)}, args)
}

// TestExtractLogOptions_OrderPreserved verifies that the order of args is unchanged after stripping options.
func TestExtractLogOptions_OrderPreserved(t *testing.T) {
	cap := newCapturingHandler()
	log := New(Config{Handler: cap}).(*SlogLogger)

	log.Warn("msg", "first", 1, "second", 2, WithRateLimit("k", time.Second), "third", 3)

	entries := cap.getEntries()
	require.Len(t, entries, 1)
	assert.Equal(t, []any{"first", int64(1), "second", int64(2), "third", int64(3)}, stripComponent(entries[0].Args))
}

// TestExtractLogOptions_ChainedWithNoDuplication verifies that chained With() calls do not duplicate args.
func TestExtractLogOptions_ChainedWithNoDuplication(t *testing.T) {
	cap := newCapturingHandler()
	base := New(Config{Handler: cap}).(*SlogLogger)

	log := base.With("x", 1).With(WithRateLimit("r", time.Hour)).With("y", 2).(*SlogLogger)
	log.Info("m")

	entries := cap.getEntries()
	require.Len(t, entries, 1)
	// Underlying slog accumulates attrs from each With; we must see x=1, y=2 without duplication.
	args := stripComponent(entries[0].Args)
	assert.Len(t, args, 4, "expected exactly 4 args (x, 1, y, 2)")
	assert.Equal(t, []any{"x", int64(1), "y", int64(2)}, args)
}

type mockCounterHook struct {
	inc func(name string)
}

func (m *mockCounterHook) Inc(name string) {
	if m.inc != nil {
		m.inc(name)
	}
}
