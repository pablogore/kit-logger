package handler_test

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/pablogore/kit-logger/pkg/logger/handler"
	"github.com/pablogore/kit-logger/pkg/logger/kitlogtest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// flatten renders a record's attrs as "key=value" strings with groups
// rendered as dotted keys, in the order the record carries them, so a test
// can assert on shape, order and value in one comparison.
func flatten(record slog.Record) []string {
	var out []string
	var walk func(prefix string, attrs []slog.Attr)
	walk = func(prefix string, attrs []slog.Attr) {
		for _, a := range attrs {
			if a.Value.Kind() == slog.KindGroup {
				walk(prefix+a.Key+".", a.Value.Group())
				continue
			}
			out = append(out, prefix+a.Key+"="+a.Value.String())
		}
	}
	var top []slog.Attr
	record.Attrs(func(a slog.Attr) bool { top = append(top, a); return true })
	walk("", top)
	return out
}

func newDedupSink(t *testing.T, opts handler.DedupOptions) (slog.Handler, func() slog.Record) {
	t.Helper()
	var got []slog.Record
	sink := kitlogtest.NewTestHandler(func(_ context.Context, r slog.Record) { got = append(got, r) })
	h := handler.NewDedupHandler(sink, opts)
	return h, func() slog.Record {
		require.Len(t, got, 1, "exactly one record must reach the sink")
		return got[0]
	}
}

func record(attrs ...any) slog.Record {
	r := slog.NewRecord(time.Now(), slog.LevelInfo, "msg", 0)
	r.Add(attrs...)
	return r
}

func TestDedupHandler_LastValueFirstPosition(t *testing.T) {
	h, last := newDedupSink(t, handler.DedupOptions{})

	require.NoError(t, h.Handle(context.Background(),
		record("request_id", "first", "user", 1, "request_id", "second")))

	assert.Equal(t, []string{"request_id=second", "user=1"}, flatten(last()))
}

func TestDedupHandler_WithAttrsAndRecordCollapseIntoOneKey(t *testing.T) {
	h, last := newDedupSink(t, handler.DedupOptions{})
	derived := h.WithAttrs([]slog.Attr{slog.String("request_id", "from-with"), slog.Int("user", 1)})

	require.NoError(t, derived.Handle(context.Background(),
		record("order", "o1", "request_id", "from-record")))

	assert.Equal(t, []string{"request_id=from-record", "user=1", "order=o1"}, flatten(last()),
		"With attrs keep their leading position; the record's later value wins")
}

func TestDedupHandler_PinnedComeFirstAndNeverLose(t *testing.T) {
	h, last := newDedupSink(t, handler.DedupOptions{
		Pinned: []slog.Attr{slog.String("env", "prod"), slog.String("service", "orders")},
	})
	derived := h.WithAttrs([]slog.Attr{slog.String("service", "from-with")})

	require.NoError(t, derived.Handle(context.Background(),
		record("env", "canary", "order", "o1")))

	assert.Equal(t, []string{"env=prod", "service=orders", "order=o1"}, flatten(last()))
}

func TestDedupHandler_PinnedScalarIgnoresLaterGroup(t *testing.T) {
	h, last := newDedupSink(t, handler.DedupOptions{Pinned: []slog.Attr{slog.String("service", "orders")}})

	require.NoError(t, h.Handle(context.Background(),
		record(slog.Group("service", "name", "other"))))

	assert.Equal(t, []string{"service=orders"}, flatten(last()))
}

func TestDedupHandler_ReservedKeysAreRenamed(t *testing.T) {
	h, last := newDedupSink(t, handler.DedupOptions{Reserved: []string{slog.SourceKey}})

	require.NoError(t, h.Handle(context.Background(),
		record("level", "debug", "msg", "shadow", "time", "t", "source", "kafka", "ok", true)))

	assert.Equal(t, []string{
		"attr.level=debug", "attr.msg=shadow", "attr.time=t", "attr.source=kafka", "ok=true",
	}, flatten(last()))
}

func TestDedupHandler_ReservedOnlyAppliesAtTopLevel(t *testing.T) {
	h, last := newDedupSink(t, handler.DedupOptions{})

	require.NoError(t, h.Handle(context.Background(),
		record(slog.Group("config", "level", "debug"))))

	assert.Equal(t, []string{"config.level=debug"}, flatten(last()))
}

func TestDedupHandler_GroupsWithTheSameKeyMerge(t *testing.T) {
	h, last := newDedupSink(t, handler.DedupOptions{})
	derived := h.WithAttrs([]slog.Attr{slog.Group("http", slog.String("method", "GET"), slog.Int("status", 0))})

	require.NoError(t, derived.Handle(context.Background(),
		record(slog.Group("http", "status", 200, "path", "/x"))))

	assert.Equal(t, []string{"http.method=GET", "http.status=200", "http.path=/x"}, flatten(last()))
}

func TestDedupHandler_ScalarThenGroupThenScalar(t *testing.T) {
	h, last := newDedupSink(t, handler.DedupOptions{})

	require.NoError(t, h.Handle(context.Background(),
		record("component", "orders", slog.Group("component", "file", "a.go"), "component", "billing")))

	assert.Equal(t, []string{"component=billing"}, flatten(last()),
		"a later scalar replaces an earlier group, exactly as a later group replaces a scalar")
}

func TestDedupHandler_WithGroupNestsAndDedupsInside(t *testing.T) {
	h, last := newDedupSink(t, handler.DedupOptions{Pinned: []slog.Attr{slog.String("service", "orders")}})
	derived := h.WithAttrs([]slog.Attr{slog.String("top", "1")}).
		WithGroup("request").
		WithAttrs([]slog.Attr{slog.String("id", "from-with")})

	require.NoError(t, derived.Handle(context.Background(),
		record("id", "from-record", "service", "inside-group")))

	assert.Equal(t, []string{
		"service=orders", "top=1", "request.id=from-record", "request.service=inside-group",
	}, flatten(last()), "pinned attrs stay at the top level; a same-named key inside a group is a different path")
}

func TestDedupHandler_EmptyGroupElidedAndEmptyKeyInlined(t *testing.T) {
	h, last := newDedupSink(t, handler.DedupOptions{})

	require.NoError(t, h.Handle(context.Background(),
		record(slog.Group("empty"), slog.Group("", slog.String("a", "1"), slog.String("a", "2")))))

	assert.Equal(t, []string{"a=2"}, flatten(last()))
}

func TestDedupHandler_EmptyWithGroupAndEmptyWithAttrsReturnReceiver(t *testing.T) {
	h, _ := newDedupSink(t, handler.DedupOptions{})

	assert.Same(t, h, h.WithGroup(""))
	assert.Same(t, h, h.WithAttrs(nil))
	assert.Same(t, h, h.WithAttrs([]slog.Attr{slog.String("", "dropped")}))
}

func TestDedupHandler_SiblingsDoNotShareHeldOps(t *testing.T) {
	var got []slog.Record
	sink := kitlogtest.NewTestHandler(func(_ context.Context, r slog.Record) { got = append(got, r) })
	parent := handler.NewDedupHandler(sink, handler.DedupOptions{}).WithAttrs([]slog.Attr{slog.String("p", "1")})
	a := parent.WithAttrs([]slog.Attr{slog.String("a", "1")})
	b := parent.WithAttrs([]slog.Attr{slog.String("b", "1")})

	require.NoError(t, a.Handle(context.Background(), record()))
	require.NoError(t, b.Handle(context.Background(), record()))

	require.Len(t, got, 2)
	assert.Equal(t, []string{"p=1", "a=1"}, flatten(got[0]))
	assert.Equal(t, []string{"p=1", "b=1"}, flatten(got[1]))
}

type valuer struct{ v string }

func (v valuer) LogValue() slog.Value { return slog.StringValue(v.v) }

func TestDedupHandler_ResolvesLogValuersBeforeComparing(t *testing.T) {
	h, last := newDedupSink(t, handler.DedupOptions{})

	require.NoError(t, h.Handle(context.Background(),
		record("k", valuer{"first"}, "k", valuer{"second"})))

	assert.Equal(t, []string{"k=second"}, flatten(last()))
}

func TestDedupHandler_CleanRecordIsForwardedVerbatim(t *testing.T) {
	var got slog.Record
	sink := kitlogtest.NewTestHandler(func(_ context.Context, r slog.Record) { got = r })
	h := handler.NewDedupHandler(sink, handler.DedupOptions{})
	in := record("a", 1, "b", "two")

	require.NoError(t, h.Handle(context.Background(), in))

	assert.Equal(t, in.Time, got.Time)
	assert.Equal(t, in.PC, got.PC)
	assert.Equal(t, flatten(in), flatten(got))
}

func TestDedupHandler_PreservesTimeLevelMessageAndPC(t *testing.T) {
	var got slog.Record
	sink := kitlogtest.NewTestHandler(func(_ context.Context, r slog.Record) { got = r })
	h := handler.NewDedupHandler(sink, handler.DedupOptions{Pinned: []slog.Attr{slog.String("s", "v")}})
	in := slog.NewRecord(time.Unix(1, 0), slog.LevelWarn, "hello", 42)
	in.Add("k", "v")

	require.NoError(t, h.Handle(context.Background(), in))

	assert.Equal(t, in.Time, got.Time)
	assert.Equal(t, slog.LevelWarn, got.Level)
	assert.Equal(t, "hello", got.Message)
	assert.Equal(t, uintptr(42), got.PC)
}

func TestDedupHandler_EnabledDelegates(t *testing.T) {
	sink := slog.NewTextHandler(nil, &slog.HandlerOptions{Level: slog.LevelWarn})
	h := handler.NewDedupHandler(sink, handler.DedupOptions{})

	assert.False(t, h.Enabled(context.Background(), slog.LevelInfo))
	assert.True(t, h.Enabled(context.Background(), slog.LevelError))
}
