package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/pablogore/kit-logger/pkg/logger/handler"
	"github.com/pablogore/kit-logger/pkg/logger/kitlogtest"
	"github.com/pablogore/kit-logger/pkg/logger/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// captureRecord returns a TestHandler that stores the last record it
// receives in *dst, with WithAttrs/WithGroup nesting rebuilt the way a real
// handler would emit it.
func captureRecord(dst *slog.Record) slog.Handler {
	return kitlogtest.NewTestHandler(func(_ context.Context, r slog.Record) { *dst = r })
}

// topKeys returns the top-level keys of r in order. Unlike a map view, it
// keeps duplicates, so asserting on it proves a key appears exactly once.
func topKeys(r slog.Record) []string {
	attrs := utils.ExtractAttrList(r)
	keys := make([]string, 0, len(attrs))
	for _, a := range attrs {
		keys = append(keys, a.Key)
	}
	return keys
}

// attrsWithKey returns every top-level attr of r whose key is key, in order.
func attrsWithKey(r slog.Record, key string) []slog.Attr {
	var out []slog.Attr
	for _, a := range utils.ExtractAttrList(r) {
		if a.Key == key {
			out = append(out, a)
		}
	}
	return out
}

// requireSingleAttr asserts that r carries key exactly once at the top level
// and that its value is want.
func requireSingleAttr(t *testing.T, r slog.Record, key, want string) {
	t.Helper()
	got := attrsWithKey(r, key)
	require.Len(t, got, 1, "record must carry %q exactly once, keys: %v", key, topKeys(r))
	require.Equal(t, want, got[0].Value.String())
}

// requireUniqueKeys asserts that no two top-level attrs of r share a key.
func requireUniqueKeys(t *testing.T, r slog.Record) {
	t.Helper()
	seen := map[string]struct{}{}
	for _, k := range topKeys(r) {
		_, dup := seen[k]
		require.False(t, dup, "duplicate key %q in %v", k, topKeys(r))
		seen[k] = struct{}{}
	}
}

// nestedValue walks path through nested groups and returns the leaf value.
func nestedValue(r slog.Record, path ...string) (slog.Value, bool) {
	attrs := utils.ExtractAttrList(r)
	for i, key := range path {
		var next []slog.Attr
		found := false
		for _, a := range attrs {
			if a.Key != key {
				continue
			}
			if i == len(path)-1 {
				return a.Value, true
			}
			if a.Value.Kind() != slog.KindGroup {
				return slog.Value{}, false
			}
			next = a.Value.Group()
			found = true
			break
		}
		if !found {
			return slog.Value{}, false
		}
		attrs = next
	}
	return slog.Value{}, false
}

// dropTime removes the time attr so two JSON lines can be compared byte for
// byte.
func dropTime(groups []string, a slog.Attr) slog.Attr {
	if len(groups) == 0 && a.Key == slog.TimeKey {
		return slog.Attr{}
	}
	return a
}

// --- override semantics -------------------------------------------------

func TestGlobalFieldsHandler_OverrideTrue_ReplacesInPlace(t *testing.T) {
	var captured slog.Record
	h := handler.NewGlobalFieldsHandler(captureRecord(&captured), map[string]string{
		"env": "staging",
	}, true)

	slog.New(h).Info("request received", "method", "GET", "env", "production", "path", "/x")

	requireSingleAttr(t, captured, "env", "staging")
	require.Equal(t, 3, captured.NumAttrs())
	require.Equal(t, []string{"method", "env", "path"}, topKeys(captured),
		"the global field must take the position of the attr it replaces")
}

func TestGlobalFieldsHandler_OverrideFalse_RecordWins(t *testing.T) {
	var captured slog.Record
	h := handler.NewGlobalFieldsHandler(captureRecord(&captured), map[string]string{
		"env": "staging",
	}, false)

	slog.New(h).Info("request received", "method", "GET", "env", "production", "path", "/x")

	requireSingleAttr(t, captured, "env", "production")
	require.Equal(t, 3, captured.NumAttrs())
	require.Equal(t, []string{"method", "env", "path"}, topKeys(captured))
}

func TestGlobalFieldsHandler_AppendsMissingFieldsInSortedOrder(t *testing.T) {
	var captured slog.Record
	h := handler.NewGlobalFieldsHandler(captureRecord(&captured), map[string]string{
		"zeta":  "z",
		"alpha": "a",
		"mid":   "m",
	}, true)

	slog.New(h).Info("request received", "method", "GET", "mid", "record")

	require.Equal(t, []string{"method", "mid", "alpha", "zeta"}, topKeys(captured))
	requireSingleAttr(t, captured, "mid", "m")
	requireSingleAttr(t, captured, "alpha", "a")
	requireSingleAttr(t, captured, "zeta", "z")
}

func TestGlobalFieldsHandler_NeverEmitsDuplicateKeys(t *testing.T) {
	fields := map[string]string{"service": "checkout", "env": "prod", "version": "1"}

	cases := []struct {
		name string
		args []any
	}{
		{"no collision", []any{"method", "GET"}},
		{"one collision", []any{"env", "canary", "method", "GET"}},
		{"every key collides", []any{"version", "9", "service", "cart", "env", "canary"}},
		{"no record attrs", nil},
		{"collision inside a group is a different path", []any{slog.Group("meta", "env", "canary")}},
	}

	for _, override := range []bool{true, false} {
		for _, tc := range cases {
			name := tc.name + "/override=false"
			if override {
				name = tc.name + "/override=true"
			}
			t.Run(name, func(t *testing.T) {
				var captured slog.Record
				h := handler.NewGlobalFieldsHandler(captureRecord(&captured), fields, override)
				slog.New(h).Info("msg", tc.args...)

				requireUniqueKeys(t, captured)
				for _, k := range []string{"service", "env", "version"} {
					require.Len(t, attrsWithKey(captured, k), 1, "global key %q must be present exactly once", k)
				}
			})
		}
	}
}

func TestGlobalFieldsHandler_OverrideTrue_CollapsesRecordRepeatsOfGlobalKey(t *testing.T) {
	// The record itself repeats "env". Replacing both with the global would
	// make the handler the author of a duplicate; only the first is kept.
	var captured slog.Record
	h := handler.NewGlobalFieldsHandler(captureRecord(&captured), map[string]string{
		"env": "staging",
	}, true)

	slog.New(h).Info("msg", "env", "a", "method", "GET", "env", "b")

	requireSingleAttr(t, captured, "env", "staging")
	require.Equal(t, []string{"env", "method"}, topKeys(captured))
}

func TestGlobalFieldsHandler_JSONOutput_SingleEnvMember(t *testing.T) {
	var buf bytes.Buffer
	h := handler.NewGlobalFieldsHandler(slog.NewJSONHandler(&buf, nil), map[string]string{
		"env": "staging",
	}, true)

	slog.New(h).Info("request received", "env", "production", "method", "GET")

	line := buf.String()
	require.Equal(t, 1, strings.Count(line, `"env"`), "raw JSON must contain the env member once: %s", line)

	// Decode token by token: a map[string]any would silently collapse a
	// duplicate member, hiding exactly the bug this test guards against.
	dec := json.NewDecoder(strings.NewReader(line))
	tok, err := dec.Token()
	require.NoError(t, err)
	require.Equal(t, json.Delim('{'), tok)

	envMembers := 0
	var envValue string
	for dec.More() {
		keyTok, err := dec.Token()
		require.NoError(t, err)
		var v any
		require.NoError(t, dec.Decode(&v))
		if keyTok == "env" {
			envMembers++
			envValue, _ = v.(string)
		}
	}
	require.Equal(t, 1, envMembers)
	require.Equal(t, "staging", envValue)
}

func TestGlobalFieldsHandler_DeterministicOrderAcrossConstructions(t *testing.T) {
	fields := map[string]string{"zeta": "z", "alpha": "a", "mid": "m", "beta": "b"}
	want := []string{"msg_attr", "alpha", "beta", "mid", "zeta"}

	for i := 0; i < 100; i++ {
		var captured slog.Record
		h := handler.NewGlobalFieldsHandler(captureRecord(&captured), fields, true)
		slog.New(h).Info("msg", "msg_attr", i)
		require.Equal(t, want, topKeys(captured), "construction %d produced a different order", i)
	}
}

// --- With / WithAttrs ---------------------------------------------------

// globalFieldsProbe records every WithAttrs/WithGroup call it receives
// directly from GlobalFieldsHandler.
type globalFieldsProbe struct {
	withAttrs      [][]slog.Attr
	withGroupCalls int
}

func (p *globalFieldsProbe) Enabled(context.Context, slog.Level) bool  { return true }
func (p *globalFieldsProbe) Handle(context.Context, slog.Record) error { return nil }
func (p *globalFieldsProbe) WithAttrs(attrs []slog.Attr) slog.Handler {
	p.withAttrs = append(p.withAttrs, attrs)
	return p
}
func (p *globalFieldsProbe) WithGroup(string) slog.Handler {
	p.withGroupCalls++
	return p
}

func TestGlobalFieldsHandler_WithAttrs_DelegatesToNext(t *testing.T) {
	probe := &globalFieldsProbe{}
	h := handler.NewGlobalFieldsHandler(probe, map[string]string{"env": "staging"}, true)

	attrs := []slog.Attr{slog.String("component", "api"), slog.Int("shard", 3)}
	derived := h.WithAttrs(attrs)

	require.Len(t, probe.withAttrs, 1, "WithAttrs must forward to next exactly once")
	require.Equal(t, attrs, probe.withAttrs[0])
	assert.IsType(t, &handler.GlobalFieldsHandler{}, derived)
}

func TestGlobalFieldsHandler_WithAttrs_NotFoldedIntoGlobalFields(t *testing.T) {
	var captured slog.Record
	h := handler.NewGlobalFieldsHandler(captureRecord(&captured), map[string]string{
		"env": "staging",
	}, false)

	derived := h.WithAttrs([]slog.Attr{slog.String("component", "api")})
	slog.New(derived).Info("request received", "method", "GET")

	// The With attr is pre-formatted by next, so it arrives before the
	// record attrs; the global field is appended by Handle, after them.
	require.Equal(t, []string{"component", "method", "env"}, topKeys(captured))
	requireSingleAttr(t, captured, "component", "api")
	requireSingleAttr(t, captured, "env", "staging")
}

func TestGlobalFieldsHandler_WithAttrs_EmptyOrInvalidReturnsReceiver(t *testing.T) {
	probe := &globalFieldsProbe{}
	h := handler.NewGlobalFieldsHandler(probe, map[string]string{"env": "staging"}, true)

	require.Same(t, h, h.WithAttrs(nil))
	require.Same(t, h, h.WithAttrs([]slog.Attr{slog.String("", "dropped")}))
	require.Empty(t, probe.withAttrs, "nothing valid to forward")
}

// --- WithGroup ----------------------------------------------------------

func TestGlobalFieldsHandler_WithGroup_MatchesBareSlogChain(t *testing.T) {
	// The reference is a plain slog.JSONHandler with the global fields
	// attached at the root, before the group: that is what "global" means.
	globals := map[string]string{"env": "staging", "service": "auth"}
	globalAttrs := []slog.Attr{slog.String("env", "staging"), slog.String("service", "auth")}

	type build func(base slog.Handler) slog.Handler
	cases := []struct {
		name string
		ours build
		bare build
		log  func(l *slog.Logger)
	}{
		{
			name: "record attrs inside group, globals outside",
			ours: func(b slog.Handler) slog.Handler {
				return handler.NewGlobalFieldsHandler(b, globals, true).WithGroup("g")
			},
			bare: func(b slog.Handler) slog.Handler { return b.WithAttrs(globalAttrs).WithGroup("g") },
			log:  func(l *slog.Logger) { l.Info("msg", "k", "v", "n", 1) },
		},
		{
			name: "WithAttrs after WithGroup nests inside the group",
			ours: func(b slog.Handler) slog.Handler {
				return handler.NewGlobalFieldsHandler(b, globals, true).WithGroup("g").
					WithAttrs([]slog.Attr{slog.String("held", "x")})
			},
			bare: func(b slog.Handler) slog.Handler {
				return b.WithAttrs(globalAttrs).WithGroup("g").WithAttrs([]slog.Attr{slog.String("held", "x")})
			},
			log: func(l *slog.Logger) { l.Info("msg", "k", "v") },
		},
		{
			name: "nested groups",
			ours: func(b slog.Handler) slog.Handler {
				return handler.NewGlobalFieldsHandler(b, globals, true).WithGroup("a").WithGroup("b")
			},
			bare: func(b slog.Handler) slog.Handler { return b.WithAttrs(globalAttrs).WithGroup("a").WithGroup("b") },
			log:  func(l *slog.Logger) { l.Info("msg", "k", "v") },
		},
		{
			name: "record attr sharing a global key inside a group is not a collision",
			ours: func(b slog.Handler) slog.Handler {
				return handler.NewGlobalFieldsHandler(b, globals, true).WithGroup("g")
			},
			bare: func(b slog.Handler) slog.Handler { return b.WithAttrs(globalAttrs).WithGroup("g") },
			log:  func(l *slog.Logger) { l.Info("msg", "env", "production") },
		},
		{
			name: "no record attrs elides the group",
			ours: func(b slog.Handler) slog.Handler {
				return handler.NewGlobalFieldsHandler(b, globals, true).WithGroup("g")
			},
			bare: func(b slog.Handler) slog.Handler { return b.WithAttrs(globalAttrs).WithGroup("g") },
			log:  func(l *slog.Logger) { l.Info("msg") },
		},
		{
			name: "inline slog.Group inside the open group",
			ours: func(b slog.Handler) slog.Handler {
				return handler.NewGlobalFieldsHandler(b, globals, true).WithGroup("g")
			},
			bare: func(b slog.Handler) slog.Handler { return b.WithAttrs(globalAttrs).WithGroup("g") },
			log:  func(l *slog.Logger) { l.Info("msg", slog.Group("inner", "k", "v")) },
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var ours, bare bytes.Buffer
			opts := &slog.HandlerOptions{ReplaceAttr: dropTime}

			tc.log(slog.New(tc.ours(slog.NewJSONHandler(&ours, opts))))
			tc.log(slog.New(tc.bare(slog.NewJSONHandler(&bare, opts))))

			require.Equal(t, bare.String(), ours.String())
		})
	}
}

func TestGlobalFieldsHandler_WithGroup_HoldsLaterCallsInsteadOfForwarding(t *testing.T) {
	// Once a group is open, forwarding WithAttrs or WithGroup to next would
	// put the global fields (added at Handle time) inside the group. Both
	// must be held.
	probe := &globalFieldsProbe{}
	h := handler.NewGlobalFieldsHandler(probe, map[string]string{"env": "staging"}, true)

	h.WithGroup("g").WithAttrs([]slog.Attr{slog.String("a", "b")}).WithGroup("h")

	require.Equal(t, 0, probe.withGroupCalls)
	require.Empty(t, probe.withAttrs)
}

func TestGlobalFieldsHandler_WithGroup_NoFieldsForwardsImmediately(t *testing.T) {
	// With nothing to keep outside the group there is no reason to give up
	// slog's pre-formatting, so the calls go straight to next.
	probe := &globalFieldsProbe{}
	h := handler.NewGlobalFieldsHandler(probe, map[string]string{}, true)

	h.WithGroup("g")
	h.WithAttrs([]slog.Attr{slog.String("a", "b")})

	require.Equal(t, 1, probe.withGroupCalls)
	require.Len(t, probe.withAttrs, 1)
}

func TestGlobalFieldsHandler_WithGroup_EmptyNameReturnsReceiver(t *testing.T) {
	var captured slog.Record
	h := handler.NewGlobalFieldsHandler(captureRecord(&captured), map[string]string{
		"env": "production",
	}, false)

	require.Same(t, h, h.WithGroup(""))

	slog.New(h.WithGroup("")).Info("test message", "key", "value")
	require.Equal(t, []string{"key", "env"}, topKeys(captured))
}

func TestGlobalFieldsHandler_WithGroup_SiblingsDoNotShareHeldOps(t *testing.T) {
	var captured slog.Record
	base := handler.NewGlobalFieldsHandler(captureRecord(&captured), map[string]string{
		"env": "staging",
	}, true)

	// Grow the parent's ops so a naive append would have spare capacity to
	// hand out to both children.
	parent := base.WithGroup("g")
	for i := 0; i < 4; i++ {
		parent = parent.WithAttrs([]slog.Attr{slog.Int("seed", i)})
	}
	childA := parent.WithAttrs([]slog.Attr{slog.String("only_a", "a")})
	childB := parent.WithAttrs([]slog.Attr{slog.String("only_b", "b")})

	slog.New(childA).Info("msg")
	_, hasB := nestedValue(captured, "g", "only_b")
	require.False(t, hasB, "child A must not see child B's attrs")
	vA, hasA := nestedValue(captured, "g", "only_a")
	require.True(t, hasA)
	require.Equal(t, "a", vA.String())

	slog.New(childB).Info("msg")
	_, hasA = nestedValue(captured, "g", "only_a")
	require.False(t, hasA, "child B must not see child A's attrs")
}

func TestGlobalFieldsHandler_WithGroup_MultipleGroups(t *testing.T) {
	var captured slog.Record
	h := handler.NewGlobalFieldsHandler(captureRecord(&captured), map[string]string{
		"env": "development",
	}, false)

	grouped := h.WithGroup("user").WithGroup("session").WithGroup("request")
	slog.New(grouped).Info("request processed", "method", "POST")

	requireSingleAttr(t, captured, "env", "development")
	v, ok := nestedValue(captured, "user", "session", "request", "method")
	require.True(t, ok, "record attr must be nested under every open group")
	require.Equal(t, "POST", v.String())
	require.Equal(t, "request processed", captured.Message)
}

func TestGlobalFieldsHandler_WithGroup_PreservesOverride(t *testing.T) {
	// Override applies to top-level record attrs. Inside a group the record's
	// env is a different path, so both values legitimately coexist.
	var captured slog.Record
	h := handler.NewGlobalFieldsHandler(captureRecord(&captured), map[string]string{
		"env": "staging",
	}, true)

	slog.New(h.WithGroup("api")).Info("api call", "env", "production")

	requireSingleAttr(t, captured, "env", "staging")
	v, ok := nestedValue(captured, "api", "env")
	require.True(t, ok)
	require.Equal(t, "production", v.String())
	requireUniqueKeys(t, captured)
}

func TestGlobalFieldsHandler_WithGroup_WithAttrs_Integration(t *testing.T) {
	var captured slog.Record
	h := handler.NewGlobalFieldsHandler(captureRecord(&captured), map[string]string{
		"service": "payment",
		"version": "2.0",
	}, false)

	derived := h.WithAttrs([]slog.Attr{slog.String("component", "processor")}).WithGroup("transaction")
	slog.New(derived).Info("payment processed", "amount", "100.00", "currency", "USD")

	requireSingleAttr(t, captured, "component", "processor")
	requireSingleAttr(t, captured, "service", "payment")
	requireSingleAttr(t, captured, "version", "2.0")
	amount, ok := nestedValue(captured, "transaction", "amount")
	require.True(t, ok)
	require.Equal(t, "100.00", amount.String())
	currency, ok := nestedValue(captured, "transaction", "currency")
	require.True(t, ok)
	require.Equal(t, "USD", currency.String())
	require.Equal(t, "payment processed", captured.Message)
}

func TestGlobalFieldsHandler_WithGroup_ContextLoggingAndLevels(t *testing.T) {
	var captured slog.Record
	h := handler.NewGlobalFieldsHandler(captureRecord(&captured), map[string]string{
		"component": "database",
	}, false)
	logger := slog.New(h.WithGroup("query"))

	logger.InfoContext(context.Background(), "email sent", "recipient", "user@example.com")
	requireSingleAttr(t, captured, "component", "database")
	v, ok := nestedValue(captured, "query", "recipient")
	require.True(t, ok)
	require.Equal(t, "user@example.com", v.String())

	logger.Error("query failed", "error", "connection timeout")
	require.Equal(t, slog.LevelError, captured.Level)
	requireSingleAttr(t, captured, "component", "database")
	v, ok = nestedValue(captured, "query", "error")
	require.True(t, ok)
	require.Equal(t, "connection timeout", v.String())
}

// --- construction and record metadata -----------------------------------

func TestGlobalFieldsHandler_EmptyKeyRejectedAtConstruction(t *testing.T) {
	var captured slog.Record
	h := handler.NewGlobalFieldsHandler(captureRecord(&captured), map[string]string{
		"":    "dropped",
		"env": "staging",
	}, true)

	slog.New(h).Info("msg")

	require.Equal(t, []string{"env"}, topKeys(captured))
	require.Empty(t, attrsWithKey(captured, ""))
}

func TestGlobalFieldsHandler_NoValidFields_PassesRecordThrough(t *testing.T) {
	var captured slog.Record
	h := handler.NewGlobalFieldsHandler(captureRecord(&captured), map[string]string{"": "x"}, true)

	slog.New(h).Info("msg", "k", "v")

	require.Equal(t, []string{"k"}, topKeys(captured))
}

func TestGlobalFieldsHandler_PreservesRecordMetadata(t *testing.T) {
	var pcs [1]uintptr
	runtime.Callers(1, pcs[:])
	pc := pcs[0]
	at := time.Date(2026, 9, 12, 10, 30, 0, 0, time.UTC)

	cases := []struct {
		name  string
		build func(slog.Handler) slog.Handler
		attrs []slog.Attr
	}{
		{
			name: "no collision (clone path)",
			build: func(b slog.Handler) slog.Handler {
				return handler.NewGlobalFieldsHandler(b, map[string]string{"env": "staging"}, true)
			},
			attrs: []slog.Attr{slog.String("k", "v")},
		},
		{
			name: "collision (rebuild path)",
			build: func(b slog.Handler) slog.Handler {
				return handler.NewGlobalFieldsHandler(b, map[string]string{"env": "staging"}, true)
			},
			attrs: []slog.Attr{slog.String("env", "production")},
		},
		{
			name: "grouped path",
			build: func(b slog.Handler) slog.Handler {
				return handler.NewGlobalFieldsHandler(b, map[string]string{"env": "staging"}, true).WithGroup("g")
			},
			attrs: []slog.Attr{slog.String("k", "v")},
		},
		{
			name:  "no fields (pass-through)",
			build: func(b slog.Handler) slog.Handler { return handler.NewGlobalFieldsHandler(b, nil, true) },
			attrs: []slog.Attr{slog.String("k", "v")},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var captured slog.Record
			h := tc.build(captureRecord(&captured))

			record := slog.NewRecord(at, slog.LevelWarn, "exact message", pc)
			record.AddAttrs(tc.attrs...)
			require.NoError(t, h.Handle(context.Background(), record))

			require.True(t, at.Equal(captured.Time), "Time must be preserved")
			require.Equal(t, slog.LevelWarn, captured.Level)
			require.Equal(t, "exact message", captured.Message)
			require.Equal(t, pc, captured.PC, "PC must survive for downstream attribution")
		})
	}
}

func TestGlobalFieldsHandler_Enabled_DelegatesToNext(t *testing.T) {
	var buf bytes.Buffer
	base := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})
	h := handler.NewGlobalFieldsHandler(base, map[string]string{"env": "staging"}, true)

	require.False(t, h.Enabled(context.Background(), slog.LevelInfo))
	require.True(t, h.Enabled(context.Background(), slog.LevelError))
}
