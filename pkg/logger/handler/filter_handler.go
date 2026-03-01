package handler

import (
	"context"
	"log/slog"
	"strings"
)

// FilterRule defines a rule for filtering log entries.
type FilterRule struct {
	Key   string // Key to filter
	Value string // Optional value to compare
}

// FilterHandler is a handler that discards logs based on rules.
type FilterHandler struct {
	next  slog.Handler // The next handler in the chain.
	rules []FilterRule // List of filter rules.
}

// NewFilterHandler creates a handler that discards logs based on rules.
func NewFilterHandler(next slog.Handler, rules []FilterRule) *FilterHandler {
	return &FilterHandler{
		next:  next,
		rules: rules,
	}
}

// Enabled checks if the logging level is enabled.
func (h *FilterHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

// Handle processes the log record and discards it if it matches any filter rule.
func (h *FilterHandler) Handle(ctx context.Context, record slog.Record) error {
	skip := false

	record.Attrs(func(attr slog.Attr) bool {
		for _, rule := range h.rules {
			if strings.EqualFold(attr.Key, rule.Key) {
				if rule.Value == "" || attr.Value.String() == rule.Value {
					skip = true
					return false // early exit
				}
			}
		}
		return true
	})

	if skip {
		return nil
	}
	return h.next.Handle(ctx, record)
}

// WithAttrs returns a new FilterHandler with additional attributes.
func (h *FilterHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return NewFilterHandler(h.next.WithAttrs(attrs), h.rules)
}

// WithGroup returns a new FilterHandler with a group name.
func (h *FilterHandler) WithGroup(name string) slog.Handler {
	return NewFilterHandler(h.next.WithGroup(name), h.rules)
}
