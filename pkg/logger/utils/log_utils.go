package utils

import (
	"log/slog"
)

// ExtractAttrs extracts the attributes from a slog.Record as a map[string]interface{}.
// It iterates over the attributes of the given slog.Record and populates a map with the attribute keys and their corresponding values.
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
