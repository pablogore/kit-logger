package logger_test

import (
	"context"
	"log/slog"
	"testing"

	"github.com/pablogore/kit-logger/pkg/logger"
	"github.com/pablogore/kit-logger/pkg/logger/kitlogtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type contextKey string

const requestID contextKey = "request_id"

func TestMockLogger_RecordsInfoMessage(t *testing.T) {
	log := kitlogtest.NewMockLogger()
	log.Info("starting service", "port", 8080)

	require.Len(t, log.Entries, 1)
	entry := log.Entries[0]
	require.Equal(t, "starting service", entry.Message)
	require.Equal(t, "port", entry.Args[0])
	require.Equal(t, 8080, entry.Args[1])
	require.Equal(t, slog.LevelInfo, entry.Level)
}

func TestMockLogger_ContextLogging(t *testing.T) {
	ctx := context.WithValue(context.Background(), requestID, "abc-123")
	log := kitlogtest.NewMockLogger()
	log.InfoContext(ctx, "user login")

	require.Len(t, log.Entries, 1)
	entry := log.Entries[0]
	require.Equal(t, "user login", entry.Message)
	require.Equal(t, ctx, entry.Context)
}

func TestMockLogger_HasMessage(t *testing.T) {
	log := kitlogtest.NewMockLogger()
	log.Info("operation completed")

	require.True(t, log.HasMessage("operation completed"))
	require.False(t, log.HasMessage("not found"))
}

func TestSetGlobal(t *testing.T) {
	mockLogger := kitlogtest.NewMockLogger()
	logger.SetGlobal(mockLogger)

	// Verify global logger is set -- by identity, so a copy or a torn value
	// would not pass.
	assert.Same(t, mockLogger, logger.L())
}

func TestL_WithExistingLogger(t *testing.T) {
	mockLogger := kitlogtest.NewMockLogger()
	logger.SetGlobal(mockLogger)

	got := logger.L()
	assert.Equal(t, mockLogger, got)
}
