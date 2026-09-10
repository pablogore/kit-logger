package logger_test

import (
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/pablogore/kit-logger/pkg/logger"
	"github.com/pablogore/kit-logger/pkg/logger/utils"
)

type requestIDKey struct{}

func requestIDFields(ctx context.Context) []any {
	if id, ok := ctx.Value(requestIDKey{}).(string); ok {
		return []any{"request_id", id}
	}
	return nil
}

func TestContextFields_AppliedByContextLogMethods(t *testing.T) {
	rec := &recorder{}
	log := logger.New(logger.Config{
		Handler:       rec.handler(),
		ContextFields: requestIDFields,
	})

	ctx := context.WithValue(context.Background(), requestIDKey{}, "req-42")

	// Every *Context method, not just one: the old behaviour was that none of
	// them consulted the extractor.
	log.DebugContext(ctx, "debug line")
	log.InfoContext(ctx, "info line")
	log.WarnContext(ctx, "warn line")
	log.ErrorContext(ctx, "error line")
	log.Log(ctx, slog.LevelInfo, "log line")

	for _, msg := range []string{"info line", "warn line", "error line", "log line"} {
		got, ok := rec.find(msg)
		require.True(t, ok, msg)
		require.Equal(t, "req-42", utils.ExtractAttrs(got)["request_id"], msg)
	}
}

func TestContextFields_AddNothingWhenTheContextCarriesNone(t *testing.T) {
	rec := &recorder{}
	log := logger.New(logger.Config{
		Handler:       rec.handler(),
		ContextFields: requestIDFields,
	})

	log.InfoContext(context.Background(), "bare")

	got, ok := rec.find("bare")
	require.True(t, ok)
	require.NotContains(t, utils.ExtractAttrs(got), "request_id")
}
