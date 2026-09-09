package handler_test

import (
	"context"
	"log/slog"
	"testing"

	"github.com/pablogore/kit-logger/pkg/logger/handler"
	"github.com/pablogore/kit-logger/pkg/logger/kitlogtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestUnwrap_ReturnsTheDecoratedHandler pins the traversal contract for every
// decorator in this package. A decorator that forgets Unwrap silently severs a
// lifecycle walk, so each one is checked by identity rather than by shape.
func TestUnwrap_ReturnsTheDecoratedHandler(t *testing.T) {
	leaf := kitlogtest.NewTestHandler(func(context.Context, slog.Record) {})
	noopHook := func(ctx context.Context, _ slog.Record) (context.Context, bool) { return ctx, true }

	decorators := map[string]slog.Handler{
		"hook":         handler.NewHookHandler(leaf, noopHook),
		"buffered":     handler.NewBufferedHandler(leaf, 1),
		"prometheus":   handler.NewPrometheusHandler(leaf),
		"sampling":     handler.NewSamplingHandler(leaf, handler.SamplingConfig{}),
		"component":    handler.NewComponentHandler(leaf),
		"globalFields": handler.NewGlobalFieldsHandler(leaf, map[string]string{"k": "v"}, true),
		"filter":       handler.NewFilterHandler(leaf, []handler.FilterRule{{Key: "k"}}),
	}

	for name, decorator := range decorators {
		t.Run(name, func(t *testing.T) {
			unwrapper, ok := decorator.(interface{ Unwrap() slog.Handler })
			require.True(t, ok, "%s must implement Unwrap", name)
			assert.Same(t, leaf, unwrapper.Unwrap())
		})
	}

	if buffered, ok := decorators["buffered"].(*handler.BufferedHandler); ok {
		require.NoError(t, buffered.Shutdown(context.Background()))
	}
}

func TestUnwrapAll_ReturnsEveryFanOutTarget(t *testing.T) {
	first := kitlogtest.NewTestHandler(func(context.Context, slog.Record) {})
	second := kitlogtest.NewTestHandler(func(context.Context, slog.Record) {})

	multi := handler.NewMultiHandler(first, second)

	targets := multi.UnwrapAll()
	require.Len(t, targets, 2)
	assert.Same(t, first, targets[0])
	assert.Same(t, second, targets[1])

	// The result is a copy: mutating it must not reach the handler's own slice.
	targets[0] = nil
	assert.Same(t, first, multi.UnwrapAll()[0])
}
