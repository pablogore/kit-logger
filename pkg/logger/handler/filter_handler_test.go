package handler_test

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/pablogore/kit-logger/pkg/logger/handler"
	"github.com/pablogore/kit-logger/pkg/logger/kitlogtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFilterHandler_ExcludesMatchingKey(t *testing.T) {
	var received bool

	base := kitlogtest.NewTestHandler(func(_ context.Context, _ slog.Record) {
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

	base := kitlogtest.NewTestHandler(func(_ context.Context, _ slog.Record) {
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

	base := kitlogtest.NewTestHandler(func(_ context.Context, _ slog.Record) {
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
	base := kitlogtest.NewTestHandler(func(_ context.Context, r slog.Record) {
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
	base := kitlogtest.NewTestHandler(func(_ context.Context, r slog.Record) {
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
	base := kitlogtest.NewTestHandler(func(_ context.Context, r slog.Record) {
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
	base := kitlogtest.NewTestHandler(func(_ context.Context, r slog.Record) {
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
	base := kitlogtest.NewTestHandler(func(_ context.Context, r slog.Record) {
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

	// service/version were added before WithGroup, so they stay top-level;
	// username/status were added after, so a correct WithGroup nests them
	// inside "user".
	var hasService, hasVersion, hasUsername, hasStatus bool
	captured.Attrs(func(a slog.Attr) bool {
		switch {
		case a.Key == "service":
			hasService = true
		case a.Key == "version":
			hasVersion = true
		case a.Key == "user" && a.Value.Kind() == slog.KindGroup:
			for _, sub := range a.Value.Group() {
				switch sub.Key {
				case "username":
					hasUsername = true
				case "status":
					hasStatus = true
				}
			}
		}
		return true
	})

	assert.True(t, hasService, "service attribute should be present")
	assert.True(t, hasVersion, "version attribute should be present")
	assert.True(t, hasUsername, "username attribute should be present inside the user group")
	assert.True(t, hasStatus, "status attribute should be present inside the user group")
}

func TestFilterHandler_WithAttrs_PreservesRules(t *testing.T) {
	var captured slog.Record
	base := kitlogtest.NewTestHandler(func(_ context.Context, r slog.Record) {
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

// --- KITLOG-GO-016: With/WithAttrs/group bypass and redaction mode ---

func TestFilterHandler_WithFiltersAttribute(t *testing.T) {
	var received bool
	base := kitlogtest.NewTestHandler(func(_ context.Context, _ slog.Record) {
		received = true
	})

	h := handler.NewFilterHandler(base, []handler.FilterRule{{Key: "password"}})
	logger := slog.New(h).With("password", "secret")
	logger.Info("login")

	require.False(t, received, "a With-supplied password must be filtered, not just a record attr")
}

func TestFilterHandler_ChainedWithFiltersAttribute(t *testing.T) {
	var received bool
	base := kitlogtest.NewTestHandler(func(_ context.Context, _ slog.Record) {
		received = true
	})

	h := handler.NewFilterHandler(base, []handler.FilterRule{{Key: "token"}})
	logger := slog.New(h).With("user", "u").With("token", "t")
	logger.Info("login")

	require.False(t, received, "a token attached through a chain of With calls must still be filtered")
}

func TestFilterHandler_FiltersAttributeInsideGroup(t *testing.T) {
	var received bool
	base := kitlogtest.NewTestHandler(func(_ context.Context, _ slog.Record) {
		received = true
	})

	h := handler.NewFilterHandler(base, []handler.FilterRule{{Key: "password"}})
	logger := slog.New(h)
	logger.Info("credential rotated", slog.Group("credential", slog.String("password", "s")))

	require.False(t, received, "a password nested inside a group must be filtered by its bare key")
}

func TestFilterHandler_DottedPathRuleMatchesOnlyNestedAttr(t *testing.T) {
	nestedFiltered := func() bool {
		var received bool
		base := kitlogtest.NewTestHandler(func(_ context.Context, _ slog.Record) {
			received = true
		})
		h := handler.NewFilterHandler(base, []handler.FilterRule{{Key: "credential.password"}})
		slog.New(h).Info("msg", slog.Group("credential", slog.String("password", "s")))
		return !received
	}()
	require.True(t, nestedFiltered, "credential.password rule must match the nested attr")

	topLevelFiltered := func() bool {
		var received bool
		base := kitlogtest.NewTestHandler(func(_ context.Context, _ slog.Record) {
			received = true
		})
		h := handler.NewFilterHandler(base, []handler.FilterRule{{Key: "credential.password"}})
		slog.New(h).Info("msg", "password", "s")
		return !received
	}()
	require.False(t, topLevelFiltered, "credential.password rule must not match a top-level password")
}

func TestFilterHandler_WithGroupFiltersRecordAttrByBareKey(t *testing.T) {
	var received bool
	base := kitlogtest.NewTestHandler(func(_ context.Context, _ slog.Record) {
		received = true
	})

	h := handler.NewFilterHandler(base, []handler.FilterRule{{Key: "password"}})
	logger := slog.New(h).WithGroup("auth")
	logger.Info("msg", "password", "s")

	require.False(t, received, "a rule must apply to a record attr nested by WithGroup")
}

func TestFilterHandler_KeyMatchingIsCaseInsensitive(t *testing.T) {
	var received bool
	base := kitlogtest.NewTestHandler(func(_ context.Context, _ slog.Record) {
		received = true
	})

	h := handler.NewFilterHandler(base, []handler.FilterRule{{Key: "Password"}})
	slog.New(h).Info("msg", "password", "s")

	require.False(t, received, "key matching must be case-insensitive")
}

// TestFilterHandler_SiblingsFromSameParentDoNotAlias pins the backing-array
// aliasing regression: two loggers derived from the same parent via With must
// not observe attrs the other one added, even after the parent's held ops
// slice has spare capacity to grow into.
func TestFilterHandler_SiblingsFromSameParentDoNotAlias(t *testing.T) {
	var capturedA, capturedB slog.Record
	base := kitlogtest.NewTestHandler(func(_ context.Context, r slog.Record) {
		// Whichever sibling logs, capture into both so either overwrite is visible.
		if capturedA.Message == "" {
			capturedA = r
			return
		}
		capturedB = r
	})

	var parent *handler.FilterHandler = handler.NewFilterHandler(base, []handler.FilterRule{{Key: "nonexistent"}})
	// Grow the held ops across several calls -- Go's allocator commonly rounds
	// a small slice's backing array up to the next size class, leaving spare
	// capacity a later append could silently reuse without slices.Clip -- then
	// branch two independent children from the same parent.
	for i := 0; i < 8; i++ {
		parent = parent.WithAttrs([]slog.Attr{slog.Int("seed", i)}).(*handler.FilterHandler)
	}

	childA := parent.WithAttrs([]slog.Attr{slog.String("only_a", "a")})
	childB := parent.WithAttrs([]slog.Attr{slog.String("only_b", "b")})

	slog.New(childA).Info("from a")
	slog.New(childB).Info("from b")

	hasKey := func(r slog.Record, key string) bool {
		found := false
		r.Attrs(func(a slog.Attr) bool {
			if a.Key == key {
				found = true
			}
			return true
		})
		return found
	}

	require.True(t, hasKey(capturedA, "only_a"))
	require.False(t, hasKey(capturedA, "only_b"), "sibling B's attr must not leak into A's record")
	require.True(t, hasKey(capturedB, "only_b"))
	require.False(t, hasKey(capturedB, "only_a"), "sibling A's attr must not leak into B's record")
}

func TestFilterHandler_RedactModeKeepsRecordAndReplacesOnlyMatch(t *testing.T) {
	var captured slog.Record
	base := kitlogtest.NewTestHandler(func(_ context.Context, r slog.Record) {
		captured = r
	})

	h := handler.NewFilterHandlerWithMode(base, []handler.FilterRule{{Key: "password"}}, handler.ModeRedact)
	logger := slog.New(h).With("password", "secret")
	before := time.Now()
	logger.Info("login", "user", "alice")

	require.Equal(t, "login", captured.Message, "ModeRedact must keep the record")
	require.False(t, captured.Time.Before(before.Add(-time.Second)), "ModeRedact must preserve a sane Time")

	var gotPassword, gotUser string
	captured.Attrs(func(a slog.Attr) bool {
		switch a.Key {
		case "password":
			gotPassword = a.Value.String()
		case "user":
			gotUser = a.Value.String()
		}
		return true
	})

	assert.Equal(t, "[REDACTED]", gotPassword, "the matching value must be replaced")
	assert.Equal(t, "alice", gotUser, "every other attr must be left byte-identical")
}

func TestFilterHandler_RedactModeUsesCustomReplacement(t *testing.T) {
	var captured slog.Record
	base := kitlogtest.NewTestHandler(func(_ context.Context, r slog.Record) {
		captured = r
	})

	h := handler.NewFilterHandlerWithMode(base, []handler.FilterRule{
		{Key: "token", Replacement: "***"},
	}, handler.ModeRedact)
	slog.New(h).Info("msg", "token", "abc123")

	var got string
	captured.Attrs(func(a slog.Attr) bool {
		if a.Key == "token" {
			got = a.Value.String()
		}
		return true
	})
	assert.Equal(t, "***", got)
}

func TestFilterHandler_RedactModePreservesLevelAndPC(t *testing.T) {
	var captured slog.Record
	base := kitlogtest.NewTestHandler(func(_ context.Context, r slog.Record) {
		captured = r
	})

	h := handler.NewFilterHandlerWithMode(base, []handler.FilterRule{{Key: "password"}}, handler.ModeRedact)
	logger := slog.New(h)
	logger.Error("oops", "password", "secret")

	require.Equal(t, slog.LevelError, captured.Level)
	require.NotZero(t, captured.PC, "ModeRedact must preserve the caller PC")
}

func TestFilterHandler_DropModeStillDropsRecordAttrs(t *testing.T) {
	var received bool
	base := kitlogtest.NewTestHandler(func(_ context.Context, _ slog.Record) {
		received = true
	})

	h := handler.NewFilterHandler(base, []handler.FilterRule{{Key: "password"}})
	slog.New(h).Info("msg", "password", "s")

	require.False(t, received, "ModeDrop (the NewFilterHandler default) must keep discarding the whole record")
}

func TestFilterHandler_ZeroRulesFastPathForwardsImmediately(t *testing.T) {
	// probe records every WithAttrs/WithGroup call it receives directly, so
	// this test fails if FilterHandler defers them instead of forwarding at
	// call time when there are no rules to check.
	probe := &filterFastPathProbe{}

	h := handler.NewFilterHandler(probe, nil)
	h.WithAttrs([]slog.Attr{slog.String("a", "b")})
	h.WithGroup("g")

	require.Equal(t, 1, probe.withAttrsCalls, "WithAttrs must forward to next immediately with zero rules")
	require.Equal(t, 1, probe.withGroupCalls, "WithGroup must forward to next immediately with zero rules")
}

type filterFastPathProbe struct {
	withAttrsCalls int
	withGroupCalls int
}

func (p *filterFastPathProbe) Enabled(context.Context, slog.Level) bool  { return true }
func (p *filterFastPathProbe) Handle(context.Context, slog.Record) error { return nil }
func (p *filterFastPathProbe) WithAttrs([]slog.Attr) slog.Handler {
	p.withAttrsCalls++
	return p
}
func (p *filterFastPathProbe) WithGroup(string) slog.Handler {
	p.withGroupCalls++
	return p
}

func TestFilterHandler_WithGroup_PreservesRules(t *testing.T) {
	var captured slog.Record
	base := kitlogtest.NewTestHandler(func(_ context.Context, r slog.Record) {
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
