package utils

import (
	"log/slog"
)

// ExtractAttrs extracts the attributes from a slog.Record as a map[string]interface{}.
// It iterates over the attributes of the given slog.Record and populates a map with the attribute keys and their corresponding values.
//
// Because the result is a map, a record that carries the same key more than
// once collapses to a single entry holding the last value. Use ExtractAttrList
// when the assertion is about how many attrs a key has, or about their order.
//
// Parameters:
// - r (slog.Record): The log record from which to extract attributes.
//
// Returns:
// - map[string]interface{}: A map containing the attribute keys and their corresponding values.
func ExtractAttrs(r slog.Record) map[string]interface{} {
	m := make(map[string]interface{})
	r.Attrs(func(a slog.Attr) bool {
		m[a.Key] = a.Value.Any()
		return true
	})
	return m
}

// ExtractAttrList returns the top-level attributes of r in the order they
// were added, without collapsing duplicate keys. It is the right helper for
// asserting that a record carries a key exactly once, or that attrs are in a
// given order; ExtractAttrs cannot see either.
//
// Parameters:
// - r (slog.Record): The log record from which to extract attributes.
//
// Returns:
// - []slog.Attr: The record's top-level attributes, in order.
func ExtractAttrList(r slog.Record) []slog.Attr {
	attrs := make([]slog.Attr, 0, r.NumAttrs())
	r.Attrs(func(a slog.Attr) bool {
		attrs = append(attrs, a)
		return true
	})
	return attrs
}
