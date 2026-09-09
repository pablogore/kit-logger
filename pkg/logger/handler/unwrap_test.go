package handler

import (
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestUnwrap_ReturnsTheDecoratedHandler pins the traversal contract for every
// decorator in this package. A decorator that forgets Unwrap silently severs a
// lifecycle walk, so each one is checked by identity rather than by shape.
func TestUnwrap_ReturnsTheDecoratedHandler(t *testing.T) {
	leaf := NewTestHandler(func(context.Context, slog.Record) {})
	noopHook := func(ctx context.Context, _ slog.Record) (context.Context, bool) { return ctx, true }

	decorators := map[string]slog.Handler{
		"hook":         NewHookHandler(leaf, noopHook),
		"buffered":     NewBufferedHandler(leaf, 1),
		"prometheus":   NewPrometheusHandler(leaf),
		"sampling":     NewSamplingHandler(leaf, SamplingConfig{}),
		"component":    NewComponentHandler(leaf),
		"globalFields": NewGlobalFieldsHandler(leaf, map[string]string{"k": "v"}, true),
		"filter":       NewFilterHandler(leaf, []FilterRule{{Key: "k"}}),
	}

	for name, decorator := range decorators {
		t.Run(name, func(t *testing.T) {
			unwrapper, ok := decorator.(interface{ Unwrap() slog.Handler })
			require.True(t, ok, "%s must implement Unwrap", name)
			assert.Same(t, leaf, unwrapper.Unwrap())
		})
	}

	if buffered, ok := decorators["buffered"].(*BufferedHandler); ok {
		require.NoError(t, buffered.Shutdown(context.Background()))
	}
}

func TestUnwrapAll_ReturnsEveryFanOutTarget(t *testing.T) {
	first := NewTestHandler(func(context.Context, slog.Record) {})
	second := NewTestHandler(func(context.Context, slog.Record) {})

	multi := NewMultiHandler(first, second)

	targets := multi.UnwrapAll()
	require.Len(t, targets, 2)
	assert.Same(t, first, targets[0])
	assert.Same(t, second, targets[1])

	// The result is a copy: mutating it must not reach the handler's own slice.
	targets[0] = nil
	assert.Same(t, first, multi.UnwrapAll()[0])
}
