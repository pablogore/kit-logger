package logger

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLevelHandler_HandleGatesEvenWithoutEnabled proves levelHandler does not
// rely on its caller having checked Enabled first. slog.Logger always does,
// but slog.Handler is a public interface -- a caller of a Config.Sink chain
// built by hand can call Handle directly, and the LevelVar must still be
// honored.
func TestLevelHandler_HandleGatesEvenWithoutEnabled(t *testing.T) {
	inner := newCapturingHandler()
	levelVar := new(slog.LevelVar)
	levelVar.Set(slog.LevelWarn)
	h := &levelHandler{next: inner, level: levelVar}

	below := slog.NewRecord(time.Now(), slog.LevelInfo, "below threshold", 0)
	require.NoError(t, h.Handle(context.Background(), below))
	assert.Empty(t, inner.getEntries(), "Handle must drop a record below the LevelVar even when Enabled was never consulted")

	atThreshold := slog.NewRecord(time.Now(), slog.LevelWarn, "at threshold", 0)
	require.NoError(t, h.Handle(context.Background(), atThreshold))
	require.Len(t, inner.getEntries(), 1, "a record at or above the LevelVar must still reach next")
}
