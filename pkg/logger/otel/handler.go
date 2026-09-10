// Package otel correlates log records with OpenTelemetry traces.
//
// It adds nothing but correlation. It does not create, start, end or sample
// spans, it does not export log records over OTLP, and it does not reimplement
// any part of the OTel SDK: it reads the span context that a propagator or
// tracer already put in the context.Context and copies two identifiers onto the
// record.
//
// The package lives outside pkg/logger on purpose. Its only dependency is
// go.opentelemetry.io/otel/trace — the API module, not the SDK — and keeping it
// in its own package is what makes that dependency opt-in at import time.
// Consumers who do not want OpenTelemetry never import this package and never
// inherit it.
//
// Wire it as the outermost decorator, through Config.ContextHandler:
//
//	cfg.ContextHandler = kitotel.Decorator(kitotel.Options{})
//
// Outermost matters. The handler reads the context, so it must run on the
// goroutine of the call that produced the record.
package otel

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/trace"
)

// Default field names. They are snake_case to match this library's existing
// convention (duration_ms, request_id, suppressed_count) rather than the dotted
// OTel semantic-convention names; Options makes them configurable for consumers
// who standardize on something else.
const (
	DefaultTraceIDKey    = "trace_id"
	DefaultSpanIDKey     = "span_id"
	DefaultTraceFlagsKey = "trace_flags"
)

// Options configures the correlation handler. The zero value is valid and
// emits trace_id and span_id for every record that has a valid span context.
type Options struct {
	// TraceIDKey is the field name for the trace ID. Empty means
	// DefaultTraceIDKey.
	TraceIDKey string

	// SpanIDKey is the field name for the span ID. Empty means
	// DefaultSpanIDKey.
	SpanIDKey string

	// TraceFlags also emits the W3C trace flags as a two-character hex string
	// under DefaultTraceFlagsKey. Off by default: most backends join on the
	// trace ID alone.
	TraceFlags bool

	// Group, when non-empty, nests the correlation fields under a group of
	// that name instead of adding them at the top level.
	Group string

	// OnlySampled restricts enrichment to sampled spans.
	//
	// Off by default, deliberately. An unsampled span still carries a valid
	// trace ID, and correlating the logs of a request whose trace was dropped
	// is frequently the only thing left to debug with. Turn it on when log
	// cardinality is the binding constraint.
	OnlySampled bool
}

// handler adds trace correlation fields to records whose context carries a
// valid span context. Records without one pass through untouched.
type handler struct {
	next slog.Handler

	traceIDKey  string
	spanIDKey   string
	traceFlags  bool
	group       string
	onlySampled bool
}

// NewHandler returns a slog.Handler that adds trace correlation fields to
// records whose context carries a valid span context.
//
// It panics if next is nil: a handler that dereferences nil on its first record
// reports the mistake somewhere far away from where it was made.
func NewHandler(next slog.Handler, opts Options) slog.Handler {
	if next == nil {
		panic("kit-logger/otel: NewHandler called with a nil next handler")
	}
	return &handler{
		next:        next,
		traceIDKey:  orDefault(opts.TraceIDKey, DefaultTraceIDKey),
		spanIDKey:   orDefault(opts.SpanIDKey, DefaultSpanIDKey),
		traceFlags:  opts.TraceFlags,
		group:       opts.Group,
		onlySampled: opts.OnlySampled,
	}
}

// Decorator returns a function suitable for logger.Config.ContextHandler, so
// wiring correlation is one line at the call site.
func Decorator(opts Options) func(slog.Handler) slog.Handler {
	return func(next slog.Handler) slog.Handler {
		return NewHandler(next, opts)
	}
}

func orDefault(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}

// Enabled reports whether the wrapped handler is enabled for level.
func (h *handler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

// Handle adds the correlation fields and delegates.
//
// The no-span path is the common one for a consumer without tracing, and it is
// on every record: it reads the context, finds nothing, and passes the record
// through by value without cloning or allocating.
func (h *handler) Handle(ctx context.Context, record slog.Record) error {
	sc := trace.SpanContextFromContext(ctx)
	if !sc.IsValid() || (h.onlySampled && !sc.IsSampled()) {
		return h.next.Handle(ctx, record)
	}

	attrs := make([]slog.Attr, 0, 3)
	attrs = append(attrs,
		slog.String(h.traceIDKey, sc.TraceID().String()),
		slog.String(h.spanIDKey, sc.SpanID().String()),
	)
	if h.traceFlags {
		attrs = append(attrs, slog.String(DefaultTraceFlagsKey, sc.TraceFlags().String()))
	}
	if h.group != "" {
		attrs = []slog.Attr{{Key: h.group, Value: slog.GroupValue(attrs...)}}
	}

	// Cloned because a Record's attribute storage may be shared with records
	// the caller still holds; AddAttrs on a record received by value can
	// otherwise write into that shared array.
	clone := record.Clone()
	clone.AddAttrs(attrs...)
	return h.next.Handle(ctx, clone)
}

// WithAttrs returns a handler whose records carry attrs.
func (h *handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return h
	}
	derived := *h
	derived.next = h.next.WithAttrs(attrs)
	return &derived
}

// WithGroup returns a handler whose records are nested under name.
//
// The correlation fields are added at Handle time, so like every other attribute
// added downstream of an open group they are nested under it too. Consumers who
// need them at the top level should open no group above this handler.
func (h *handler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	derived := *h
	derived.next = h.next.WithGroup(name)
	return &derived
}

// Unwrap returns the handler this one decorates. It lets a logger lifecycle
// (Flush, Shutdown) traverse a chain that was assembled by hand, instead of
// stopping at the outermost handler.
func (h *handler) Unwrap() slog.Handler { return h.next }
