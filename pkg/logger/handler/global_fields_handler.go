package handler

import (
	"context"
	"log/slog"
)

// GlobalFieldsHandler adds global fields to all log records.
// If override is true, the global fields overwrite existing ones with the same key.
type GlobalFieldsHandler struct {
	next     slog.Handler // The next handler in the chain.
	fields   []slog.Attr  // Global fields to add to log records.
	override bool         // Whether to override existing fields with the same key.
}

// NewGlobalFieldsHandler creates a new handler that adds global fields.
// `fields` is a map of key-value pairs, and `override` defines whether to overwrite duplicate keys.
func NewGlobalFieldsHandler(next slog.Handler, fields map[string]string, override bool) slog.Handler {
	attrs := make([]slog.Attr, 0, len(fields))
	for k, v := range fields {
		attrs = append(attrs, slog.String(k, v))
	}
	return &GlobalFieldsHandler{
		next:     next,
		fields:   attrs,
		override: override,
	}
}

// Enabled checks if the logging level is enabled.
func (h *GlobalFieldsHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

// Handle processes the log record, adding global fields and passing it to the next handler.
func (h *GlobalFieldsHandler) Handle(ctx context.Context, record slog.Record) error {
	overridden := map[string]struct{}{}
	if h.override {
		for _, a := range h.fields {
			overridden[a.Key] = struct{}{}
		}
	}

	// Rebuild the record instead of cloning it, dropping any attr whose key a
	// global field will override. Cloning and then AddAttrs-ing the global
	// field on top would leave both the original and the override attr in
	// the record, emitting the same key twice in the encoded output.
	clone := slog.NewRecord(record.Time, record.Level, record.Message, record.PC)
	record.Attrs(func(a slog.Attr) bool {
		if _, drop := overridden[a.Key]; drop {
			return true
		}
		clone.AddAttrs(a)
		return true
	})

	existing := map[string]struct{}{}
	clone.Attrs(func(a slog.Attr) bool {
		existing[a.Key] = struct{}{}
		return true
	})

	// Add global fields, avoiding overwriting if override == false
	for _, a := range h.fields {
		if _, exists := existing[a.Key]; exists && !h.override {
			continue
		}
		clone.AddAttrs(a)
	}

	return h.next.Handle(ctx, clone)
}

// filterValidAttrs filters out attributes with empty keys.
func filterValidAttrs(attrs []slog.Attr) []slog.Attr {
	var out []slog.Attr
	for _, a := range attrs {
		if a.Key != "" {
			out = append(out, a)
		}
	}
	return out
}

// WithAttrs returns a new GlobalFieldsHandler with additional attributes.
func (h *GlobalFieldsHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	combined := append([]slog.Attr{}, h.fields...)
	combined = append(combined, filterValidAttrs(attrs)...)

	return &GlobalFieldsHandler{
		next:     h.next,
		fields:   combined,
		override: h.override,
	}
}

// WithGroup returns a new GlobalFieldsHandler with a group name.
func (h *GlobalFieldsHandler) WithGroup(name string) slog.Handler {
	return &GlobalFieldsHandler{
		next:     h.next.WithGroup(name),
		fields:   h.fields,
		override: h.override,
	}
}

// Unwrap returns the handler this one decorates. It lets a logger lifecycle
// (Flush, Shutdown) traverse a chain that was assembled by hand, instead of
// stopping at the outermost handler.
func (h *GlobalFieldsHandler) Unwrap() slog.Handler { return h.next }
