package logger_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	oteltrace "go.opentelemetry.io/otel/trace"

	"github.com/pablogore/kit-logger/pkg/logger"
	"github.com/pablogore/kit-logger/pkg/logger/handler"
	kitotel "github.com/pablogore/kit-logger/pkg/logger/otel"
	"github.com/pablogore/kit-logger/pkg/logger/utils"
)

const (
	correlationTraceHex = "4bf92f3577b34da6a3ce929d0e0e4736"
	correlationSpanHex  = "00f067aa0ba902b7"
)

func tracedContext(t *testing.T) context.Context {
	t.Helper()

	traceID, err := oteltrace.TraceIDFromHex(correlationTraceHex)
	require.NoError(t, err)
	spanID, err := oteltrace.SpanIDFromHex(correlationSpanHex)
	require.NoError(t, err)

	return oteltrace.ContextWithSpanContext(context.Background(), oteltrace.NewSpanContext(oteltrace.SpanContextConfig{
		TraceID:    traceID,
		SpanID:     spanID,
		TraceFlags: oteltrace.FlagsSampled,
	}))
}

func TestConfigContextHandler_CorrelatesContextLogMethods(t *testing.T) {
	rec := &recorder{}
	log := logger.New(logger.Config{
		Handler:        rec.handler(),
		ContextHandler: kitotel.Decorator(kitotel.Options{}),
	})

	// The ergonomic call, not WithContext: this is the one that used to carry
	// nothing at all.
	log.InfoContext(tracedContext(t), "handling request", "order_id", "A-1")

	got, ok := rec.find("handling request")
	require.True(t, ok)

	fields := utils.ExtractAttrs(got)
	require.Equal(t, correlationTraceHex, fields["trace_id"])
	require.Equal(t, correlationSpanHex, fields["span_id"])
	require.Equal(t, "A-1", fields["order_id"])
}

func TestConfigContextHandler_SurvivesDerivedLoggers(t *testing.T) {
	rec := &recorder{}
	log := logger.New(logger.Config{
		Handler:        rec.handler(),
		ContextHandler: kitotel.Decorator(kitotel.Options{}),
	}).With("service", "inventory").With("region", "us-east-1")

	log.ErrorContext(tracedContext(t), "stock check failed")

	got, ok := rec.find("stock check failed")
	require.True(t, ok)

	fields := utils.ExtractAttrs(got)
	require.Equal(t, correlationTraceHex, fields["trace_id"])
	require.Equal(t, "inventory", fields["service"])
}

func TestConfigContextHandler_AddsNothingWithoutASpan(t *testing.T) {
	rec := &recorder{}
	log := logger.New(logger.Config{
		Handler:        rec.handler(),
		ContextHandler: kitotel.Decorator(kitotel.Options{}),
	})

	log.InfoContext(context.Background(), "no trace here")

	got, ok := rec.find("no trace here")
	require.True(t, ok)

	fields := utils.ExtractAttrs(got)
	require.NotContains(t, fields, "trace_id")
	require.NotContains(t, fields, "span_id")
}

// The correlation handler reads the context, so it has to run on the goroutine
// of the call that produced the record. Config places it above the buffer for
// exactly that reason, and this pins it: the assertion would fail the moment
// something reordered the pipeline or dropped the per-record context.
func TestConfigContextHandler_CorrelatesAcrossTheBuffer(t *testing.T) {
	rec := &recorder{}
	buffered := handler.NewBufferedHandler(rec.handler(), 16)

	log := logger.New(logger.Config{
		Handler:        buffered,
		ContextHandler: kitotel.Decorator(kitotel.Options{}),
	})

	log.InfoContext(tracedContext(t), "async record")

	managed, ok := log.(logger.ManagedLogger)
	require.True(t, ok)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.NoError(t, managed.Flush(ctx))

	got, found := rec.find("async record")
	require.True(t, found)
	require.Equal(t, correlationTraceHex, utils.ExtractAttrs(got)["trace_id"])
}
