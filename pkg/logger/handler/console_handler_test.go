package handler_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pablogore/kit-logger/pkg/logger/handler"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var consoleTime = time.Date(2026, 9, 12, 15, 4, 5, 123_000_000, time.UTC)

func consoleRecord(msg string, attrs ...any) slog.Record {
	r := slog.NewRecord(consoleTime, slog.LevelInfo, msg, 0)
	r.Add(attrs...)
	return r
}

func TestConsoleHandler_Layout(t *testing.T) {
	var buf bytes.Buffer
	h := handler.NewConsoleHandler(&buf, handler.ConsoleOptions{})

	require.NoError(t, h.Handle(context.Background(),
		consoleRecord("order created", "service", "orders", "order_id", "ord-1", "amount", 99.5)))

	assert.Equal(t, "15:04:05.123 INF order created service=orders order_id=ord-1 amount=99.5\n", buf.String())
}

func TestConsoleHandler_LevelsAndQuoting(t *testing.T) {
	var buf bytes.Buffer
	h := handler.NewConsoleHandler(&buf, handler.ConsoleOptions{Level: slog.LevelDebug})
	for _, l := range []slog.Level{slog.LevelDebug, slog.LevelInfo, slog.LevelWarn, slog.LevelError, slog.LevelInfo + 2} {
		r := slog.NewRecord(consoleTime, l, "m", 0)
		r.Add("q", "has space", "e", "", "eq", "a=b", "err", errors.New("boom"))
		require.NoError(t, h.Handle(context.Background(), r))
	}

	lines := strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n")
	require.Len(t, lines, 5)
	assert.True(t, strings.HasPrefix(lines[0], "15:04:05.123 DBG m "), lines[0])
	assert.True(t, strings.HasPrefix(lines[1], "15:04:05.123 INF m "), lines[1])
	assert.True(t, strings.HasPrefix(lines[2], "15:04:05.123 WRN m "), lines[2])
	assert.True(t, strings.HasPrefix(lines[3], "15:04:05.123 ERR m "), lines[3])
	assert.True(t, strings.HasPrefix(lines[4], "15:04:05.123 INFO+2 m "), lines[4])
	assert.True(t, strings.HasSuffix(lines[0], ` q="has space" e="" eq="a=b" err=boom`), lines[0])
}

func TestConsoleHandler_GroupsFlattenAndSourceGoesLast(t *testing.T) {
	var buf bytes.Buffer
	h := handler.NewConsoleHandler(&buf, handler.ConsoleOptions{}).
		WithAttrs([]slog.Attr{slog.String("service", "orders")}).
		WithGroup("http")

	require.NoError(t, h.Handle(context.Background(), consoleRecord("done",
		slog.Group(slog.SourceKey, "function", "orders.Reserve", "file", "/abs/orders.go", "line", 42),
		"status", 200,
		slog.Group("peer", "ip", "10.0.0.1"),
	)))

	assert.Equal(t,
		"15:04:05.123 INF done service=orders http.source.function=orders.Reserve http.source.file=/abs/orders.go http.source.line=42 http.status=200 http.peer.ip=10.0.0.1\n",
		buf.String(), "a source group inside an open group is just a group; only the top-level one is a location")

	buf.Reset()
	top := handler.NewConsoleHandler(&buf, handler.ConsoleOptions{})
	require.NoError(t, top.Handle(context.Background(), consoleRecord("done",
		slog.Group(slog.SourceKey, "function", "orders.Reserve", "file", "/abs/orders.go", "line", 42),
		"status", 200,
	)))
	assert.Equal(t, "15:04:05.123 INF done status=200 source=orders.go:42\n", buf.String())
}

func TestConsoleHandler_ColorsOnlyWhenForced(t *testing.T) {
	var plain, colored bytes.Buffer
	require.NoError(t, handler.NewConsoleHandler(&plain, handler.ConsoleOptions{}).
		Handle(context.Background(), consoleRecord("m", "error", "x")))
	require.NoError(t, handler.NewConsoleHandler(&colored, handler.ConsoleOptions{ForceColor: true}).
		Handle(context.Background(), consoleRecord("m", "error", "x")))
	var suppressed bytes.Buffer
	require.NoError(t, handler.NewConsoleHandler(&suppressed, handler.ConsoleOptions{ForceColor: true, NoColor: true}).
		Handle(context.Background(), consoleRecord("m", "error", "x")))

	assert.NotContains(t, plain.String(), "\x1b[", "a buffer is not a terminal")
	assert.Contains(t, colored.String(), "\x1b[31mx\x1b[0m", "error values are red")
	assert.Contains(t, colored.String(), "\x1b[32mINF\x1b[0m")
	assert.Equal(t, plain.String(), suppressed.String(), "NoColor wins over ForceColor")
}

func TestConsoleHandler_EnabledAndTimeFormat(t *testing.T) {
	var buf bytes.Buffer
	h := handler.NewConsoleHandler(&buf, handler.ConsoleOptions{Level: slog.LevelWarn, TimeFormat: "15:04"})

	assert.False(t, h.Enabled(context.Background(), slog.LevelInfo))
	assert.True(t, h.Enabled(context.Background(), slog.LevelWarn))

	r := slog.NewRecord(consoleTime, slog.LevelWarn, "m", 0)
	require.NoError(t, h.Handle(context.Background(), r))
	assert.Equal(t, "15:04 WRN m\n", buf.String())

	buf.Reset()
	require.NoError(t, h.Handle(context.Background(), slog.NewRecord(time.Time{}, slog.LevelWarn, "no time", 0)))
	assert.Equal(t, "WRN no time\n", buf.String(), "a zero time is omitted, as slog's handlers do")
}

func TestConsoleHandler_EmptyGroupAndEmptyWithReturnReceiver(t *testing.T) {
	var buf bytes.Buffer
	h := handler.NewConsoleHandler(&buf, handler.ConsoleOptions{})

	assert.Same(t, h, h.WithGroup(""))
	assert.Same(t, h, h.WithAttrs(nil))

	require.NoError(t, h.Handle(context.Background(), consoleRecord("m", slog.Group("empty"), slog.Group("", "a", 1))))
	assert.Equal(t, "15:04:05.123 INF m a=1\n", buf.String())
}

func TestConsoleHandler_LinesNeverInterleave(t *testing.T) {
	var buf bytes.Buffer
	h := handler.NewConsoleHandler(&buf, handler.ConsoleOptions{})
	const n = 200
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = h.Handle(context.Background(), consoleRecord("m", "k", strings.Repeat("v", 50)))
		}()
	}
	wg.Wait()

	lines := strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n")
	require.Len(t, lines, n)
	for _, l := range lines {
		assert.Equal(t, "15:04:05.123 INF m k="+strings.Repeat("v", 50), l)
	}
}
