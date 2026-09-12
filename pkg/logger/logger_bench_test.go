package logger

import (
	"context"
	"testing"

	"github.com/pablogore/kit-logger/pkg/logger/handler"
)

// BenchmarkLogger_Info measures the cheapest possible call path: a logger
// with no decorators at all, sitting directly on a discarding sink.
func BenchmarkLogger_Info(b *testing.B) {
	log := New(Config{Sink: discardHandler{}})

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		log.Info("message", "k1", "v1", "k2", 2)
	}
}

// BenchmarkLogger_InfoContext_FullPipeline measures the opposite end: every
// decorator decorate() can add -- FilterRules, GlobalFields, component
// attribution, sampling and a context field extractor -- wired together, with
// InfoContext driving the extractor on every call.
func BenchmarkLogger_InfoContext_FullPipeline(b *testing.B) {
	log := New(Config{
		Sink: discardHandler{},
		FilterRules: []handler.FilterRule{
			{Key: "password", Value: "*"},
		},
		GlobalFields: map[string]string{
			"service": "bench-service",
		},
		Sampling: SamplingConfig{
			Enabled:     true,
			Interval:    0,
			Probability: 1,
			MinLevel:    LevelInfo,
		},
		ContextFields: func(context.Context) []any {
			return []any{"request_id", "bench-request"}
		},
	})

	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		log.InfoContext(ctx, "message", "k1", "v1", "k2", 2)
	}
}
