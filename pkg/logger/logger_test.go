package logger_test

import (
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/getsyntegrity/kit-logger/pkg/logger"
)

type contextKey string

const requestID contextKey = "request_id"

func TestMockLogger_RecordsInfoMessage(t *testing.T) {
	log := logger.NewMockLogger()
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
	log := logger.NewMockLogger()
	log.InfoContext(ctx, "user login")

	require.Len(t, log.Entries, 1)
	entry := log.Entries[0]
	require.Equal(t, "user login", entry.Message)
	require.Equal(t, ctx, entry.Context)
}

func TestMockLogger_HasMessage(t *testing.T) {
	log := logger.NewMockLogger()
	log.Info("operation completed")

	require.True(t, log.HasMessage("operation completed"))
	require.False(t, log.HasMessage("not found"))
}
