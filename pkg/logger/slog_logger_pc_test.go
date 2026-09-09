package logger

import (
	"context"
	"log/slog"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pablogore/kit-logger/pkg/logger/handler"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// gateHandler is a sink whose Enabled answer is controllable, so the tests can
// observe what the facade pays for on a disabled level.
type gateHandler struct {
	enabled  atomic.Bool
	attrs    []slog.Attr
	records  chan slog.Record
	enabledN atomic.Int64
}

func newGateHandler() *gateHandler {
	h := &gateHandler{records: make(chan slog.Record, 64)}
	h.enabled.Store(true)
	return h
}

func (h *gateHandler) Enabled(context.Context, slog.Level) bool {
	h.enabledN.Add(1)
	return h.enabled.Load()
}

func (h *gateHandler) Handle(_ context.Context, r slog.Record) error {
	if len(h.attrs) > 0 {
		r.AddAttrs(h.attrs...)
	}
	h.records <- r
	return nil
}

func (h *gateHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	derived := &gateHandler{records: h.records, attrs: append(append([]slog.Attr{}, h.attrs...), attrs...)}
	derived.enabled.Store(h.enabled.Load())
	return derived
}

func (h *gateHandler) WithGroup(string) slog.Handler { return h }

// drain returns every record delivered so far.
func (h *gateHandler) drain() []slog.Record {
	var out []slog.Record
	for {
		select {
		case r := <-h.records:
			out = append(out, r)
		default:
			return out
		}
	}
}

// componentGroup is the flattened "component" group added by ComponentHandler.
type componentGroup struct {
	found bool
	file  string
	line  int
	fn    string
}

func componentGroupOf(record slog.Record) componentGroup {
	var got componentGroup
	record.Attrs(func(a slog.Attr) bool {
		if a.Key != "component" {
			return true
		}
		got.found = true
		for _, attr := range a.Value.Group() {
			switch attr.Key {
			case "file":
				got.file = attr.Value.String()
			case "line":
				got.line = int(attr.Value.Int64())
			case "func":
				got.fn = attr.Value.String()
			}
		}
		return false
	})
	return got
}

// newFacade builds a Logger whose pipeline is ComponentHandler over a
// controllable sink, optionally behind a BufferedHandler.
func newFacade(t *testing.T, bufferSize int) (Logger, *gateHandler, func()) {
	t.Helper()

	sink := newGateHandler()
	var h slog.Handler = handler.NewComponentHandler(sink)

	flush := func() {}
	if bufferSize > 0 {
		buffered := handler.NewBufferedHandler(h, bufferSize)
		t.Cleanup(func() { _ = buffered.Shutdown(context.Background()) })
		flush = func() { require.NoError(t, buffered.Flush(context.Background())) }
		h = buffered
	}

	return New(Config{Level: "debug", Handler: h}), sink, flush
}

// thisFile is the base name of this test file, the expected attribution target
// for every call made from it.
var thisFile = func() string {
	_, self, _, _ := runtime.Caller(0)
	return filepath.Base(self)
}()

// facadeCall invokes one exported facade method and returns the source line the
// resulting record must be attributed to.
type facadeCall struct {
	name string
	call func(l Logger) int
}

// facadeCalls covers every exported emit method plus the two derivation paths.
// Each closure reads its own line with runtime.Caller(0) and logs on the very
// next line, so the expectation cannot drift when this file is edited.
func facadeCalls() []facadeCall {
	return []facadeCall{
		{"Debug", func(l Logger) int {
			_, _, line, _ := runtime.Caller(0)
			l.Debug("message")
			return line + 1
		}},
		{"Info", func(l Logger) int {
			_, _, line, _ := runtime.Caller(0)
			l.Info("message")
			return line + 1
		}},
		{"Warn", func(l Logger) int {
			_, _, line, _ := runtime.Caller(0)
			l.Warn("message")
			return line + 1
		}},
		{"Error", func(l Logger) int {
			_, _, line, _ := runtime.Caller(0)
			l.Error("message")
			return line + 1
		}},
		{"DebugContext", func(l Logger) int {
			_, _, line, _ := runtime.Caller(0)
			l.DebugContext(context.Background(), "message")
			return line + 1
		}},
		{"InfoContext", func(l Logger) int {
			_, _, line, _ := runtime.Caller(0)
			l.InfoContext(context.Background(), "message")
			return line + 1
		}},
		{"WarnContext", func(l Logger) int {
			_, _, line, _ := runtime.Caller(0)
			l.WarnContext(context.Background(), "message")
			return line + 1
		}},
		{"ErrorContext", func(l Logger) int {
			_, _, line, _ := runtime.Caller(0)
			l.ErrorContext(context.Background(), "message")
			return line + 1
		}},
		{"Log", func(l Logger) int {
			_, _, line, _ := runtime.Caller(0)
			l.Log(context.Background(), slog.LevelInfo, "message")
			return line + 1
		}},
		{"With", func(l Logger) int {
			derived := l.With("request_id", "abc")
			_, _, line, _ := runtime.Caller(0)
			derived.Info("message")
			return line + 1
		}},
		{"WithContext", func(l Logger) int {
			derived := l.WithContext(context.Background())
			_, _, line, _ := runtime.Caller(0)
			derived.Info("message")
			return line + 1
		}},
	}
}

// TestSlogLogger_AttributesTheConsumerCallSite pins the runtime.Callers skip
// depth: every exported method must attribute to its own caller, never to the
// facade or to an internal closure.
func TestSlogLogger_AttributesTheConsumerCallSite(t *testing.T) {
	restore := globalContextFieldExtractor
	globalContextFieldExtractor = func(context.Context) []any { return []any{"tenant", "acme"} }
	t.Cleanup(func() { globalContextFieldExtractor = restore })

	for _, bufferSize := range []int{0, 1024} {
		for _, tc := range facadeCalls() {
			t.Run(tc.name, func(t *testing.T) {
				log, sink, flush := newFacade(t, bufferSize)

				expectedLine := tc.call(log)
				flush()

				records := sink.drain()
				require.Len(t, records, 1)
				got := componentGroupOf(records[0])
				require.True(t, got.found, "component group not found")
				assert.Equal(t, thisFile, got.file)
				assert.Equal(t, expectedLine, got.line)
				for _, forbidden := range []string{"slog.", "kit-logger", "ComponentHandler", "SlogLogger"} {
					assert.NotContains(t, got.fn, forbidden)
				}
			})
		}
	}
}

// TestSlogLogger_AttributionIdenticalAcrossBufferSizes proves the facade call
// site attribution is the same synchronously and behind a buffer.
func TestSlogLogger_AttributionIdenticalAcrossBufferSizes(t *testing.T) {
	syncLog, syncSink, _ := newFacade(t, 0)
	bufferedLog, bufferedSink, flush := newFacade(t, 1024)

	emit := func(l Logger) { l.Info("same call site") }
	emit(syncLog)
	emit(bufferedLog)
	flush()

	syncRecords := syncSink.drain()
	bufferedRecords := bufferedSink.drain()
	require.Len(t, syncRecords, 1)
	require.Len(t, bufferedRecords, 1)

	got := componentGroupOf(syncRecords[0])
	require.True(t, got.found, "component group not found")
	assert.Equal(t, got, componentGroupOf(bufferedRecords[0]))
}

// TestSlogLogger_WithCarriesBakedAttrs proves that emitting through the
// handler chain still carries the attrs slog.Logger.With baked in.
func TestSlogLogger_WithCarriesBakedAttrs(t *testing.T) {
	log, sink, _ := newFacade(t, 0)

	log.With("request_id", "abc").Info("derived", "extra", 1)

	records := sink.drain()
	require.Len(t, records, 1)

	got := map[string]string{}
	records[0].Attrs(func(a slog.Attr) bool {
		got[a.Key] = a.Value.String()
		return true
	})
	assert.Equal(t, "abc", got["request_id"])
	assert.Equal(t, "1", got["extra"])
}

// TestSlogLogger_ArgsNormalizationMatchesSlog proves the record built by the
// facade normalizes args exactly as slog.Logger does: key/value pairs, a
// dangling key, pre-built slog.Attr values and mixtures of them.
func TestSlogLogger_ArgsNormalizationMatchesSlog(t *testing.T) {
	cases := []struct {
		name string
		args []any
	}{
		{"pairs", []any{"a", 1, "b", "two"}},
		{"dangling key", []any{"a", 1, "dangling"}},
		{"prebuilt attrs", []any{slog.String("a", "1"), slog.Int("b", 2)}},
		{"mixed", []any{slog.String("a", "1"), "b", 2, "dangling"}},
		{"empty group", []any{slog.Group("empty"), "a", 1}},
		{"no args", nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			log, sink, _ := newFacade(t, 0)

			log.Info("normalization", tc.args...)
			log.Slog().Info("normalization", tc.args...)

			records := sink.drain()
			require.Len(t, records, 2)
			assert.Equal(t, attrStrings(records[1]), attrStrings(records[0]),
				"facade args must normalize exactly as slog.Logger does")
		})
	}
}

// attrStrings renders a record's attrs, skipping the component group, whose
// line legitimately differs between the two emit paths.
func attrStrings(record slog.Record) []string {
	var out []string
	record.Attrs(func(a slog.Attr) bool {
		if a.Key == "component" {
			return true
		}
		out = append(out, a.Key+"="+a.Value.String())
		return true
	})
	return out
}

// TestSlogLogger_DisabledLevelPaysNothing proves the facade gates on Enabled
// before it pays for PC capture, timestamps, attr normalization, the rate
// machinery or the counter hook.
func TestSlogLogger_DisabledLevelPaysNothing(t *testing.T) {
	sink := newGateHandler()
	counter := &countingHook{}
	log := New(Config{Level: "debug", Handler: handler.NewComponentHandler(sink)}, WithCounterHook(counter))
	slogLog, ok := log.(*SlogLogger)
	require.True(t, ok)

	sink.enabled.Store(false)
	for i := 0; i < 5; i++ {
		log.Info("suppressed",
			WithRateLimit("shared-key", time.Hour),
			WithCounter("suppressed_metric"))
	}

	assert.Empty(t, sink.drain(), "a disabled level must emit nothing")
	assert.Zero(t, counter.total(), "the counter hook must not fire on a disabled level")
	slogLog.rateState.mu.Lock()
	assert.Empty(t, slogLog.rateState.limiters, "no rate-limit state may be created on a disabled level")
	slogLog.rateState.mu.Unlock()

	// The rate-limit token was never consumed, so the first enabled call passes.
	sink.enabled.Store(true)
	log.Info("emitted", WithRateLimit("shared-key", time.Hour), WithCounter("emitted_metric"))
	assert.Len(t, sink.drain(), 1)
	assert.Equal(t, 1, counter.total())
}

// TestSlogLogger_NilContextIsTolerated pins the nil-context normalization that
// slog.Logger performs for us today: the Context methods must not panic and
// must still attribute to the consumer call site.
func TestSlogLogger_NilContextIsTolerated(t *testing.T) {
	calls := []struct {
		name string
		call func(l Logger) int
	}{
		{"DebugContext", func(l Logger) int {
			_, _, line, _ := runtime.Caller(0)
			l.DebugContext(nil, "message") //nolint:staticcheck // nil context is the case under test
			return line + 1
		}},
		{"InfoContext", func(l Logger) int {
			_, _, line, _ := runtime.Caller(0)
			l.InfoContext(nil, "message") //nolint:staticcheck // nil context is the case under test
			return line + 1
		}},
		{"WarnContext", func(l Logger) int {
			_, _, line, _ := runtime.Caller(0)
			l.WarnContext(nil, "message") //nolint:staticcheck // nil context is the case under test
			return line + 1
		}},
		{"ErrorContext", func(l Logger) int {
			_, _, line, _ := runtime.Caller(0)
			l.ErrorContext(nil, "message") //nolint:staticcheck // nil context is the case under test
			return line + 1
		}},
		{"Log", func(l Logger) int {
			_, _, line, _ := runtime.Caller(0)
			l.Log(nil, slog.LevelInfo, "message") //nolint:staticcheck // nil context is the case under test
			return line + 1
		}},
	}

	for _, tc := range calls {
		t.Run(tc.name, func(t *testing.T) {
			log, sink, _ := newFacade(t, 0)

			expectedLine := tc.call(log)

			records := sink.drain()
			require.Len(t, records, 1)
			got := componentGroupOf(records[0])
			require.True(t, got.found, "component group not found")
			assert.Equal(t, thisFile, got.file)
			assert.Equal(t, expectedLine, got.line)
		})
	}
}

// TestSlogLogger_DisabledLevelEmitsNothing covers the early Enabled gate on
// every exported method.
func TestSlogLogger_DisabledLevelEmitsNothing(t *testing.T) {
	for _, tc := range facadeCalls() {
		t.Run(tc.name, func(t *testing.T) {
			sink := newGateHandler()
			log := New(Config{Level: "debug", Handler: handler.NewComponentHandler(sink)})
			sink.enabled.Store(false)

			tc.call(log)

			assert.Empty(t, sink.drain(), "a disabled level must emit nothing")
		})
	}
}

// TestSlogLogger_RateLimitSemanticsPreserved pins the suppressed_count field
// and the counter hook on the enabled path.
func TestSlogLogger_RateLimitSemanticsPreserved(t *testing.T) {
	sink := newGateHandler()
	counter := &countingHook{}
	log := New(Config{Level: "debug", Handler: handler.NewComponentHandler(sink)}, WithCounterHook(counter))

	for i := 0; i < 3; i++ {
		log.Info("rate limited", WithRateLimit("key", time.Hour), WithCounter("metric"))
	}
	first := sink.drain()
	require.Len(t, first, 1, "only the first call of the interval is emitted")
	assert.Equal(t, 1, counter.total(), "the counter hook fires once per emitted record")

	// A new key emits immediately and reports the suppressions of its own key.
	log.Info("other key", WithRateLimit("other", time.Millisecond))
	time.Sleep(2 * time.Millisecond)
	log.Info("other key", WithRateLimit("other", time.Millisecond))
	log.Info("other key", WithRateLimit("other", time.Millisecond))
	time.Sleep(2 * time.Millisecond)
	log.Info("other key", WithRateLimit("other", time.Millisecond))

	var withSuppressed int
	for _, record := range sink.drain() {
		record.Attrs(func(a slog.Attr) bool {
			if a.Key == "suppressed_count" {
				withSuppressed++
				assert.Equal(t, int64(1), a.Value.Int64())
			}
			return true
		})
	}
	assert.Equal(t, 1, withSuppressed, "suppressed_count is reported on the next emitted record")
}

// countingHook counts CounterHook invocations.
type countingHook struct {
	n atomic.Int64
}

func (c *countingHook) Inc(string) { c.n.Add(1) }

func (c *countingHook) total() int { return int(c.n.Load()) }
