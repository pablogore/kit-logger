package utils_test

import (
	"log/slog"
	"testing"
	"time"

	"github.com/pablogore/kit-logger/pkg/logger/utils"
	"github.com/stretchr/testify/require"
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

// TestExtractAttrList_PreservesOrderAndDuplicates tests that ExtractAttrList
// returns attributes in insertion order and does not collapse repeated keys,
// which is what makes it usable for uniqueness assertions.
func TestExtractAttrList_PreservesOrderAndDuplicates(t *testing.T) {
	record := slog.NewRecord(time.Now(), slog.LevelInfo, "test message", 0)
	record.AddAttrs(
		slog.String("env", "production"),
		slog.String("method", "GET"),
		slog.String("env", "staging"),
	)

	attrs := utils.ExtractAttrList(record)

	require.Len(t, attrs, 3)
	require.Equal(t, "env", attrs[0].Key)
	require.Equal(t, "production", attrs[0].Value.String())
	require.Equal(t, "method", attrs[1].Key)
	require.Equal(t, "env", attrs[2].Key)
	require.Equal(t, "staging", attrs[2].Value.String())

	// The map view collapses the same record to two keys, last write wins.
	require.Len(t, utils.ExtractAttrs(record), 2)
}

// TestExtractAttrList_EmptyRecord tests that a record with no attributes
// yields an empty, non-nil slice.
func TestExtractAttrList_EmptyRecord(t *testing.T) {
	record := slog.NewRecord(time.Now(), slog.LevelInfo, "test message", 0)

	attrs := utils.ExtractAttrList(record)

	require.NotNil(t, attrs)
	require.Empty(t, attrs)
}
