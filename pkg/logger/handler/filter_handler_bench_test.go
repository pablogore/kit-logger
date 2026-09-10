package handler_test

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/pablogore/kit-logger/pkg/logger/handler"
)

type filterDiscard struct{}

func (filterDiscard) Enabled(context.Context, slog.Level) bool  { return true }
func (filterDiscard) Handle(context.Context, slog.Record) error { return nil }
func (d filterDiscard) WithAttrs([]slog.Attr) slog.Handler      { return d }
func (d filterDiscard) WithGroup(string) slog.Handler           { return d }

func filterBenchRecord() slog.Record {
	r := slog.NewRecord(time.Unix(0, 0), slog.LevelInfo, "message", 0)
	r.AddAttrs(slog.String("user", "alice"), slog.Int("status", 200))
	return r
}

// No rules: the fast path this fix must not regress -- WithAttrs forwards to
// next immediately and Handle skips all filtering.
func BenchmarkFilterHandler_Handle_NoRules(b *testing.B) {
	h := handler.NewFilterHandler(filterDiscard{}, nil)
	ctx := context.Background()
	rec := filterBenchRecord()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = h.Handle(ctx, rec)
	}
}

// Rules configured, no With-supplied attrs: only the record's own attrs are
// walked per record.
func BenchmarkFilterHandler_Handle_RulesNoWithAttrs(b *testing.B) {
	h := handler.NewFilterHandler(filterDiscard{}, []handler.FilterRule{{Key: "password"}})
	ctx := context.Background()
	rec := filterBenchRecord()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = h.Handle(ctx, rec)
	}
}

// Rules configured with 10 chained With attrs: the cost this fix accepts --
// slog's WithAttrs pre-formatting is forfeited so the held attrs can be
// checked against the rules on every record.
func BenchmarkFilterHandler_Handle_RulesWith10Attrs(b *testing.B) {
	var h slog.Handler = handler.NewFilterHandler(filterDiscard{}, []handler.FilterRule{{Key: "password"}})
	for i := 0; i < 10; i++ {
		h = h.WithAttrs([]slog.Attr{slog.Int("field", i)})
	}
	ctx := context.Background()
	rec := filterBenchRecord()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = h.Handle(ctx, rec)
	}
}
