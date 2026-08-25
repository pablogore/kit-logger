package handler_test

import (
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/pablogore/kit-logger/pkg/logger/handler"
)

func TestFilterHandler_ExcludesMatchingKey(t *testing.T) {
	var received bool

	base := handler.NewTestHandler(func(_ context.Context, _ slog.Record) {
		received = true
	})

	h := handler.NewFilterHandler(base, []handler.FilterRule{
		{Key: "password"}, // si aparece esta key, se filtra
	})

	logger := slog.New(h)
	logger.Info("login attempt", "username", "alice", "password", "secret")

	require.False(t, received, "log should have been filtered due to 'password' key")
}

func TestFilterHandler_ExcludesMatchingKeyValue(t *testing.T) {
	var received bool

	base := handler.NewTestHandler(func(_ context.Context, _ slog.Record) {
		received = true
	})

	h := handler.NewFilterHandler(base, []handler.FilterRule{
		{Key: "error", Value: "unauthorized"},
	})

	logger := slog.New(h)
	logger.Error("login failed", "error", "unauthorized")

	require.False(t, received, "log should have been filtered due to matching key/value")
}

func TestFilterHandler_AllowsNonMatching(t *testing.T) {
	var received bool

	base := handler.NewTestHandler(func(_ context.Context, _ slog.Record) {
		received = true
	})

	h := handler.NewFilterHandler(base, []handler.FilterRule{
		{Key: "secret"},
	})

	logger := slog.New(h)
	logger.Info("normal log", "user", "bob")

	require.True(t, received, "log should have passed (no filter match)")
}

func TestFilterHandler_WithAttrs(t *testing.T) {
	var captured slog.Record
	base := handler.NewTestHandler(func(_ context.Context, r slog.Record) {
		captured = r
	})

	h := handler.NewFilterHandler(base, []handler.FilterRule{
		{Key: "password"},
	})

	// Create a new handler with additional attributes
	newHandler := h.WithAttrs([]slog.Attr{
		slog.String("service", "auth"),
		slog.Int("version", 1),
	})
	assert.NotNil(t, newHandler)
	assert.IsType(t, &handler.FilterHandler{}, newHandler)

	// Test that the new handler works correctly
	logger := slog.New(newHandler)
	logger.Info("test message", "user", "alice", "password", "secret")

	// Verify that the log was filtered (password key should trigger filter)
	assert.Empty(t, captured.Message, "Log should be filtered due to password key")

	// Test with non-filtered message
	logger.Info("test message", "user", "bob", "action", "login")

	// Verify that the log was captured with additional attributes
	assert.Equal(t, "test message", captured.Message)

	var hasService, hasVersion, hasUser, hasAction bool
	captured.Attrs(func(a slog.Attr) bool {
		switch a.Key {
		case "service":
			hasService = true
		case "version":
			hasVersion = true
		case "user":
			hasUser = true
		case "action":
			hasAction = true
		}
		return true
	})

	assert.True(t, hasService, "service attribute should be present")
	assert.True(t, hasVersion, "version attribute should be present")
	assert.True(t, hasUser, "user attribute should be present")
	assert.True(t, hasAction, "action attribute should be present")
}

func TestFilterHandler_WithGroup(t *testing.T) {
	var captured slog.Record
	base := handler.NewTestHandler(func(_ context.Context, r slog.Record) {
		captured = r
	})

	h := handler.NewFilterHandler(base, []handler.FilterRule{
		{Key: "secret"},
	})

	// Create a new handler with a group
	newHandler := h.WithGroup("user")
	assert.NotNil(t, newHandler)
	assert.IsType(t, &handler.FilterHandler{}, newHandler)

	// Test that the new handler works correctly
	logger := slog.New(newHandler)
	logger.Info("test message", "id", "123", "secret", "value")

	// Verify that the log was filtered (secret key should trigger filter)
	assert.Empty(t, captured.Message, "Log should be filtered due to secret key")

	// Test with non-filtered message
	logger.Info("test message", "id", "123", "name", "john")

	// Verify that the log was captured
	assert.Equal(t, "test message", captured.Message)
}

func TestFilterHandler_WithAttrs_EmptyAttrs(t *testing.T) {
	var captured slog.Record
	base := handler.NewTestHandler(func(_ context.Context, r slog.Record) {
		captured = r
	})

	h := handler.NewFilterHandler(base, []handler.FilterRule{
		{Key: "password"},
	})

	// Create a new handler with empty attributes
	newHandler := h.WithAttrs([]slog.Attr{})
	assert.NotNil(t, newHandler)
	assert.IsType(t, &handler.FilterHandler{}, newHandler)

	// Test that the new handler works correctly
	logger := slog.New(newHandler)
	logger.Info("test message", "user", "alice")

	// Verify that the log was captured
	assert.Equal(t, "test message", captured.Message)
}

func TestFilterHandler_WithGroup_EmptyGroup(t *testing.T) {
	var captured slog.Record
	base := handler.NewTestHandler(func(_ context.Context, r slog.Record) {
		captured = r
	})

	h := handler.NewFilterHandler(base, []handler.FilterRule{
		{Key: "secret"},
	})

	// Create a new handler with empty group name
	newHandler := h.WithGroup("")
	assert.NotNil(t, newHandler)
	assert.IsType(t, &handler.FilterHandler{}, newHandler)

	// Test that the new handler works correctly
	logger := slog.New(newHandler)
	logger.Info("test message", "user", "bob")

	// Verify that the log was captured
	assert.Equal(t, "test message", captured.Message)
}

func TestFilterHandler_WithAttrsAndGroup_Integration(t *testing.T) {
	var captured slog.Record
	base := handler.NewTestHandler(func(_ context.Context, r slog.Record) {
		captured = r
	})

	h := handler.NewFilterHandler(base, []handler.FilterRule{
		{Key: "password"},
		{Key: "token", Value: "invalid"},
	})

	// Create a handler with attributes
	handlerWithAttrs := h.WithAttrs([]slog.Attr{
		slog.String("service", "auth"),
		slog.Int("version", 2),
	})

	// Create a handler with group
	handlerWithGroup := handlerWithAttrs.WithGroup("user")

	// Test that the handler works correctly
	logger := slog.New(handlerWithGroup)
	logger.Info("login attempt", "username", "alice", "password", "secret")

	// Verify that the log was filtered (password key should trigger filter)
	assert.Empty(t, captured.Message, "Log should be filtered due to password key")

	// Test with non-filtered message
	logger.Info("login attempt", "username", "alice", "status", "success")

	// Verify that the log was captured with all attributes
	assert.Equal(t, "login attempt", captured.Message)

	var hasService, hasVersion, hasUsername, hasStatus bool
	captured.Attrs(func(a slog.Attr) bool {
		switch a.Key {
		case "service":
			hasService = true
		case "version":
			hasVersion = true
		case "username":
			hasUsername = true
		case "status":
			hasStatus = true
		}
		return true
	})

	assert.True(t, hasService, "service attribute should be present")
	assert.True(t, hasVersion, "version attribute should be present")
	assert.True(t, hasUsername, "username attribute should be present")
	assert.True(t, hasStatus, "status attribute should be present")
}

func TestFilterHandler_WithAttrs_PreservesRules(t *testing.T) {
	var captured slog.Record
	base := handler.NewTestHandler(func(_ context.Context, r slog.Record) {
		captured = r
	})

	// Create filter with specific rules
	rules := []handler.FilterRule{
		{Key: "password"},
		{Key: "secret", Value: "admin"},
	}
	h := handler.NewFilterHandler(base, rules)

	// Create a new handler with attributes
	newHandler := h.WithAttrs([]slog.Attr{
		slog.String("service", "auth"),
	})

	// Test that the filter rules are preserved
	logger := slog.New(newHandler)

	// Test password filter
	logger.Info("test", "password", "secret")
	assert.Empty(t, captured.Message, "Log should be filtered due to password key")

	// Test secret value filter
	logger.Info("test", "secret", "admin")
	assert.Empty(t, captured.Message, "Log should be filtered due to secret=admin")

	// Test non-filtered message
	logger.Info("test", "secret", "user")
	assert.Equal(t, "test", captured.Message, "Log should pass through")
}

func TestFilterHandler_WithGroup_PreservesRules(t *testing.T) {
	var captured slog.Record
	base := handler.NewTestHandler(func(_ context.Context, r slog.Record) {
		captured = r
	})

	// Create filter with specific rules
	rules := []handler.FilterRule{
		{Key: "token"},
		{Key: "api_key", Value: "invalid"},
	}
	h := handler.NewFilterHandler(base, rules)

	// Create a new handler with group
	newHandler := h.WithGroup("api")

	// Test that the filter rules are preserved
	logger := slog.New(newHandler)

	// Test token filter
	logger.Info("test", "token", "secret")
	assert.Empty(t, captured.Message, "Log should be filtered due to token key")

	// Test api_key value filter
	logger.Info("test", "api_key", "invalid")
	assert.Empty(t, captured.Message, "Log should be filtered due to api_key=invalid")

	// Test non-filtered message
	logger.Info("test", "api_key", "valid")
	assert.Equal(t, "test", captured.Message, "Log should pass through")
}
