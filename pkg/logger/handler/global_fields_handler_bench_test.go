package handler_test

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/pablogore/kit-logger/pkg/logger/handler"
	"github.com/pablogore/kit-logger/pkg/logger/kitlogtest"
)

func BenchmarkGlobalFieldsHandler_Handle(b *testing.B) {
	h := handler.NewGlobalFieldsHandler(discardHandler{}, map[string]string{
		"service": "bench-service",
		"env":     "bench",
	}, false)

	ctx := context.Background()
	rec := slog.NewRecord(benchTime(), slog.LevelInfo, "message", 0)
	rec.AddAttrs(slog.String("k1", "v1"), slog.Int("k2", 2))

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = h.Handle(ctx, rec)
	}
}

// BenchmarkGlobalFieldsHandler measures the per-record cost of attaching
// global fields on each path Handle can take.
//
// "NoFields" is the pass-through when every configured key was empty.
// "NoCollision" is the common case: the record shares no key with the global
// set, so the fields are appended to a clone. "Collision" is the rebuild path,
// where a record attr is replaced in place by the global with the same key.
// "WithGroup" logs through a handler derived via WithGroup, which nests the
// record attrs under the group while the global fields stay at top level.
func BenchmarkGlobalFieldsHandler(b *testing.B) {
	sink := kitlogtest.NewTestHandler(func(context.Context, slog.Record) {})
	fields := map[string]string{"service": "checkout", "env": "prod", "version": "1.2.3"}
	ctx := context.Background()

	newRecord := func(attrs ...slog.Attr) slog.Record {
		r := slog.NewRecord(time.Now(), slog.LevelInfo, "bench", benchPC)
		r.AddAttrs(attrs...)
		return r
	}

	b.Run("NoFields", func(b *testing.B) {
		h := handler.NewGlobalFieldsHandler(sink, map[string]string{}, true)
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = h.Handle(ctx, newRecord(slog.String("method", "GET"), slog.Int("status", 200)))
		}
	})

	b.Run("NoCollision", func(b *testing.B) {
		h := handler.NewGlobalFieldsHandler(sink, fields, true)
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = h.Handle(ctx, newRecord(slog.String("method", "GET"), slog.Int("status", 200)))
		}
	})

	b.Run("Collision", func(b *testing.B) {
		h := handler.NewGlobalFieldsHandler(sink, fields, true)
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = h.Handle(ctx, newRecord(slog.String("method", "GET"), slog.String("env", "canary")))
		}
	})

	b.Run("WithGroup", func(b *testing.B) {
		h := handler.NewGlobalFieldsHandler(sink, fields, true).WithGroup("request")
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = h.Handle(ctx, newRecord(slog.String("method", "GET"), slog.Int("status", 200)))
		}
	})
}
