package handler

import (
	"context"
	"log/slog"
	"slices"
	"strings"
)

// defaultRedactionReplacement is used in ModeRedact when a FilterRule sets no
// Replacement of its own.
const defaultRedactionReplacement = "[REDACTED]"

// Mode selects what FilterHandler does with a record whose attributes match a
// rule.
type Mode uint8

const (
	// ModeDrop discards the whole record. This is the historical behavior of
	// NewFilterHandler and remains its default: dropping is the wrong choice
	// for an important line that merely mentions a secret (see ModeRedact),
	// but it is a safe, explicit "don't emit this" for pure noise-suppression
	// rules.
	ModeDrop Mode = iota

	// ModeRedact keeps the record but replaces the value of every matching
	// attribute with FilterRule.Replacement (or defaultRedactionReplacement
	// if unset). Every other attribute, and the record's Time, Level,
	// Message and PC, are left untouched.
	ModeRedact
)

// FilterRule defines a rule for filtering log entries.
//
// Key matching is case-insensitive and matches either the attribute's bare
// key (at any nesting depth) or its full dotted path from the enclosing
// groups, e.g. a Key of "password" matches both a top-level "password" attr
// and a "password" nested inside slog.Group("credential", ...); a Key of
// "credential.password" matches only the latter.
//
// Value matching is exact and case-sensitive. An empty Value matches any
// value for the key, i.e. it drops/redacts on key presence alone.
type FilterRule struct {
	Key   string // Key or dotted group path to match, case-insensitive.
	Value string // Optional value to compare, case-sensitive; empty matches any value.

	// Replacement is substituted for the value in ModeRedact. Defaults to
	// "[REDACTED]" when empty. Ignored in ModeDrop.
	Replacement string
}

// filterOp records a WithAttrs or WithGroup call so it can be replayed onto
// next at Handle time, once the attrs it carries have been checked (and, in
// ModeRedact, rewritten) against the rules. Deferring the call is what makes
// filtering apply to With-supplied attributes at all: forwarding them to next
// immediately -- as a plain decorator would -- bakes their values into next's
// pre-formatted state before this handler ever sees them again.
type filterOp struct {
	attrs   []slog.Attr // set when isGroup is false
	group   string      // set when isGroup is true
	isGroup bool
}

// FilterHandler is a handler that discards or redacts logs based on rules.
type FilterHandler struct {
	next  slog.Handler // The next handler in the chain.
	rules []FilterRule // List of filter rules.
	mode  Mode

	// ops holds WithAttrs/WithGroup calls in the order they were made, for a
	// handler with rules configured. It is left nil (and next.WithAttrs /
	// next.WithGroup are called immediately, exactly as before this fix) when
	// there are no rules, since a handler with nothing to match can safely
	// keep slog's pre-formatting optimization.
	ops []filterOp
}

// NewFilterHandler creates a handler that discards logs based on rules. It
// uses ModeDrop, matching this constructor's historical behavior. Use
// NewFilterHandlerWithMode for ModeRedact.
func NewFilterHandler(next slog.Handler, rules []FilterRule) *FilterHandler {
	return NewFilterHandlerWithMode(next, rules, ModeDrop)
}

// NewRedactingFilterHandler creates a filtering handler in ModeRedact: a
// matching record is kept with the matching values replaced, rather than
// dropped outright. This is the recommended constructor for anything
// resembling a security control, since ModeDrop can silently delete an
// important log line (e.g. an error) for mentioning a secret.
func NewRedactingFilterHandler(next slog.Handler, rules []FilterRule) *FilterHandler {
	return NewFilterHandlerWithMode(next, rules, ModeRedact)
}

// NewFilterHandlerWithMode creates a handler that filters logs based on
// rules, with an explicit Mode.
func NewFilterHandlerWithMode(next slog.Handler, rules []FilterRule, mode Mode) *FilterHandler {
	return &FilterHandler{
		next:  next,
		rules: rules,
		mode:  mode,
	}
}

// Enabled checks if the logging level is enabled.
func (h *FilterHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

// Handle processes the log record, applying rules to the record's own attrs
// and to every attr held from a prior WithAttrs/WithGroup call.
func (h *FilterHandler) Handle(ctx context.Context, record slog.Record) error {
	if len(h.rules) == 0 {
		return h.next.Handle(ctx, record)
	}

	next := h.next
	path := make([]string, 0, len(h.ops))
	for _, op := range h.ops {
		if op.isGroup {
			next = next.WithGroup(op.group)
			path = append(path, op.group)
			continue
		}

		attrs, hit := h.filterAttrs(path, op.attrs)
		if hit && h.mode == ModeDrop {
			return nil
		}
		next = next.WithAttrs(attrs)
	}

	recordAttrs := make([]slog.Attr, 0, record.NumAttrs())
	record.Attrs(func(a slog.Attr) bool {
		recordAttrs = append(recordAttrs, a)
		return true
	})

	newAttrs, hit := h.filterAttrs(path, recordAttrs)
	if hit {
		if h.mode == ModeDrop {
			return nil
		}
		rebuilt := slog.NewRecord(record.Time, record.Level, record.Message, record.PC)
		rebuilt.AddAttrs(newAttrs...)
		record = rebuilt
	}

	return next.Handle(ctx, record)
}

// filterAttrs walks attrs -- recursing into nested groups -- looking for a
// rule match. path is the dotted group prefix attrs are nested under. It
// returns attrs unchanged unless mode is ModeRedact and something matched, in
// which case it returns a copy with matching values replaced. The returned
// bool reports whether any rule matched anywhere in attrs, regardless of mode.
func (h *FilterHandler) filterAttrs(path []string, attrs []slog.Attr) ([]slog.Attr, bool) {
	var out []slog.Attr // allocated lazily, only when mode is ModeRedact and something matched
	hit := false

	for i, a := range attrs {
		if a.Value.Kind() == slog.KindGroup {
			subAttrs, subHit := h.filterAttrs(append(slices.Clone(path), a.Key), a.Value.Group())
			if !subHit {
				continue
			}
			hit = true
			if h.mode == ModeRedact {
				if out == nil {
					out = slices.Clone(attrs)
				}
				out[i] = slog.Attr{Key: a.Key, Value: slog.GroupValue(subAttrs...)}
			}
			continue
		}

		rule, matched := h.matchRule(path, a)
		if !matched {
			continue
		}
		hit = true
		if h.mode == ModeDrop {
			return attrs, true
		}
		if out == nil {
			out = slices.Clone(attrs)
		}
		replacement := rule.Replacement
		if replacement == "" {
			replacement = defaultRedactionReplacement
		}
		out[i] = slog.String(a.Key, replacement)
	}

	if out == nil {
		out = attrs
	}
	return out, hit
}

// matchRule reports whether a rule matches a, checked against both its bare
// key and its full dotted path under path.
func (h *FilterHandler) matchRule(path []string, a slog.Attr) (FilterRule, bool) {
	full := a.Key
	if len(path) > 0 {
		full = strings.Join(path, ".") + "." + a.Key
	}

	for _, rule := range h.rules {
		if !strings.EqualFold(a.Key, rule.Key) && !strings.EqualFold(full, rule.Key) {
			continue
		}
		if rule.Value == "" || a.Value.String() == rule.Value {
			return rule, true
		}
	}
	return FilterRule{}, false
}

// WithAttrs returns a new FilterHandler with additional attributes.
//
// When rules are configured, the attrs are held (not forwarded to next)
// until Handle time, so they can be checked -- and, in ModeRedact, rewritten
// -- against the rules on every record. slices.Clip on the prior ops before
// appending guarantees this handler and any sibling derived from the same
// parent via With never share a backing array, so appending to one can never
// silently mutate the other's held attrs.
func (h *FilterHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(h.rules) == 0 {
		return &FilterHandler{next: h.next.WithAttrs(attrs), rules: h.rules, mode: h.mode}
	}
	return &FilterHandler{
		next:  h.next,
		rules: h.rules,
		mode:  h.mode,
		ops:   append(slices.Clip(h.ops), filterOp{attrs: slices.Clone(attrs)}),
	}
}

// WithGroup returns a new FilterHandler with a group name.
//
// As with WithAttrs, the group is held rather than forwarded to next when
// rules are configured, so record attrs and later With-supplied attrs nested
// under it are matched against their full dotted path.
func (h *FilterHandler) WithGroup(name string) slog.Handler {
	if len(h.rules) == 0 {
		return &FilterHandler{next: h.next.WithGroup(name), rules: h.rules, mode: h.mode}
	}
	return &FilterHandler{
		next:  h.next,
		rules: h.rules,
		mode:  h.mode,
		ops:   append(slices.Clip(h.ops), filterOp{isGroup: true, group: name}),
	}
}

// Unwrap returns the handler this one decorates. It lets a logger lifecycle
// (Flush, Shutdown) traverse a chain that was assembled by hand, instead of
// stopping at the outermost handler.
func (h *FilterHandler) Unwrap() slog.Handler { return h.next }
