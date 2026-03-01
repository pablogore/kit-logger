package handler

import (
	"context"
	"log/slog"
)

// MultiHandler is a handler that distributes each log to multiple handlers.
type MultiHandler struct {
	handlers []slog.Handler // List of handlers to distribute logs to.
}

// NewMultiHandler creates a new MultiHandler that distributes logs to the provided handlers.
func NewMultiHandler(handlers ...slog.Handler) *MultiHandler {
	return &MultiHandler{handlers: handlers}
}

// Enabled checks if any of the handlers enable the logging level.
func (h *MultiHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, handler := range h.handlers {
		if handler.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

// Handle distributes the log record to all handlers and returns the last error encountered, if any.
func (h *MultiHandler) Handle(ctx context.Context, record slog.Record) error {
	var lastErr error
	for _, handler := range h.handlers {
		if err := handler.Handle(ctx, record); err != nil {
			lastErr = err
		}
	}
	return lastErr
}

// WithAttrs returns a new MultiHandler with additional attributes added to each handler.
func (h *MultiHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	var newHandlers []slog.Handler
	for _, handler := range h.handlers {
		newHandlers = append(newHandlers, handler.WithAttrs(attrs))
	}
	return &MultiHandler{handlers: newHandlers}
}

// WithGroup returns a new MultiHandler with a group name added to each handler.
func (h *MultiHandler) WithGroup(name string) slog.Handler {
	newHandlers := make([]slog.Handler, 0, len(h.handlers))
	for _, handler := range h.handlers {
		newHandlers = append(newHandlers, handler.WithGroup(name))
	}
	return NewMultiHandler(newHandlers...)
}
