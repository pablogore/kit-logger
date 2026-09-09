package handler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
)

// MultiHandler is a handler that fans each record out to multiple handlers.
type MultiHandler struct {
	handlers []slog.Handler // List of handlers to distribute logs to.
}

// NewMultiHandler creates a new MultiHandler that distributes logs to the provided handlers.
func NewMultiHandler(handlers ...slog.Handler) *MultiHandler {
	return &MultiHandler{handlers: handlers}
}

// Enabled reports whether any child enables level -- the OR across children
// that slog.Logger consults before it even builds a record. This is a gate on
// "is it worth building the record at all", not permission to deliver it to
// every child regardless of that child's own policy; Handle enforces the
// per-child decision separately.
func (h *MultiHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, child := range h.handlers {
		if child.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

// Handle delivers record to every child whose own Enabled(ctx, record.Level)
// agrees, in construction order, and joins every child's error instead of
// keeping only the last. A child is handed its own Clone of record: slog
// records share their attr backing array, so without cloning, one child
// calling Record.AddAttrs (as ComponentHandler and TestHandler both do) would
// leak attrs into what a sibling child observes.
func (h *MultiHandler) Handle(ctx context.Context, record slog.Record) error {
	var errs []error
	for i, child := range h.handlers {
		if !child.Enabled(ctx, record.Level) {
			continue
		}
		if err := child.Handle(ctx, record.Clone()); err != nil {
			errs = append(errs, fmt.Errorf("multihandler: child %d: %w", i, err))
		}
	}
	return errors.Join(errs...)
}

// WithAttrs returns a new MultiHandler with attrs added to every child, in
// the same order and count as the receiver.
func (h *MultiHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	newHandlers := make([]slog.Handler, 0, len(h.handlers))
	for _, child := range h.handlers {
		newHandlers = append(newHandlers, child.WithAttrs(attrs))
	}
	return NewMultiHandler(newHandlers...)
}

// WithGroup returns a new MultiHandler with name opened as a group on every
// child, in the same order and count as the receiver.
func (h *MultiHandler) WithGroup(name string) slog.Handler {
	newHandlers := make([]slog.Handler, 0, len(h.handlers))
	for _, child := range h.handlers {
		newHandlers = append(newHandlers, child.WithGroup(name))
	}
	return NewMultiHandler(newHandlers...)
}

// UnwrapAll returns every handler this one fans out to. A MultiHandler wraps
// more than one handler, so it cannot answer Unwrap with a single handler
// without hiding the rest from lifecycle traversal.
func (h *MultiHandler) UnwrapAll() []slog.Handler {
	return append([]slog.Handler(nil), h.handlers...)
}
