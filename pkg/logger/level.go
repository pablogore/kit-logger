package logger

import (
	"context"
	"log/slog"
)

// levelHandler gates a handler chain on a shared *slog.LevelVar.
//
// The default pipeline gets this for free from slog.HandlerOptions.Level on
// the Text/JSON handler it builds. A caller-supplied Sink knows nothing about
// levelVar, so without this wrapper SetLevel would set a variable nothing
// reads -- exactly the bug this type exists to close. Placed directly around
// Sink, in the same position the default path's HandlerOptions occupies, it
// makes every decorator above it (Filter, GlobalFields, Component, ...)
// inherit the gate through their existing Enabled delegation.
type levelHandler struct {
	next  slog.Handler
	level *slog.LevelVar
}

// Enabled reports whether level is at or above the shared LevelVar's current
// value, and defers to next for anything else it wants to gate on. The AND is
// deliberate: LevelVar is this wrapper's whole reason to exist, but next may
// be a real handler with its own Enabled logic (a test double, a sampler-fed
// sink, anything) that must still be honored rather than silently overridden.
func (h *levelHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return level >= h.level.Level() && h.next.Enabled(ctx, level)
}

// Handle re-checks the level before forwarding. slog.Logger always calls
// Enabled first, so this is redundant on that path -- but slog.Handler is a
// public interface, and nothing stops a caller from invoking Handle directly
// against a chain built from Config.Sink. Gating here too means the LevelVar
// is honored regardless of how this handler is reached.
func (h *levelHandler) Handle(ctx context.Context, record slog.Record) error {
	if record.Level < h.level.Level() {
		return nil
	}
	return h.next.Handle(ctx, record)
}

// WithAttrs returns a handler that adds attrs to every record.
func (h *levelHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &levelHandler{next: h.next.WithAttrs(attrs), level: h.level}
}

// WithGroup returns a handler that nests subsequent attrs under name.
func (h *levelHandler) WithGroup(name string) slog.Handler {
	return &levelHandler{next: h.next.WithGroup(name), level: h.level}
}

// Unwrap returns the handler this one decorates. It lets a logger lifecycle
// (Flush, Shutdown) traverse a chain that was assembled by hand, instead of
// stopping at the outermost handler.
func (h *levelHandler) Unwrap() slog.Handler { return h.next }
