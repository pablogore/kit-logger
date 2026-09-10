package otel_test

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	oteltrace "go.opentelemetry.io/otel/trace"

	"github.com/pablogore/kit-logger/pkg/logger/handler"
	"github.com/pablogore/kit-logger/pkg/logger/kitlogtest"
	kitotel "github.com/pablogore/kit-logger/pkg/logger/otel"
	"github.com/pablogore/kit-logger/pkg/logger/utils"
)

const (
	traceHex = "4bf92f3577b34da6a3ce929d0e0e4736"
	spanHex  = "00f067aa0ba902b7"
)

// spanCtx builds a context carrying a span context directly, with no SDK, no
// exporter and no tracer provider: reading correlation state needs nothing but
// the OTel API module, and the tests say so.
func spanCtx(t *testing.T, sampled bool) context.Context {
	t.Helper()

	traceID, err := oteltrace.TraceIDFromHex(traceHex)
	require.NoError(t, err)
	spanID, err := oteltrace.SpanIDFromHex(spanHex)
	require.NoError(t, err)

	var flags oteltrace.TraceFlags
	if sampled {
		flags = oteltrace.FlagsSampled
	}

	return oteltrace.ContextWithSpanContext(context.Background(), oteltrace.NewSpanContext(oteltrace.SpanContextConfig{
		TraceID:    traceID,
		SpanID:     spanID,
		TraceFlags: flags,
	}))
}

// capture returns a handler that records the last record it saw, plus a getter
// for its attributes.
func capture() (slog.Handler, func() slog.Record) {
	var captured slog.Record
	h := kitlogtest.NewTestHandler(func(_ context.Context, r slog.Record) {
		captured = r
	})
	return h, func() slog.Record { return captured }
}

// attrKeys returns the exact attribute keys of a record, so an assertion can
// prove a field is absent rather than merely empty.
func attrKeys(r slog.Record) []string {
	keys := make([]string, 0, r.NumAttrs())
	r.Attrs(func(a slog.Attr) bool {
		keys = append(keys, a.Key)
		return true
	})
	return keys
}

// groupAttrs reads a slog group out of an extracted attribute map.
func groupAttrs(t *testing.T, fields map[string]any, name string) map[string]any {
	t.Helper()

	group, ok := fields[name].([]slog.Attr)
	require.True(t, ok, "expected %q to be a slog group, got %T", name, fields[name])

	nested := map[string]any{}
	for _, a := range group {
		nested[a.Key] = a.Value.Any()
	}
	return nested
}

func TestHandler_EnrichesRecordWithTraceAndSpanID(t *testing.T) {
	base, last := capture()
	log := slog.New(kitotel.NewHandler(base, kitotel.Options{}))

	log.InfoContext(spanCtx(t, true), "checkout failed", "order_id", "A-1")

	fields := utils.ExtractAttrs(last())
	require.Equal(t, traceHex, fields["trace_id"])
	require.Equal(t, spanHex, fields["span_id"])
	require.Equal(t, "A-1", fields["order_id"])
}

func TestHandler_WithoutSpanAddsNoFields(t *testing.T) {
	base, last := capture()
	log := slog.New(kitotel.NewHandler(base, kitotel.Options{}))

	log.InfoContext(context.Background(), "no trace here", "order_id", "A-1")

	// Asserted on the exact attribute list: a present-but-empty trace_id is
	// still a field consumers have to filter out of their queries.
	require.Equal(t, []string{"order_id"}, attrKeys(last()))
}

func TestHandler_InvalidSpanContextIsTreatedAsNoSpan(t *testing.T) {
	base, last := capture()
	log := slog.New(kitotel.NewHandler(base, kitotel.Options{}))

	// A zero SpanContext is what a context populated by a broken propagator
	// carries. It is not a span, and it must not produce all-zero correlation
	// IDs that silently join unrelated requests.
	ctx := oteltrace.ContextWithSpanContext(context.Background(), oteltrace.SpanContext{})
	log.InfoContext(ctx, "msg")

	require.Empty(t, attrKeys(last()))
}

func TestHandler_NoSpanPathAllocatesNothingExtra(t *testing.T) {
	base, _ := capture()
	wrapped := kitotel.NewHandler(base, kitotel.Options{})

	ctx := context.Background()
	record := slog.NewRecord(time.Now(), slog.LevelInfo, "msg", 0)
	record.AddAttrs(slog.String("order_id", "A-1"))

	baseline := testing.AllocsPerRun(200, func() {
		_ = base.Handle(ctx, record)
	})
	withHandler := testing.AllocsPerRun(200, func() {
		_ = wrapped.Handle(ctx, record)
	})

	require.Equal(t, baseline, withHandler,
		"the no-span path must not allocate: it is on every record of every consumer that has no tracing")
}

func TestHandler_CustomKeys(t *testing.T) {
	base, last := capture()
	log := slog.New(kitotel.NewHandler(base, kitotel.Options{
		TraceIDKey: "traceId",
		SpanIDKey:  "spanId",
	}))

	log.InfoContext(spanCtx(t, true), "msg")

	fields := utils.ExtractAttrs(last())
	require.Equal(t, traceHex, fields["traceId"])
	require.Equal(t, spanHex, fields["spanId"])
	require.NotContains(t, fields, "trace_id")
	require.NotContains(t, fields, "span_id")
}

func TestHandler_GroupNestsBothFields(t *testing.T) {
	base, last := capture()
	log := slog.New(kitotel.NewHandler(base, kitotel.Options{Group: "trace"}))

	log.InfoContext(spanCtx(t, true), "msg")

	fields := utils.ExtractAttrs(last())
	nested := groupAttrs(t, fields, "trace")
	require.Equal(t, traceHex, nested["trace_id"])
	require.Equal(t, spanHex, nested["span_id"])
	require.NotContains(t, fields, "trace_id")
}

func TestHandler_TraceFlags(t *testing.T) {
	t.Run("off by default", func(t *testing.T) {
		base, last := capture()
		log := slog.New(kitotel.NewHandler(base, kitotel.Options{}))

		log.InfoContext(spanCtx(t, true), "msg")

		require.NotContains(t, utils.ExtractAttrs(last()), "trace_flags")
	})

	t.Run("emitted when enabled", func(t *testing.T) {
		base, last := capture()
		log := slog.New(kitotel.NewHandler(base, kitotel.Options{TraceFlags: true}))

		log.InfoContext(spanCtx(t, true), "msg")

		require.Equal(t, "01", utils.ExtractAttrs(last())["trace_flags"])
	})
}

func TestHandler_OnlySampled(t *testing.T) {
	t.Run("enriches an unsampled span by default", func(t *testing.T) {
		base, last := capture()
		log := slog.New(kitotel.NewHandler(base, kitotel.Options{}))

		log.InfoContext(spanCtx(t, false), "msg")

		require.Equal(t, traceHex, utils.ExtractAttrs(last())["trace_id"])
	})

	t.Run("skips an unsampled span when set", func(t *testing.T) {
		base, last := capture()
		log := slog.New(kitotel.NewHandler(base, kitotel.Options{OnlySampled: true}))

		log.InfoContext(spanCtx(t, false), "msg")

		require.Empty(t, attrKeys(last()))
	})

	t.Run("still enriches a sampled span when set", func(t *testing.T) {
		base, last := capture()
		log := slog.New(kitotel.NewHandler(base, kitotel.Options{OnlySampled: true}))

		log.InfoContext(spanCtx(t, true), "msg")

		require.Equal(t, traceHex, utils.ExtractAttrs(last())["trace_id"])
	})
}

func TestHandler_SurvivesWithAttrsAndADecoratorChain(t *testing.T) {
	base, last := capture()

	// Three decorators between the correlation handler and the sink, plus a
	// derived logger on top: correlation has to survive every one of them.
	chain := handler.NewComponentHandler(base)
	chain = handler.NewGlobalFieldsHandler(chain, map[string]string{"env": "prod"}, false)
	chain = handler.NewHookHandler(chain, func(ctx context.Context, _ slog.Record) (context.Context, bool) {
		return ctx, true
	})

	log := slog.New(kitotel.NewHandler(chain, kitotel.Options{})).
		With("service", "inventory").
		With("region", "us-east-1")

	log.InfoContext(spanCtx(t, true), "msg")

	fields := utils.ExtractAttrs(last())
	require.Equal(t, traceHex, fields["trace_id"])
	require.Equal(t, spanHex, fields["span_id"])
	require.Equal(t, "inventory", fields["service"])
	require.Equal(t, "us-east-1", fields["region"])
	require.Equal(t, "prod", fields["env"])
}

func TestHandler_DelegatesEnabled(t *testing.T) {
	h := kitotel.NewHandler(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelWarn}), kitotel.Options{})

	require.False(t, h.Enabled(context.Background(), slog.LevelInfo))
	require.True(t, h.Enabled(context.Background(), slog.LevelError))
}

func TestHandler_Unwrap(t *testing.T) {
	base, _ := capture()
	h := kitotel.NewHandler(base, kitotel.Options{})

	unwrapper, ok := h.(interface{ Unwrap() slog.Handler })
	require.True(t, ok, "the handler must expose Unwrap so the logger lifecycle can walk a hand-assembled chain")
	require.Equal(t, base, unwrapper.Unwrap())
}

func TestNewHandler_NilNextPanics(t *testing.T) {
	// Returning a handler that dereferences nil on the first record moves the
	// failure far away from the mistake. Fail at construction instead.
	require.Panics(t, func() { kitotel.NewHandler(nil, kitotel.Options{}) })
}

func TestDecorator_WrapsWithTheGivenOptions(t *testing.T) {
	base, last := capture()
	log := slog.New(kitotel.Decorator(kitotel.Options{TraceIDKey: "traceId"})(base))

	log.InfoContext(spanCtx(t, true), "msg")

	require.Equal(t, traceHex, utils.ExtractAttrs(last())["traceId"])
}

// noopHandler is the benchmark sink. The capture handler rebuilds every record
// to honor WithGroup, which would put its allocations in the numbers below and
// hide the only figure that matters here: what this handler itself costs.
type noopHandler struct{}

func (noopHandler) Enabled(context.Context, slog.Level) bool  { return true }
func (noopHandler) Handle(context.Context, slog.Record) error { return nil }
func (h noopHandler) WithAttrs([]slog.Attr) slog.Handler      { return h }
func (h noopHandler) WithGroup(string) slog.Handler           { return h }

func BenchmarkHandler_NoSpan(b *testing.B) {
	h := kitotel.NewHandler(noopHandler{}, kitotel.Options{})
	ctx := context.Background()
	record := slog.NewRecord(time.Now(), slog.LevelInfo, "msg", 0)

	b.ReportAllocs()
	for b.Loop() {
		_ = h.Handle(ctx, record)
	}
}

func BenchmarkHandler_WithSpan(b *testing.B) {
	h := kitotel.NewHandler(noopHandler{}, kitotel.Options{})

	traceID, _ := oteltrace.TraceIDFromHex(traceHex)
	spanID, _ := oteltrace.SpanIDFromHex(spanHex)
	ctx := oteltrace.ContextWithSpanContext(context.Background(), oteltrace.NewSpanContext(oteltrace.SpanContextConfig{
		TraceID: traceID, SpanID: spanID, TraceFlags: oteltrace.FlagsSampled,
	}))
	record := slog.NewRecord(time.Now(), slog.LevelInfo, "msg", 0)

	b.ReportAllocs()
	for b.Loop() {
		_ = h.Handle(ctx, record)
	}
}

func TestHandler_WithAttrsAndWithGroupAreIdentityForEmptyInput(t *testing.T) {
	base, _ := capture()
	h := kitotel.NewHandler(base, kitotel.Options{})

	require.Same(t, h, h.WithAttrs(nil))
	require.Same(t, h, h.WithGroup(""))
}

func TestHandler_WithGroupNestsCorrelationUnderTheOpenGroup(t *testing.T) {
	base, last := capture()

	// Standard slog semantics: attributes added at Handle time land inside
	// whatever group is open above the sink, and the correlation fields are no
	// exception. Consumers who need them at the top level must open no group
	// above this handler.
	log := slog.New(kitotel.NewHandler(base, kitotel.Options{}).WithGroup("req"))

	log.InfoContext(spanCtx(t, true), "msg")

	nested := groupAttrs(t, utils.ExtractAttrs(last()), "req")
	require.Equal(t, traceHex, nested["trace_id"])
	require.Equal(t, spanHex, nested["span_id"])
}
