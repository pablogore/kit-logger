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
)

// benchPC is a real program counter used as slog.Record.PC in the benchmarks.
var benchPC = func() uintptr {
	var pcs [1]uintptr
	runtime.Callers(1, pcs[:])
	return pcs[0]
}()

// BenchmarkComponentHandler measures the cost of enriching one record with the
// component group.
//
// "Direct" delivers synchronously from the benchmark goroutine. "Buffered"
// delivers through BufferedHandler, i.e. on the worker goroutine, which is the
// configuration where live-stack attribution used to break.
func BenchmarkComponentHandler(b *testing.B) {
	b.Run("Direct", func(b *testing.B) {
		sink := kitlogtest.NewTestHandler(func(context.Context, slog.Record) {})
		h := handler.NewComponentHandler(sink)
		ctx := context.Background()

		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			record := slog.NewRecord(time.Now(), slog.LevelInfo, "bench", benchPC)
			_ = h.Handle(ctx, record)
		}
	})

	b.Run("Buffered", func(b *testing.B) {
		sink := kitlogtest.NewTestHandler(func(context.Context, slog.Record) {})
		buffered := handler.NewBufferedHandler(handler.NewComponentHandler(sink), 4096)
		ctx := context.Background()

		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			record := slog.NewRecord(time.Now(), slog.LevelInfo, "bench", benchPC)
			_ = buffered.Handle(ctx, record)
		}
		b.StopTimer()
		_ = buffered.Flush(ctx)
	})
}

// BenchmarkComponentHandler_ResolvePC measures the single CallersFrames resolve
// that replaced the live stack walk.
func BenchmarkComponentHandler_ResolvePC(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		frame, _ := runtime.CallersFrames([]uintptr{benchPC}).Next()
		_ = filepath.Base(frame.File)
		_ = frame.Line
		_ = frame.Function
	}
}

// BenchmarkComponentHandler_LegacyStackWalk measures the worst case of the
// removed heuristic: a runtime.Caller loop that finds no acceptable frame and
// therefore runs all 25 iterations. That is exactly what happened on
// BufferedHandler's worker goroutine, whose stack holds no application frame.
func BenchmarkComponentHandler_LegacyStackWalk(b *testing.B) {
	const maxDepth = 25

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		for depth := 2; depth < maxDepth; depth++ {
			pc, file, line, ok := runtime.Caller(depth)
			if !ok {
				continue
			}
			_ = filepath.Base(file)
			_ = line
			if fn := runtime.FuncForPC(pc); fn != nil {
				_ = fn.Name()
			}
		}
	}
}
