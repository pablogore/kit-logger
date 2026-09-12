package otel_test

import (
	"context"

	"github.com/pablogore/kit-logger/pkg/logger"
	kitotel "github.com/pablogore/kit-logger/pkg/logger/otel"
)

// Example mirrors the README's trace-correlation section. It has no Output
// comment: the JSON line carries a wall-clock timestamp, and without an
// active span it carries no trace fields at all. What it verifies is that
// the snippet compiles against the real Config.ContextHandler seam and the
// real Decorator/Options API.
func Example() {
	log := logger.New(logger.Config{
		Level:          logger.LevelInfo,
		Format:         logger.FormatJSON,
		ContextHandler: kitotel.Decorator(kitotel.Options{}),
	})

	ctx := context.Background() // a span in ctx adds trace_id and span_id
	log.ErrorContext(ctx, "stock check failed")
}
