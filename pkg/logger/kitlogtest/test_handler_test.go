package kitlogtest

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTestHandler_WithAttrs_DoesNotAliasSiblings pins KITLOG-GO-017: WithAttrs
// used to do append(h.Attrs, attrs...), which can silently write into a
// sibling handler's backing array whenever the parent slice has spare
// capacity. Two handlers derived from the same parent must never see each
// other's attrs.
func TestTestHandler_WithAttrs_DoesNotAliasSiblings(t *testing.T) {
	base := NewTestHandler(func(context.Context, slog.Record) {})
	parent := base.WithAttrs([]slog.Attr{slog.String("k", "parent")}).(*TestHandler)

	childA := parent.WithAttrs([]slog.Attr{slog.String("case", "A")}).(*TestHandler)
	childB := parent.WithAttrs([]slog.Attr{slog.String("case", "B")}).(*TestHandler)

	var gotA, gotB map[string]string
	childA.Callback = func(_ context.Context, r slog.Record) { gotA = attrsOf(r) }
	childB.Callback = func(_ context.Context, r slog.Record) { gotB = attrsOf(r) }

	require.NoError(t, childA.Handle(context.Background(), newRecord("a")))
	require.NoError(t, childB.Handle(context.Background(), newRecord("b")))

	assert.Equal(t, "A", gotA["case"])
	assert.Equal(t, "B", gotB["case"])
}

// TestTestHandler_WithGroup_NestsAttrs asserts that WithGroup actually nests
// subsequent attrs and the record's own attrs inside the named group, instead
// of the previous no-op that dropped grouping on the floor. The expectation
// is checked against slog.NewJSONHandler's own nesting for the same calls.
func TestTestHandler_WithGroup_NestsAttrs(t *testing.T) {
	var captured slog.Record
	h := NewTestHandler(func(_ context.Context, r slog.Record) { captured = r })

	derived := h.WithAttrs([]slog.Attr{slog.String("top", "t")}).
		WithGroup("g").
		WithAttrs([]slog.Attr{slog.String("inner", "i")})

	rec := newRecord("grouped")
	rec.AddAttrs(slog.String("leaf", "l"))
	require.NoError(t, derived.Handle(context.Background(), rec))

	var buf bytes.Buffer
	ref := slog.NewJSONHandler(&buf, nil)
	refDerived := ref.WithAttrs([]slog.Attr{slog.String("top", "t")}).
		WithGroup("g").
		WithAttrs([]slog.Attr{slog.String("inner", "i")})
	require.NoError(t, refDerived.Handle(context.Background(), rec))

	var want map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &want))

	got := map[string]any{}
	captured.Attrs(func(a slog.Attr) bool {
		got[a.Key] = attrValue(a)
		return true
	})

	assert.Equal(t, want["top"], got["top"])
	assert.Equal(t, want["g"], got["g"])
}

func attrValue(a slog.Attr) any {
	if a.Value.Kind() == slog.KindGroup {
		out := map[string]any{}
		for _, sub := range a.Value.Group() {
			out[sub.Key] = attrValue(sub)
		}
		return out
	}
	return a.Value.String()
}

func attrsOf(r slog.Record) map[string]string {
	out := make(map[string]string, r.NumAttrs())
	r.Attrs(func(a slog.Attr) bool {
		out[a.Key] = a.Value.String()
		return true
	})
	return out
}

func newRecord(msg string) slog.Record {
	return slog.NewRecord(time.Now(), slog.LevelInfo, msg, 0)
}
