package handler

import (
	"context"
	"log/slog"
	"slices"
	"strings"
)

// GlobalFieldsHandler attaches a fixed set of fields to every record.
//
// Key semantics:
//
//   - With override=true, a global field replaces the record attribute that
//     shares its key, in the position that attribute occupied. With
//     override=false, the record attribute wins and the global field is not
//     added. Either way the record forwarded to next carries each global key
//     at most once; this handler never produces a duplicate key.
//   - Global fields are emitted in sorted key order, so output is
//     deterministic across processes built from the same config.
//   - Global fields override record attributes only. Attributes attached
//     through With/WithAttrs before any WithGroup are delegated to next --
//     keeping slog's pre-formatting optimization -- and are therefore not
//     visible to the override check. A With-supplied attribute that collides
//     with a global key is left as the wrapped handler formats it.
//   - Global fields always stay at the top level of the record. Record
//     attributes, and attributes added via WithAttrs after a WithGroup, are
//     nested under the open groups, matching slog's own nesting rules.
//
// Placing global fields outside a group while the record's own attributes go
// inside it is impossible once next.WithGroup has been called, so WithGroup,
// and every WithAttrs after it, is held and nested into the record at Handle
// time instead of being forwarded to next (the same approach FilterHandler
// takes for rule matching).
type GlobalFieldsHandler struct {
	next     slog.Handler // The next handler in the chain.
	fields   []slog.Attr  // Global fields, sorted by key, no empty keys.
	override bool         // Whether global fields replace record attrs with the same key.

	// ops holds WithGroup calls, and WithAttrs calls made after the first
	// WithGroup, in the order they were made. It is nil until a group is
	// opened; before that WithAttrs delegates to next.
	ops []globalFieldsOp
}

// globalFieldsOp is one held WithAttrs or WithGroup call, replayed onto the
// record's attributes at Handle time to rebuild slog's nesting.
type globalFieldsOp struct {
	attrs   []slog.Attr // set when isGroup is false
	group   string      // set when isGroup is true
	isGroup bool
}

// NewGlobalFieldsHandler creates a new handler that adds global fields.
// `fields` is a map of key-value pairs, and `override` defines whether a
// global field replaces a record attribute with the same key. Fields with an
// empty key are rejected. Fields are attached in sorted key order.
func NewGlobalFieldsHandler(next slog.Handler, fields map[string]string, override bool) slog.Handler {
	attrs := make([]slog.Attr, 0, len(fields))
	for k, v := range fields {
		attrs = append(attrs, slog.String(k, v))
	}
	attrs = filterValidAttrs(attrs)
	slices.SortFunc(attrs, func(a, b slog.Attr) int { return strings.Compare(a.Key, b.Key) })
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

// Handle attaches the global fields and forwards the record to next.
//
// record.Time, Level, Message and PC are carried over unchanged on every
// path, so downstream attribution (ComponentHandler reads PC) survives.
func (h *GlobalFieldsHandler) Handle(ctx context.Context, record slog.Record) error {
	if len(h.fields) == 0 {
		return h.next.Handle(ctx, record)
	}
	if len(h.ops) > 0 {
		return h.handleGrouped(ctx, record)
	}

	// Fast path: no record attr shares a key with a global field, so there
	// is nothing to replace or skip and the fields can simply be appended.
	collides := false
	record.Attrs(func(a slog.Attr) bool {
		if _, ok := h.lookup(a.Key); ok {
			collides = true
			return false
		}
		return true
	})
	if !collides {
		clone := record.Clone()
		clone.AddAttrs(h.fields...)
		return h.next.Handle(ctx, clone)
	}

	out := slog.NewRecord(record.Time, record.Level, record.Message, record.PC)
	out.AddAttrs(h.merge(recordAttrs(record))...)
	return h.next.Handle(ctx, out)
}

// handleGrouped is the Handle path for a handler derived via WithGroup. The
// global fields go at the top level; the record's own attrs, and any attrs
// held from a WithAttrs made after the group was opened, are nested under
// the held groups. Keys inside a group live on a different path from the
// top-level global fields, so no override check applies here.
func (h *GlobalFieldsHandler) handleGrouped(ctx context.Context, record slog.Record) error {
	attrs := recordAttrs(record)
	for i := len(h.ops) - 1; i >= 0; i-- {
		op := h.ops[i]
		if !op.isGroup {
			attrs = append(slices.Clone(op.attrs), attrs...)
			continue
		}
		if len(attrs) == 0 {
			// A real handler elides an empty group rather than emitting it.
			continue
		}
		attrs = []slog.Attr{{Key: op.group, Value: slog.GroupValue(attrs...)}}
	}

	out := slog.NewRecord(record.Time, record.Level, record.Message, record.PC)
	out.AddAttrs(h.fields...)
	out.AddAttrs(attrs...)
	return h.next.Handle(ctx, out)
}

// merge returns attrs with the global fields applied: a colliding attr is
// replaced in place (override) or kept as is (no override), and every global
// field whose key the record does not carry is appended in sorted order. The
// result holds each global key at most once even when attrs itself repeats
// a key.
func (h *GlobalFieldsHandler) merge(attrs []slog.Attr) []slog.Attr {
	out := make([]slog.Attr, 0, len(attrs)+len(h.fields))
	for _, a := range attrs {
		i, isGlobal := h.lookup(a.Key)
		if !isGlobal || !h.override {
			out = append(out, a)
			continue
		}
		if hasKey(out, a.Key) {
			continue // the first occurrence was already replaced; drop repeats
		}
		out = append(out, h.fields[i])
	}
	for _, g := range h.fields {
		if !hasKey(attrs, g.Key) {
			out = append(out, g)
		}
	}
	return out
}

// lookup returns the index of the global field with the given key.
func (h *GlobalFieldsHandler) lookup(key string) (int, bool) {
	return slices.BinarySearchFunc(h.fields, key, func(a slog.Attr, k string) int {
		return strings.Compare(a.Key, k)
	})
}

// hasKey reports whether attrs contains a top-level attr with the given key.
func hasKey(attrs []slog.Attr, key string) bool {
	for _, a := range attrs {
		if a.Key == key {
			return true
		}
	}
	return false
}

// recordAttrs returns the record's top-level attrs in order.
func recordAttrs(record slog.Record) []slog.Attr {
	attrs := make([]slog.Attr, 0, record.NumAttrs())
	record.Attrs(func(a slog.Attr) bool {
		attrs = append(attrs, a)
		return true
	})
	return attrs
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

// WithAttrs returns a handler that adds attrs to every record.
//
// Before any WithGroup the attrs are delegated to next, so the wrapped
// handler can pre-format them once; they are never folded into the global
// field set. After a WithGroup they are held instead, so they nest under the
// group at Handle time (forwarding them to next would place them outside
// it). slices.Clip on the prior ops guarantees siblings derived from the same
// parent never share a backing array.
func (h *GlobalFieldsHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	attrs = filterValidAttrs(attrs)
	if len(attrs) == 0 {
		return h
	}
	if len(h.ops) == 0 {
		return &GlobalFieldsHandler{
			next:     h.next.WithAttrs(attrs),
			fields:   h.fields,
			override: h.override,
		}
	}
	return &GlobalFieldsHandler{
		next:     h.next,
		fields:   h.fields,
		override: h.override,
		ops:      append(slices.Clip(h.ops), globalFieldsOp{attrs: slices.Clone(attrs)}),
	}
}

// WithGroup returns a handler that nests subsequent record attrs, and attrs
// from later WithAttrs calls, under name. Global fields stay at the top
// level. An empty name returns the receiver, as slog's own handlers do.
//
// With no global fields configured there is nothing to keep outside the
// group, so the call is forwarded to next directly.
func (h *GlobalFieldsHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	if len(h.fields) == 0 {
		return &GlobalFieldsHandler{
			next:     h.next.WithGroup(name),
			fields:   h.fields,
			override: h.override,
		}
	}
	return &GlobalFieldsHandler{
		next:     h.next,
		fields:   h.fields,
		override: h.override,
		ops:      append(slices.Clip(h.ops), globalFieldsOp{isGroup: true, group: name}),
	}
}

// Unwrap returns the handler this one decorates. It lets a logger lifecycle
// (Flush, Shutdown) traverse a chain that was assembled by hand, instead of
// stopping at the outermost handler.
func (h *GlobalFieldsHandler) Unwrap() slog.Handler { return h.next }
