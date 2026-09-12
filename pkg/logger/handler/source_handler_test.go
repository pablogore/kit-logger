package handler_test

import (
	"context"
	"log/slog"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/pablogore/kit-logger/pkg/logger/handler"
	"github.com/pablogore/kit-logger/pkg/logger/kitlogtest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// source is the flattened shape of the "source" group SourceHandler adds.
type source struct {
	found    bool
	function string
	file     string
	line     int
}

func sourceOf(record slog.Record) source {
	var got source
	record.Attrs(func(a slog.Attr) bool {
		if a.Key != slog.SourceKey {
			return true
		}
		got.found = true
		for _, m := range a.Value.Group() {
			switch m.Key {
			case "function":
				got.function = m.Value.String()
			case "file":
				got.file = m.Value.String()
			case "line":
				got.line = int(m.Value.Int64())
			}
		}
		return false
	})
	return got
}

func newSourceSink(t *testing.T) (*slog.Logger, func() slog.Record) {
	t.Helper()
	var got []slog.Record
	sink := kitlogtest.NewTestHandler(func(_ context.Context, r slog.Record) { got = append(got, r) })
	return slog.New(handler.NewSourceHandler(sink)), func() slog.Record {
		require.Len(t, got, 1)
		return got[0]
	}
}

func TestSourceHandler_AttributesTheCallSite(t *testing.T) {
	log, last := newSourceSink(t)

	_, file, line, _ := runtime.Caller(0)
	log.Info("hello")

	got := sourceOf(last())
	require.True(t, got.found)
	assert.Equal(t, filepath.Base(file), got.file)
	assert.Equal(t, line+1, got.line)
	assert.Equal(t, "handler_test.TestSourceHandler_AttributesTheCallSite", got.function)
}

func TestSourceHandler_ClosureNamesCollapseToTheDefiningFunction(t *testing.T) {
	log, last := newSourceSink(t)

	func() {
		func() { log.Info("from a nested closure") }()
	}()

	got := sourceOf(last())
	require.True(t, got.found)
	assert.Equal(t, "handler_test.TestSourceHandler_ClosureNamesCollapseToTheDefiningFunction", got.function,
		"func1.1 suffixes name nothing a reader can search for")
}

func TestSourceHandler_NoGroupWithoutPC(t *testing.T) {
	var got []slog.Record
	sink := kitlogtest.NewTestHandler(func(_ context.Context, r slog.Record) { got = append(got, r) })
	h := handler.NewSourceHandler(sink)

	require.NoError(t, h.Handle(context.Background(), slog.NewRecord(time.Now(), slog.LevelInfo, "by hand", 0)))

	require.Len(t, got, 1)
	assert.False(t, sourceOf(got[0]).found, "a record built by hand carries no PC and must get no source group")
}

func TestSourceHandler_WithAttrsAndWithGroupForward(t *testing.T) {
	var got []slog.Record
	sink := kitlogtest.NewTestHandler(func(_ context.Context, r slog.Record) { got = append(got, r) })
	log := slog.New(handler.NewSourceHandler(sink).WithAttrs([]slog.Attr{slog.String("k", "v")}))

	log.Info("hello")

	require.Len(t, got, 1)
	assert.Equal(t, "v", func() string {
		var v string
		got[0].Attrs(func(a slog.Attr) bool {
			if a.Key == "k" {
				v = a.Value.String()
			}
			return true
		})
		return v
	}())
	assert.True(t, sourceOf(got[0]).found)
}
