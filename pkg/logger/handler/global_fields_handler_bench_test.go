package handler_test

import (
	"context"
	"log/slog"
	"testing"

	"github.com/pablogore/kit-logger/pkg/logger/handler"
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
