package utils_test

import (
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/pablogore/kit-logger/pkg/logger/utils"
)

// TestExtractAttrs_ReturnsAllAttributes tests that ExtractAttrs function returns all attributes from a log record.
func TestExtractAttrs_ReturnsAllAttributes(t *testing.T) {
	// Create a new log record with a timestamp, level, message, and no error.
	record := slog.NewRecord(
		time.Now(),
		slog.LevelInfo,
		"test message",
		0,
	)
	// Add attributes to the log record.
	record.AddAttrs(
		slog.String("user", "alice"),
		slog.Int("id", 42),
		slog.Bool("active", true),
	)

	// Extract attributes from the log record.
	attrs := utils.ExtractAttrs(record)

	// Assert that the extracted attributes match the expected values.
	require.Equal(t, "alice", attrs["user"])
	require.Equal(t, int64(42), attrs["id"])
	require.Equal(t, true, attrs["active"])
	require.Len(t, attrs, 3)
}
