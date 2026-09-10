package handler

import (
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
)

type filterNoopHandler struct{}

func (filterNoopHandler) Enabled(context.Context, slog.Level) bool  { return true }
func (filterNoopHandler) Handle(context.Context, slog.Record) error { return nil }
func (h filterNoopHandler) WithAttrs([]slog.Attr) slog.Handler      { return h }
func (h filterNoopHandler) WithGroup(string) slog.Handler           { return h }

// TestFilterHandler_WithAttrsClipsBeforeAppend pins the backing-array
// aliasing fix directly, instead of hoping the allocator happens to leave
// spare capacity the way real use would. It builds a parent whose held ops
// slice has spare capacity (len 1, cap 4) -- unreachable through the public
// API alone, since the allocator's own growth rarely lines up this way for
// filterOp's size -- and checks that two children branched from it each land
// in their own backing array rather than overwriting each other's slot 1.
func TestFilterHandler_WithAttrsClipsBeforeAppend(t *testing.T) {
	ops := make([]filterOp, 1, 4)
	ops[0] = filterOp{attrs: []slog.Attr{slog.String("base", "v")}}

	parent := &FilterHandler{
		next:  filterNoopHandler{},
		rules: []FilterRule{{Key: "x"}},
		ops:   ops,
	}

	childA := parent.WithAttrs([]slog.Attr{slog.String("only_a", "a")}).(*FilterHandler)
	childB := parent.WithAttrs([]slog.Attr{slog.String("only_b", "b")}).(*FilterHandler)

	require.Len(t, childA.ops, 2)
	require.Len(t, childB.ops, 2)
	require.Equal(t, "only_a", childA.ops[1].attrs[0].Key)
	require.Equal(t, "only_b", childB.ops[1].attrs[0].Key,
		"childB's append must not have landed in childA's backing array slot")
}

// TestFilterHandler_WithGroupClipsBeforeAppend is the WithGroup counterpart.
func TestFilterHandler_WithGroupClipsBeforeAppend(t *testing.T) {
	ops := make([]filterOp, 1, 4)
	ops[0] = filterOp{attrs: []slog.Attr{slog.String("base", "v")}}

	parent := &FilterHandler{
		next:  filterNoopHandler{},
		rules: []FilterRule{{Key: "x"}},
		ops:   ops,
	}

	childA := parent.WithGroup("a").(*FilterHandler)
	childB := parent.WithGroup("b").(*FilterHandler)

	require.Len(t, childA.ops, 2)
	require.Len(t, childB.ops, 2)
	require.Equal(t, "a", childA.ops[1].group)
	require.Equal(t, "b", childB.ops[1].group,
		"childB's append must not have landed in childA's backing array slot")
}
