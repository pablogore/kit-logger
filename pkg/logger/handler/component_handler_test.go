package handler_test

import (
	"context"
	"log/slog"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/pablogore/kit-logger/pkg/logger/handler"
	"github.com/pablogore/kit-logger/pkg/logger/handler/testdata"
	"github.com/pablogore/kit-logger/pkg/logger/handler/testdata/fixtures"
	"github.com/pablogore/kit-logger/pkg/logger/kitlogtest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// component is the flattened shape of the "component" group that
// ComponentHandler adds.
type component struct {
	found bool
	file  string
	line  int
	fn    string
}

// componentOf extracts the "component" group from a record, if present.
// WithGroup correctly nests attrs added after it was called -- including the
// "component" group ComponentHandler adds inside Handle -- so this searches
// every nesting depth rather than only the top level.
func componentOf(t *testing.T, record slog.Record) component {
	t.Helper()

	var got component
	var walk func(attrs []slog.Attr) bool
	walk = func(attrs []slog.Attr) bool {
		for _, a := range attrs {
			if a.Key == "component" {
				got.found = true
				for _, attr := range a.Value.Group() {
					switch attr.Key {
					case "file":
						got.file = attr.Value.String()
					case "line":
						got.line = int(attr.Value.Int64())
					case "func":
						got.fn = attr.Value.String()
					default:
						t.Fatalf("unexpected component field %q", attr.Key)
					}
				}
				return true
			}
			if a.Value.Kind() == slog.KindGroup && walk(a.Value.Group()) {
				return true
			}
		}
		return false
	}

	var top []slog.Attr
	record.Attrs(func(a slog.Attr) bool {
		top = append(top, a)
		return true
	})
	walk(top)
	return got
}

// captureSink returns a sink handler plus an accessor for the records it received.
// The accessor is safe to call after Flush of an upstream BufferedHandler.
func captureSink() (slog.Handler, func() []slog.Record) {
	records := make(chan slog.Record, 64)
	sink := kitlogtest.NewTestHandler(func(_ context.Context, r slog.Record) {
		records <- r
	})
	return sink, func() []slog.Record {
		var out []slog.Record
		for {
			select {
			case r := <-records:
				out = append(out, r)
			default:
				return out
			}
		}
	}
}

// logHere emits one record and returns the file base name and line the record
// must be attributed to.
func logHere(logger *slog.Logger, msg string) (file string, line int) {
	_, self, at, _ := runtime.Caller(0)
	logger.Info(msg)
	return filepath.Base(self), at + 1
}

// passthroughHandler is a decorator that only forwards, used to prove that
// attribution is independent of how deep ComponentHandler sits in the chain.
type passthroughHandler struct{ next slog.Handler }

func (h passthroughHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

func (h passthroughHandler) Handle(ctx context.Context, r slog.Record) error {
	return h.next.Handle(ctx, r)
}

func (h passthroughHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return passthroughHandler{next: h.next.WithAttrs(attrs)}
}

func (h passthroughHandler) WithGroup(name string) slog.Handler {
	return passthroughHandler{next: h.next.WithGroup(name)}
}

// disabledHandler reports every level as disabled while still recording what
// it is handed.
type disabledHandler struct {
	next    slog.Handler
	enabled bool
	handled int
}

func (h *disabledHandler) Enabled(context.Context, slog.Level) bool { return h.enabled }

func (h *disabledHandler) Handle(ctx context.Context, r slog.Record) error {
	h.handled++
	return h.next.Handle(ctx, r)
}

func (h *disabledHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &disabledHandler{next: h.next.WithAttrs(attrs), enabled: h.enabled}
}

func (h *disabledHandler) WithGroup(name string) slog.Handler {
	return &disabledHandler{next: h.next.WithGroup(name), enabled: h.enabled}
}

func TestComponentHandler_AttributesTheCallSite(t *testing.T) {
	sink, recorded := captureSink()
	logger := slog.New(handler.NewComponentHandler(sink))

	file, line := testdata.LogInvoker{}.Invoke(logger)

	records := recorded()
	require.Len(t, records, 1)
	got := componentOf(t, records[0])
	require.True(t, got.found, "component group not found")
	assert.Equal(t, file, got.file)
	assert.Equal(t, line, got.line)
	assert.Equal(t, "testdata.deeperCall", got.fn)
}

// TestComponentHandler_AttributionSurvivesBuffering covers the original defect:
// with a BufferedHandler upstream, ComponentHandler.Handle runs on the worker
// goroutine, whose stack holds no application frame.
func TestComponentHandler_AttributionSurvivesBuffering(t *testing.T) {
	sink, recorded := captureSink()
	buffered := handler.NewBufferedHandler(handler.NewComponentHandler(sink), 64)
	t.Cleanup(func() { _ = buffered.Shutdown(context.Background()) })

	logger := slog.New(buffered)
	file, line := logHere(logger, "buffered attribution")
	require.NoError(t, buffered.Flush(context.Background()))

	records := recorded()
	require.Len(t, records, 1)
	got := componentOf(t, records[0])
	require.True(t, got.found, "component group not found")
	assert.Equal(t, file, got.file)
	assert.Equal(t, line, got.line)
}

// TestComponentHandler_AttributionIdenticalAcrossBufferSizes proves the
// attribution of one call site does not depend on the delivery mode.
func TestComponentHandler_AttributionIdenticalAcrossBufferSizes(t *testing.T) {
	syncSink, syncRecorded := captureSink()
	syncLogger := slog.New(handler.NewComponentHandler(syncSink))

	bufferedSink, bufferedRecorded := captureSink()
	buffered := handler.NewBufferedHandler(handler.NewComponentHandler(bufferedSink), 1024)
	t.Cleanup(func() { _ = buffered.Shutdown(context.Background()) })
	bufferedLogger := slog.New(buffered)

	// Both calls must originate from the same source line.
	emit := func(logger *slog.Logger) { logger.Info("same call site") }
	emit(syncLogger)
	emit(bufferedLogger)
	require.NoError(t, buffered.Flush(context.Background()))

	syncRecords := syncRecorded()
	bufferedRecords := bufferedRecorded()
	require.Len(t, syncRecords, 1)
	require.Len(t, bufferedRecords, 1)

	got := componentOf(t, syncRecords[0])
	require.True(t, got.found, "component group not found")
	assert.Equal(t, got, componentOf(t, bufferedRecords[0]))
}

// TestComponentHandler_AttributesAnyPathShape covers the removed denylist: the
// directories internal/, pkg/ and cmd/ and the file names service.go,
// handler.go, controller.go, middleware.go and client.go are ordinary
// application code and must be attributed.
func TestComponentHandler_AttributesAnyPathShape(t *testing.T) {
	for _, fixture := range fixtures.All() {
		t.Run(fixture.Name, func(t *testing.T) {
			sink, recorded := captureSink()
			logger := slog.New(handler.NewComponentHandler(sink))

			file, line := fixture.Log(logger)

			records := recorded()
			require.Len(t, records, 1)
			got := componentOf(t, records[0])
			require.True(t, got.found, "component group not found")
			assert.Equal(t, file, got.file)
			assert.Equal(t, line, got.line)
		})
	}
}

// TestComponentHandler_NoMessageSpecialCase covers the removed GraphQL message
// special case: no message prefix may bypass attribution.
func TestComponentHandler_NoMessageSpecialCase(t *testing.T) {
	messages := []string{
		"GraphQL Error(s) Found",
		"GraphQL HTTP Trace",
		"ordinary message",
	}

	var got []component
	for _, msg := range messages {
		sink, recorded := captureSink()
		logger := slog.New(handler.NewComponentHandler(sink))

		file, line := logHere(logger, msg)

		records := recorded()
		require.Len(t, records, 1)
		c := componentOf(t, records[0])
		require.True(t, c.found, "component group not found for %q", msg)
		assert.Equal(t, file, c.file)
		assert.Equal(t, line, c.line)
		got = append(got, c)
	}

	for _, c := range got[1:] {
		assert.Equal(t, got[0], c, "attribution must not depend on the message")
	}
}

// TestComponentHandler_NoComponentGroupWithoutPC covers records built by hand,
// which carry PC == 0. Silence is honest; a "unknown" value is noise.
func TestComponentHandler_NoComponentGroupWithoutPC(t *testing.T) {
	sink, recorded := captureSink()
	h := handler.NewComponentHandler(sink)

	record := slog.NewRecord(time.Now(), slog.LevelInfo, "hand built", 0)
	require.NoError(t, h.Handle(context.Background(), record))

	records := recorded()
	require.Len(t, records, 1)
	assert.False(t, componentOf(t, records[0]).found, "no component group expected")
}

// TestComponentHandler_ForwardsWhenNextIsDisabled covers the removed Enabled
// short-circuit: a record that reached Handle was already accepted upstream, so
// dropping it here would make delivered + dropped != produced.
func TestComponentHandler_ForwardsWhenNextIsDisabled(t *testing.T) {
	sink, recorded := captureSink()
	next := &disabledHandler{next: sink, enabled: false}
	h := handler.NewComponentHandler(next)

	assert.False(t, h.Enabled(context.Background(), slog.LevelError), "Enabled must delegate")

	record := slog.NewRecord(time.Now(), slog.LevelInfo, "already accepted", 0)
	require.NoError(t, h.Handle(context.Background(), record))

	assert.Equal(t, 1, next.handled, "the record must still be forwarded")
	require.Len(t, recorded(), 1)
}

// TestComponentHandler_AttributionThroughDecoratorChain proves attribution is
// independent of the depth of the handler chain.
func TestComponentHandler_AttributionThroughDecoratorChain(t *testing.T) {
	sink, recorded := captureSink()

	var h slog.Handler = handler.NewComponentHandler(sink)
	for i := 0; i < 5; i++ {
		h = passthroughHandler{next: h}
	}
	logger := slog.New(h)

	file, line := logHere(logger, "through five decorators")

	records := recorded()
	require.Len(t, records, 1)
	got := componentOf(t, records[0])
	require.True(t, got.found, "component group not found")
	assert.Equal(t, file, got.file)
	assert.Equal(t, line, got.line)
}

// TestComponentHandler_AttributionThroughWithDerivation proves a logger derived
// with With is still attributed to the consumer call site.
func TestComponentHandler_AttributionThroughWithDerivation(t *testing.T) {
	sink, recorded := captureSink()
	logger := slog.New(handler.NewComponentHandler(sink)).With("request_id", "abc")

	file, line := logHere(logger, "derived logger")

	records := recorded()
	require.Len(t, records, 1)
	got := componentOf(t, records[0])
	require.True(t, got.found, "component group not found")
	assert.Equal(t, file, got.file)
	assert.Equal(t, line, got.line)
}

// TestComponentHandler_FuncNeverNamesTheLibrary is the invariant engineers rely
// on: the attributed function is consumer code, never slog or kit-logger.
func TestComponentHandler_FuncNeverNamesTheLibrary(t *testing.T) {
	sink, recorded := captureSink()
	logger := slog.New(handler.NewComponentHandler(sink))

	_, _ = logHere(logger, "attribution target")
	_, _ = testdata.LogInvoker{}.Invoke(logger)

	records := recorded()
	require.Len(t, records, 2)
	for _, record := range records {
		got := componentOf(t, record)
		require.True(t, got.found, "component group not found")
		for _, forbidden := range []string{"slog.", "kit-logger", "ComponentHandler"} {
			assert.NotContains(t, got.fn, forbidden)
		}
	}
}

func TestComponentHandler_WithAttrs(t *testing.T) {
	sink, recorded := captureSink()
	logger := slog.New(handler.NewComponentHandler(sink))

	logger.With("service", "orders").Info("with attrs")

	records := recorded()
	require.Len(t, records, 1)
	require.True(t, componentOf(t, records[0]).found, "component group not found")

	var attrs []string
	records[0].Attrs(func(a slog.Attr) bool {
		attrs = append(attrs, a.Key)
		return true
	})
	assert.Contains(t, attrs, "service")
}

func TestComponentHandler_WithGroup(t *testing.T) {
	sink, recorded := captureSink()
	logger := slog.New(handler.NewComponentHandler(sink))

	logger.WithGroup("request").Info("with group", "id", "42")

	records := recorded()
	require.Len(t, records, 1)
	assert.True(t, componentOf(t, records[0]).found, "component group not found")
}

// TestComponentHandler_WithOffsetIsANoOp pins the deprecated compatibility
// shim: it must keep compiling and must not change attribution.
func TestComponentHandler_WithOffsetIsANoOp(t *testing.T) {
	sink, recorded := captureSink()
	base, ok := handler.NewComponentHandler(sink).(*handler.ComponentHandler)
	require.True(t, ok, "NewComponentHandler must return *ComponentHandler")

	for _, offset := range []int{-3, 0, 7} {
		logger := slog.New(base.WithOffset(offset))
		file, line := logHere(logger, "offset is ignored")

		records := recorded()
		require.Len(t, records, 1)
		got := componentOf(t, records[0])
		require.True(t, got.found, "component group not found")
		assert.Equal(t, file, got.file)
		assert.Equal(t, line, got.line)
	}
}

// TestComponentHandler_NoLiveStackWalk guards the implementation choice: the
// resolved frame comes from record.PC, so a record whose PC was captured in a
// completely different goroutine still resolves to that goroutine's call site.
func TestComponentHandler_NoLiveStackWalk(t *testing.T) {
	sink, recorded := captureSink()
	h := handler.NewComponentHandler(sink)

	type captured struct {
		pc   uintptr
		file string
		line int
	}
	result := make(chan captured, 1)
	go func() {
		var pcs [1]uintptr
		runtime.Callers(1, pcs[:])
		_, file, line, _ := runtime.Caller(0)
		result <- captured{pc: pcs[0], file: filepath.Base(file), line: line - 1}
	}()
	origin := <-result

	record := slog.NewRecord(time.Now(), slog.LevelInfo, "from another goroutine", origin.pc)
	require.NoError(t, h.Handle(context.Background(), record))

	records := recorded()
	require.Len(t, records, 1)
	got := componentOf(t, records[0])
	require.True(t, got.found, "component group not found")
	assert.Equal(t, origin.file, got.file)
	assert.Equal(t, origin.line, got.line)
	assert.True(t, strings.HasSuffix(got.fn, "func1"), "unexpected func %q", got.fn)
}
