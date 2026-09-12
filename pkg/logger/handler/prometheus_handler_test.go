package handler_test

import (
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/pablogore/kit-logger/pkg/logger/handler"
	"github.com/pablogore/kit-logger/pkg/logger/kitlogtest"
)

// TestNewPrometheusHandler_IsANoOp pins the deprecated shim's contract: it
// registers nothing, counts nothing, and returns next unchanged by identity
// -- not merely an equivalent wrapper.
func TestNewPrometheusHandler_IsANoOp(t *testing.T) {
	baseHandler := kitlogtest.NewTestHandler(func(_ context.Context, r slog.Record) {})

	got := handler.NewPrometheusHandler(baseHandler)

	assert.Same(t, baseHandler, got)
}

func TestNewPrometheusHandler_StillLogsThroughNext(t *testing.T) {
	var captured slog.Record
	baseHandler := kitlogtest.NewTestHandler(func(_ context.Context, r slog.Record) {
		captured = r
	})

	promHandler := handler.NewPrometheusHandler(baseHandler)
	logger := slog.New(promHandler)
	logger.Info("test message")

	assert.Equal(t, "test message", captured.Message)
}
