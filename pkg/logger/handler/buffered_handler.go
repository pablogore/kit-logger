package handler

import (
	"context"
	"errors"
	"log/slog"
)

// BufferedHandler enqueues log records and processes them asynchronously.
// If the buffer is full, Handle returns an error.
type BufferedHandler struct {
	next slog.Handler     // The next handler in the chain.
	ch   chan slog.Record // Channel to buffer log records.
}

// NewBufferedHandler creates a handler that enqueues up to `size` messages.
func NewBufferedHandler(next slog.Handler, size int) *BufferedHandler {
	h := &BufferedHandler{
		next: next,
		ch:   make(chan slog.Record, size),
	}

	go h.run()
	return h
}

// Enabled checks if the logging level is enabled.
func (h *BufferedHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

// Handle attempts to enqueue the log record without blocking; returns an error if the buffer is full.
func (h *BufferedHandler) Handle(_ context.Context, record slog.Record) error {
	select {
	case h.ch <- record:
		return nil
	default:
		return errors.New("buffer full")
	}
}

// WithAttrs returns a new handler with additional attributes.
func (h *BufferedHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return NewBufferedHandler(h.next.WithAttrs(attrs), cap(h.ch))
}

// WithGroup returns a new handler with a group name.
func (h *BufferedHandler) WithGroup(name string) slog.Handler {
	return NewBufferedHandler(h.next.WithGroup(name), cap(h.ch))
}

// run processes log records from the buffer.
func (h *BufferedHandler) run() {
	for record := range h.ch {
		_ = h.next.Handle(context.Background(), record)
	}
}
