package logger

import (
	"context"
	"io"
	"log/slog"
	"os"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// attrCapturingHandler is a minimal leaf handler that hands the attrs of every
// record it receives to a callback. Tests in this file cannot use
// kitlogtest.TestHandler: this is an internal (package logger, not
// logger_test) test file, and kitlogtest imports package logger -- importing
// it back here would be a compile-time import cycle.
type attrCapturingHandler struct {
	callback func(context.Context, slog.Record)
	attrs    []slog.Attr
}

func newAttrCapturingHandler(callback func(context.Context, slog.Record)) *attrCapturingHandler {
	return &attrCapturingHandler{callback: callback}
}

func (h *attrCapturingHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *attrCapturingHandler) Handle(ctx context.Context, r slog.Record) error {
	out := slog.NewRecord(r.Time, r.Level, r.Message, r.PC)
	out.AddAttrs(h.attrs...)
	r.Attrs(func(a slog.Attr) bool {
		out.AddAttrs(a)
		return true
	})
	h.callback(ctx, out)
	return nil
}

func (h *attrCapturingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return h
	}
	merged := make([]slog.Attr, 0, len(h.attrs)+len(attrs))
	merged = append(merged, h.attrs...)
	merged = append(merged, attrs...)
	return &attrCapturingHandler{callback: h.callback, attrs: merged}
}

func (h *attrCapturingHandler) WithGroup(_ string) slog.Handler { return h }

// discardHandler is a leaf handler that costs nothing. The concurrency tests
// below hammer the *globals*, so the pipeline underneath must not dominate the
// runtime — otherwise the iteration counts the acceptance criteria ask for
// would buy allocation throughput instead of race coverage.
type discardHandler struct{}

func (discardHandler) Enabled(context.Context, slog.Level) bool  { return true }
func (discardHandler) Handle(context.Context, slog.Record) error { return nil }
func (discardHandler) WithAttrs([]slog.Attr) slog.Handler        { return discardHandler{} }
func (discardHandler) WithGroup(string) slog.Handler             { return discardHandler{} }

// attrByKey finds the first attr with the given key. Config.Handler is
// decorated like any other sink, so a record may carry attrs the pipeline
// adds -- tests that only care about their own attr look it up by key
// instead of assuming a position or an exact count.
func attrByKey(attrs []slog.Attr, key string) (slog.Attr, bool) {
	for _, a := range attrs {
		if a.Key == key {
			return a, true
		}
	}
	return slog.Attr{}, false
}

// TestGlobalLogger_ConcurrentAccess exercises every global entry point from
// many goroutines at once. It is a race-detector test: it asserts almost
// nothing by itself and is meaningless without -race, where it must be clean.
//
// The writes are the point. SetGlobal publishes a two-word interface value and
// SetContextFieldExtractor publishes a func value, both read on the per-log
// path by L() and WithContext.
func TestGlobalLogger_ConcurrentAccess(t *testing.T) {
	const (
		goroutines = 100
		iterations = 10_000
	)

	// Pre-built loggers: constructing one per iteration would measure New,
	// not the global handoff.
	loggers := make([]Logger, 4)
	for i := range loggers {
		loggers[i] = New(Config{Handler: discardHandler{}})
	}

	// The extractor returns no fields on purpose: the race is on reading the
	// variable, not on what the extracted fields cost.
	extractors := []ContextFieldExtractorFunc{
		nil,
		func(context.Context) []any { return nil },
		func(context.Context) []any { return nil },
	}

	SetGlobal(loggers[0])
	ctx := context.Background()

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func(g int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				switch (g + i) % 4 {
				case 0:
					SetGlobal(loggers[i%len(loggers)])
				case 1:
					SetContextFieldExtractor(extractors[i%len(extractors)])
				case 2:
					require.NotNil(t, L())
				default:
					require.NotNil(t, L().WithContext(ctx))
				}
			}
		}(g)
	}
	wg.Wait()

	assert.NotNil(t, L())
}

// TestGlobalLogger_ReturnsTheExactInstanceLastSet pins identity: L() must hand
// back the very logger the most recent completed SetGlobal was given, never a
// copy, a zero value or a torn one.
func TestGlobalLogger_ConcurrentSetGlobalPublishesWholeValues(t *testing.T) {
	const (
		readers    = 100
		iterations = 10_000
	)

	first := New(Config{Handler: discardHandler{}})
	second := New(Config{Handler: discardHandler{}})
	SetGlobal(first)

	stop := make(chan struct{})
	var writers sync.WaitGroup
	writers.Add(1)
	go func() {
		defer writers.Done()
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			if i%2 == 0 {
				SetGlobal(first)
			} else {
				SetGlobal(second)
			}
		}
	}()

	var wg sync.WaitGroup
	wg.Add(readers)
	for r := 0; r < readers; r++ {
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				got := L()
				// A torn interface value would be neither of the two, or
				// would panic on use. Both are published, so every read must
				// land on one of them whole.
				if got != first && got != second {
					require.Failf(t, "torn read", "L() returned %#v", got)
				}
			}
		}()
	}
	wg.Wait()
	close(stop)
	writers.Wait()
}

// withFreshGlobals gives one test a clean slate and puts back whatever the
// rest of the package had installed. The globals are shared process state, so
// a test that resets them without restoring them fails its neighbours instead
// of itself.
func withFreshGlobals(t *testing.T) {
	t.Helper()

	previousLogger := peekGlobalForTest()
	previousExtractor := contextFieldExtractor()
	t.Cleanup(func() {
		restoreGlobalForTest(previousLogger)
		SetContextFieldExtractor(previousExtractor)
	})

	resetGlobalsForTest()
}

// TestGlobalLogger_LazyInitHappensExactlyOnce pins the lost update in the old
// check-then-write L(): two goroutines could both see nil, both construct a
// logger, and both assign — discarding one, and emitting its startup line
// anyway. The startup line is what makes the defect observable, so it is what
// the test counts.
func TestGlobalLogger_LazyInitHappensExactlyOnce(t *testing.T) {
	withFreshGlobals(t)

	realStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	const goroutines = 100
	got := make([]Logger, goroutines)
	start := make(chan struct{})

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(i int) {
			defer wg.Done()
			<-start
			got[i] = L()
		}(i)
	}
	close(start)
	wg.Wait()

	require.NoError(t, w.Close())
	os.Stdout = realStdout
	output, err := io.ReadAll(r)
	require.NoError(t, err)

	for i, l := range got {
		require.NotNil(t, l, "goroutine %d got no logger", i)
		assert.Same(t, got[0], l, "goroutine %d got a different logger instance", i)
	}
	assert.Empty(t, string(output),
		"the lazy default must not write to stdout because a consumer touched a global")
}

func TestGlobalLogger_ReturnsTheExactInstanceLastSet(t *testing.T) {
	withFreshGlobals(t)

	first := New(Config{Handler: discardHandler{}})
	second := New(Config{Handler: discardHandler{}})

	SetGlobal(first)
	assert.Same(t, first, L())

	SetGlobal(second)
	assert.Same(t, second, L())
}

func TestSetGlobal_PanicsOnNil(t *testing.T) {
	withFreshGlobals(t)

	assert.PanicsWithValue(t, "kit-logger: SetGlobal called with a nil Logger", func() {
		SetGlobal(nil)
	})
	assert.Nil(t, peekGlobalForTest(), "a rejected SetGlobal must install nothing")
}

// TestSetGlobal_LeavesSlogDefaultAlone pins the removed side effect: setting
// this library's global must not reconfigure the standard library for the whole
// process.
func TestSetGlobal_LeavesSlogDefaultAlone(t *testing.T) {
	withFreshGlobals(t)

	previousDefault := slog.Default()
	t.Cleanup(func() { slog.SetDefault(previousDefault) })

	sentinel := slog.New(discardHandler{})
	slog.SetDefault(sentinel)

	SetGlobal(New(Config{Handler: discardHandler{}}))

	assert.Same(t, sentinel, slog.Default(),
		"SetGlobal must not touch slog.Default")
}

func TestSetGlobalAndSlogDefault_InstallsBoth(t *testing.T) {
	withFreshGlobals(t)

	previousDefault := slog.Default()
	t.Cleanup(func() { slog.SetDefault(previousDefault) })

	log := New(Config{Handler: discardHandler{}})
	SetGlobalAndSlogDefault(log)

	assert.Same(t, log, L())
	assert.Same(t, log.Slog(), slog.Default())
}

// TestWithContext_PerInstanceExtractorIgnoresTheGlobal covers both halves of
// the contract: the per-instance extractor is the one that runs, and the path
// reads no package-level state at all — proved by making the global extractor
// fail the test if it is ever consulted.
func TestWithContext_PerInstanceExtractorIgnoresTheGlobal(t *testing.T) {
	withFreshGlobals(t)

	var seen []slog.Attr
	base := newAttrCapturingHandler(func(_ context.Context, record slog.Record) {
		record.Attrs(func(a slog.Attr) bool {
			seen = append(seen, a)
			return true
		})
	})

	log := New(Config{
		Handler:       base,
		ContextFields: func(context.Context) []any { return []any{"tenant", "acme"} },
	})

	SetContextFieldExtractor(func(context.Context) []any {
		require.Fail(t, "WithContext consulted the process-wide extractor")
		return nil
	})

	log.WithContext(context.Background()).Info("hello")

	tenant, ok := attrByKey(seen, "tenant")
	require.True(t, ok, "tenant attr not found in %v", seen)
	assert.Equal(t, "acme", tenant.Value.String())
}

// A logger without its own extractor still honours the deprecated global, so
// existing consumers keep working.
func TestWithContext_FallsBackToTheGlobalExtractor(t *testing.T) {
	withFreshGlobals(t)

	var seen []slog.Attr
	base := newAttrCapturingHandler(func(_ context.Context, record slog.Record) {
		record.Attrs(func(a slog.Attr) bool {
			seen = append(seen, a)
			return true
		})
	})

	log := New(Config{Handler: base})
	SetContextFieldExtractor(func(context.Context) []any { return []any{"tenant", "globex"} })

	log.WithContext(context.Background()).Info("hello")

	tenant, ok := attrByKey(seen, "tenant")
	require.True(t, ok, "tenant attr not found in %v", seen)
	assert.Equal(t, "globex", tenant.Value.String())
}
