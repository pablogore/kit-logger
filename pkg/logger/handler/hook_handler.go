package handler

import (
	"context"
	"log/slog"
)

// HookFunc is a function that executes before passing the log record to the next handler.
// It can modify the context or inspect the record. If it returns false, the record is not passed to the next handler.
type HookFunc func(ctx context.Context, record slog.Record) (context.Context, bool)

// HookHandler executes a Hook function before delegating the log record.
type HookHandler struct {
	next slog.Handler // The next handler in the chain.
	hook HookFunc     // The hook function to execute.
}

// NewHookHandler creates a new HookHandler with the specified next handler and hook function.
func NewHookHandler(next slog.Handler, hook HookFunc) *HookHandler {
	return &HookHandler{
		next: next,
		hook: hook,
	}
}

// Enabled checks if the logging level is enabled.
func (h *HookHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

// Handle executes the hook function and, if it returns true, passes the log record to the next handler.
func (h *HookHandler) Handle(ctx context.Context, record slog.Record) error {
	ctx, ok := h.hook(ctx, record)
	if !ok {
		return nil // Stops the chain
	}
	return h.next.Handle(ctx, record)
}

// WithAttrs returns a new HookHandler with additional attributes.
func (h *HookHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &HookHandler{
		next: h.next.WithAttrs(attrs),
		hook: h.hook,
	}
}

// WithGroup returns a new HookHandler with a group name.
func (h *HookHandler) WithGroup(name string) slog.Handler {
	return &HookHandler{
		next: h.next.WithGroup(name),
		hook: h.hook,
	}
}
