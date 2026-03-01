package handler

import (
	"context"
	"log/slog"
)

// TestHandler is a testing slog.Handler to capture log records.
type TestHandler struct {
	Callback func(context.Context, slog.Record) // Callback function to handle log records.
	Attrs    []slog.Attr                        // Attributes to be added to log records.
}

// NewTestHandler creates a new TestHandler with the specified callback function.
func NewTestHandler(callback func(context.Context, slog.Record)) slog.Handler {
	return &TestHandler{
		Callback: callback,
	}
}

// Enabled always returns true, indicating that logging is enabled.
func (h *TestHandler) Enabled(_ context.Context, _ slog.Level) bool {
	return true
}

// Handle processes the log record, injecting any attributes passed through WithAttrs and invoking the callback.
func (h *TestHandler) Handle(ctx context.Context, r slog.Record) error {
	// Inject attributes passed through WithAttrs
	if len(h.Attrs) > 0 {
		r.AddAttrs(h.Attrs...)
	}
	h.Callback(ctx, r)
	return nil
}

// WithAttrs returns a new TestHandler with additional attributes, preserving previous attributes.
func (h *TestHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &TestHandler{
		Callback: h.Callback,
		Attrs:    append(h.Attrs, attrs...), // Preserve previous attributes
	}
}

// WithGroup returns the same TestHandler without implementing grouping for simplicity in testing.
func (h *TestHandler) WithGroup(_ string) slog.Handler {
	// For simplicity in testing, grouping is not implemented.
	return h
}
