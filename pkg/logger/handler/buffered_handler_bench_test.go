package handler_test

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/pablogore/kit-logger/pkg/logger/handler"
)

// discardHandler is the cheapest possible downstream sink, so the benchmarks
// measure the buffering layer rather than an encoder.
type discardHandler struct{}

func (discardHandler) Enabled(context.Context, slog.Level) bool  { return true }
func (discardHandler) Handle(context.Context, slog.Record) error { return nil }
func (d discardHandler) WithAttrs([]slog.Attr) slog.Handler      { return d }
func (d discardHandler) WithGroup(string) slog.Handler           { return d }

func BenchmarkBufferedHandler_Handle(b *testing.B) {
	h := handler.NewBufferedHandler(discardHandler{}, 4096)
	defer func() { _ = h.Shutdown(context.Background()) }()

	ctx := context.Background()
	rec := slog.NewRecord(benchTime(), slog.LevelInfo, "message", 0)
	rec.AddAttrs(slog.String("k1", "v1"), slog.Int("k2", 2))

	// The acceptance rate is reported because a full buffer makes the cheap
	// rejection path dominate: a ns/op figure is not interpretable without it.
	var accepted int64
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if h.Handle(ctx, rec) == nil {
			accepted++
		}
	}
	b.StopTimer()
	b.ReportMetric(float64(accepted)/float64(b.N)*100, "%accepted")
	if accepted > 0 {
		b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(accepted), "ns/accepted")
	}
}

func BenchmarkBufferedHandler_HandleParallel(b *testing.B) {
	h := handler.NewBufferedHandler(discardHandler{}, 4096)
	defer func() { _ = h.Shutdown(context.Background()) }()

	rec := slog.NewRecord(benchTime(), slog.LevelInfo, "message", 0)
	rec.AddAttrs(slog.String("k1", "v1"), slog.Int("k2", 2))

	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		ctx := context.Background()
		for pb.Next() {
			_ = h.Handle(ctx, rec)
		}
	})
}

// BenchmarkBufferedHandler_WithAttrs measures handler derivation, which is what
// slog.Logger.With does on every call. On HEAD this allocated a channel and
// started a goroutine per call.
func BenchmarkBufferedHandler_WithAttrs(b *testing.B) {
	h := handler.NewBufferedHandler(discardHandler{}, 4096)
	defer func() { _ = h.Shutdown(context.Background()) }()

	attrs := []slog.Attr{slog.String("request_id", "01JABCDEF")}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = h.WithAttrs(attrs)
	}
}

func BenchmarkBufferedHandler_WithGroup(b *testing.B) {
	h := handler.NewBufferedHandler(discardHandler{}, 4096)
	defer func() { _ = h.Shutdown(context.Background()) }()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = h.WithGroup("group")
	}
}

// BenchmarkBufferedHandler_Overflow measures the rejection path with a buffer
// too small to keep up.
func BenchmarkBufferedHandler_Overflow(b *testing.B) {
	release := make(chan struct{})
	h := handler.NewBufferedHandler(blockingHandler{release}, 1)
	defer func() {
		close(release)
		_ = h.Shutdown(context.Background())
	}()

	ctx := context.Background()
	rec := slog.NewRecord(benchTime(), slog.LevelInfo, "message", 0)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = h.Handle(ctx, rec)
	}
}

type blockingHandler struct{ release <-chan struct{} }

func (blockingHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h blockingHandler) Handle(context.Context, slog.Record) error {
	<-h.release
	return nil
}
func (h blockingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h blockingHandler) WithGroup(string) slog.Handler      { return h }

func benchTime() time.Time { return time.Unix(0, 0) }
