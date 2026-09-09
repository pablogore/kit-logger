package kitlogtest

import (
	"context"
	"log/slog"
)

// groupOrAttrs is one link in the chain of WithGroup/WithAttrs calls that
// produced a TestHandler. Recording the sequence, rather than flattening it
// eagerly, is what lets Handle rebuild the correct nesting: attrs added
// before a WithGroup belong outside it, attrs added after belong inside it.
type groupOrAttrs struct {
	group string      // group name; empty when this link holds attrs instead
	attrs []slog.Attr // attrs added via WithAttrs; empty when this link is a group
}

// TestHandler is a testing slog.Handler that captures log records via a
// callback, honoring WithAttrs and WithGroup the way a real handler does.
type TestHandler struct {
	Callback func(context.Context, slog.Record) // Callback function to handle log records.

	goas []groupOrAttrs
}

// NewTestHandler creates a new TestHandler with the specified callback function.
func NewTestHandler(callback func(context.Context, slog.Record)) slog.Handler {
	return &TestHandler{Callback: callback}
}

// Enabled always returns true, indicating that logging is enabled.
func (h *TestHandler) Enabled(_ context.Context, _ slog.Level) bool {
	return true
}

// Handle rebuilds the record with every attribute added through WithAttrs and
// WithGroup nested exactly as a real handler (e.g. slog.JSONHandler) would
// nest them, then invokes the callback.
func (h *TestHandler) Handle(ctx context.Context, r slog.Record) error {
	var attrs []slog.Attr
	r.Attrs(func(a slog.Attr) bool {
		attrs = append(attrs, a)
		return true
	})

	for i := len(h.goas) - 1; i >= 0; i-- {
		goa := h.goas[i]
		if goa.group == "" {
			attrs = append(append([]slog.Attr(nil), goa.attrs...), attrs...)
			continue
		}
		if len(attrs) == 0 {
			// A real handler drops an empty group rather than emitting it.
			continue
		}
		attrs = []slog.Attr{slog.Group(goa.group, attrsToAny(attrs)...)}
	}

	out := slog.NewRecord(r.Time, r.Level, r.Message, r.PC)
	out.AddAttrs(attrs...)
	h.Callback(ctx, out)
	return nil
}

// WithAttrs returns a new TestHandler with additional attributes, preserving
// previous attributes and groups.
func (h *TestHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return h
	}
	return h.withGroupOrAttrs(groupOrAttrs{attrs: append([]slog.Attr(nil), attrs...)})
}

// WithGroup returns a new TestHandler that nests every attribute added from
// this point on -- including the record's own attrs -- inside a group with
// the given name.
func (h *TestHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	return h.withGroupOrAttrs(groupOrAttrs{group: name})
}

func (h *TestHandler) withGroupOrAttrs(goa groupOrAttrs) *TestHandler {
	h2 := *h
	h2.goas = make([]groupOrAttrs, len(h.goas)+1)
	copy(h2.goas, h.goas)
	h2.goas[len(h.goas)] = goa
	return &h2
}

func attrsToAny(attrs []slog.Attr) []any {
	out := make([]any, len(attrs))
	for i, a := range attrs {
		out[i] = a
	}
	return out
}
