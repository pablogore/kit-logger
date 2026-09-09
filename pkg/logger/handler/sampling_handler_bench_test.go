package handler_test

import (
	"context"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/pablogore/kit-logger/pkg/logger/handler"
)

// samDiscard is the cheapest downstream sink, so the benchmarks measure the
// sampler rather than an encoder.
type samDiscard struct{}

func (samDiscard) Enabled(context.Context, slog.Level) bool  { return true }
func (samDiscard) Handle(context.Context, slog.Record) error { return nil }
func (d samDiscard) WithAttrs([]slog.Attr) slog.Handler      { return d }
func (d samDiscard) WithGroup(string) slog.Handler           { return d }

func samBenchRecord(msg string) slog.Record {
	return slog.NewRecord(time.Unix(0, 0), slog.LevelInfo, msg, 0)
}

// Emitting path: Interval 0 and Probability 1, so every record passes.
func BenchmarkSamplingHandler_Handle(b *testing.B) {
	h := handler.NewSamplingHandler(samDiscard{}, handler.SamplingConfig{
		MinLevel:    slog.LevelInfo,
		Probability: 1,
	})
	ctx := context.Background()
	rec := samBenchRecord("message")

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = h.Handle(ctx, rec)
	}
}

// Suppressed path: a long interval, so every record after the first is dropped.
func BenchmarkSamplingHandler_HandleSuppressed(b *testing.B) {
	h := handler.NewSamplingHandler(samDiscard{}, handler.SamplingConfig{
		Interval:    time.Hour,
		MinLevel:    slog.LevelInfo,
		Probability: 1,
	})
	ctx := context.Background()
	rec := samBenchRecord("message")
	_ = h.Handle(ctx, rec) // prime the key

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = h.Handle(ctx, rec)
	}
}

// Below MinLevel: the bypass path, which must stay essentially free.
func BenchmarkSamplingHandler_HandleBelowMinLevel(b *testing.B) {
	h := handler.NewSamplingHandler(samDiscard{}, handler.SamplingConfig{
		MinLevel:    slog.LevelError,
		Probability: 1,
	})
	ctx := context.Background()
	rec := samBenchRecord("message")

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = h.Handle(ctx, rec)
	}
}

// Contention: the number that matters, because HEAD held its mutex across the
// wrapped handler. Run with -cpu=1,4,16.
func BenchmarkSamplingHandler_HandleParallel(b *testing.B) {
	h := handler.NewSamplingHandler(samDiscard{}, handler.SamplingConfig{
		MinLevel:    slog.LevelInfo,
		Probability: 1,
	})
	rec := samBenchRecord("message")

	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		ctx := context.Background()
		for pb.Next() {
			_ = h.Handle(ctx, rec)
		}
	})
}

// Contended interval path: a positive interval means the sampler takes its
// lock, which is the case HEAD serialised across the wrapped handler.
func BenchmarkSamplingHandler_HandleParallelWithInterval(b *testing.B) {
	h := handler.NewSamplingHandler(samDiscard{}, handler.SamplingConfig{
		Interval:    time.Hour,
		MinLevel:    slog.LevelInfo,
		Probability: 1,
	})
	rec := samBenchRecord("message")

	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		ctx := context.Background()
		for pb.Next() {
			_ = h.Handle(ctx, rec)
		}
	})
}

// High key cardinality exercises the bounded state and its eviction.
func BenchmarkSamplingHandler_HandleHighCardinality(b *testing.B) {
	h := handler.NewSamplingHandler(samDiscard{}, handler.SamplingConfig{
		Interval:    time.Hour,
		MinLevel:    slog.LevelInfo,
		Probability: 1,
		MaxKeys:     1024,
	})
	ctx := context.Background()
	records := make([]slog.Record, 4096)
	for i := range records {
		records[i] = samBenchRecord(fmt.Sprintf("event-%d", i))
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = h.Handle(ctx, records[i%len(records)])
	}
}

// Cardinality that fits inside MaxKeys: the ordinary case, with no eviction
// pressure. HandleHighCardinality above is deliberately adversarial (4096
// rotating keys against a 1024-key budget, so every lookup misses).
func BenchmarkSamplingHandler_HandleCardinalityWithinBudget(b *testing.B) {
	h := handler.NewSamplingHandler(samDiscard{}, handler.SamplingConfig{
		Interval:    time.Hour,
		MinLevel:    slog.LevelInfo,
		Probability: 1,
		MaxKeys:     8192,
	})
	ctx := context.Background()
	records := make([]slog.Record, 4096)
	for i := range records {
		records[i] = samBenchRecord(fmt.Sprintf("event-%d", i))
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = h.Handle(ctx, records[i%len(records)])
	}
}

func BenchmarkSamplingHandler_WithAttrs(b *testing.B) {
	h := handler.NewSamplingHandler(samDiscard{}, handler.SamplingConfig{
		MinLevel:    slog.LevelInfo,
		Probability: 1,
	})
	attrs := []slog.Attr{slog.String("request_id", "01JABCDEF")}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = h.WithAttrs(attrs)
	}
}

func BenchmarkDefaultSamplingKey(b *testing.B) {
	ctx := context.Background()
	rec := samBenchRecord("some log message")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = handler.DefaultSamplingKey(ctx, rec)
	}
}

// Probability path: math/rand/v2, not crypto/rand.
func BenchmarkSamplingHandler_HandleProbability(b *testing.B) {
	h := handler.NewSamplingHandler(samDiscard{}, handler.SamplingConfig{
		MinLevel:    slog.LevelInfo,
		Probability: 0.5,
	})
	ctx := context.Background()
	rec := samBenchRecord("message")

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = h.Handle(ctx, rec)
	}
}
