package handler_test

import (
	"context"
	"log/slog"
	"testing"

	"github.com/pablogore/kit-logger/pkg/logger/handler"
)

func BenchmarkMultiHandler_Handle(b *testing.B) {
	h := handler.NewMultiHandler(discardHandler{}, discardHandler{}, discardHandler{})

	ctx := context.Background()
	rec := slog.NewRecord(benchTime(), slog.LevelInfo, "message", 0)
	rec.AddAttrs(slog.String("k1", "v1"), slog.Int("k2", 2))

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = h.Handle(ctx, rec)
	}
}
