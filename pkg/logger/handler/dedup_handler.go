package handler

import (
	"context"
	"log/slog"
	"slices"
)

// DedupHandler guarantees that the record it forwards carries each key at
// most once per group, so the terminal handler never writes a line with two
// members of the same name.
//
// slog deliberately leaves duplicate keys alone: log/slog's JSON handler
// writes them verbatim, and a shipper that parses the line then has to guess
// which value wins (golang/go#59365). The duplicates come from the ergonomic
// paths, not from mistakes: a logger derived with With("request_id", id), a
// context extractor that adds "request_id" again, and a call site that logs
// it a third time all end up on the same line.
//
// Resolution rules:
//
//   - Value of the last occurrence, position of the first. A key learned
//     deeper in the call replaces the earlier value, while the line keeps a
//     stable shape. This is what veqryn/slog-dedup calls "overwrite", the
//     mode it recommends.
//   - Pinned attrs come first and never lose. They are meant for
//     resource-level fields (service, env, version) that describe the process
//     and must not be overridden by a record. See DedupOptions.Pinned.
//   - Reserved keys belong to the terminal handler. time, level and msg are
//     always reserved; DedupOptions.Reserved adds more (source, when the
//     pipeline emits it). A top-level attr with a reserved key is renamed to
//     "attr.<key>" so it can neither shadow nor duplicate the built-in.
//   - Two groups with the same key are merged, member by member, so that
//     With("http", ...) and a record's slog.Group("http", ...) fold into one
//     object instead of two.
//   - An attr whose value is a group with an empty key is inlined, and an
//     empty group is elided, exactly as slog's own handlers do.
//
// DedupHandler must sit directly above the terminal handler. slog's Text and
// JSON handlers pre-format WithAttrs when they are called, and an attr that
// has already been formatted cannot be compared against anything, so every
// WithAttrs and WithGroup is held here and materialized into the record at
// Handle time. The wrapped handler therefore never sees WithAttrs or
// WithGroup at all; nesting is rebuilt with slog.GroupValue instead. That
// trades slog's pre-formatting optimization for a line whose shape can be
// trusted, which is the point of the handler.
type DedupHandler struct {
	next     slog.Handler
	pinned   []slog.Attr
	reserved map[string]struct{}
	ops      []dedupOp
}

// dedupOp is one held WithAttrs or WithGroup call.
type dedupOp struct {
	attrs   []slog.Attr // set when isGroup is false
	group   string      // set when isGroup is true
	isGroup bool
}

// DedupOptions configures NewDedupHandler.
type DedupOptions struct {
	// Pinned attrs are emitted first, at the top level, in the order given,
	// and win over any attr with the same key from With, from the record or
	// from a context extractor. Attrs with an empty key are dropped.
	Pinned []slog.Attr

	// Reserved lists top-level keys owned by the terminal handler, in
	// addition to time, level and msg, which are always reserved. A record
	// attr that uses one is renamed to "attr.<key>".
	Reserved []string
}

// NewDedupHandler wraps next so that every record it receives carries each
// key at most once per group.
func NewDedupHandler(next slog.Handler, opts DedupOptions) *DedupHandler {
	reserved := map[string]struct{}{
		slog.TimeKey:    {},
		slog.LevelKey:   {},
		slog.MessageKey: {},
	}
	for _, k := range opts.Reserved {
		if k != "" {
			reserved[k] = struct{}{}
		}
	}
	return &DedupHandler{
		next:     next,
		pinned:   filterValidAttrs(slices.Clone(opts.Pinned)),
		reserved: reserved,
	}
}

// Enabled reports whether the wrapped handler is enabled for level.
func (h *DedupHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

// Handle resolves every held attr and the record's own attrs into a single
// tree with unique keys and forwards a fresh record built from it.
//
// A record with no held ops, no pinned attrs, no groups, no reserved key and
// no repeated key is forwarded untouched: that is the common case for a
// plain log call, and it costs one pass over the attrs.
func (h *DedupHandler) Handle(ctx context.Context, record slog.Record) error {
	if len(h.ops) == 0 && len(h.pinned) == 0 && h.isClean(record) {
		return h.next.Handle(ctx, record)
	}

	root := newDedupNode()
	for _, a := range h.pinned {
		root.insert(a, true)
	}
	cur := root
	for _, op := range h.ops {
		if op.isGroup {
			cur = cur.child(op.group)
			continue
		}
		for _, a := range op.attrs {
			cur.insert(a, false)
		}
	}
	record.Attrs(func(a slog.Attr) bool {
		cur.insert(a, false)
		return true
	})

	out := slog.NewRecord(record.Time, record.Level, record.Message, record.PC)
	out.AddAttrs(root.materialize(h.reserved)...)
	return h.next.Handle(ctx, out)
}

// isClean reports whether the record's top-level attrs can be forwarded as
// they are: no group values, no reserved keys and no repeated keys.
func (h *DedupHandler) isClean(record slog.Record) bool {
	n := record.NumAttrs()
	if n == 0 {
		return true
	}
	seen := make([]string, 0, n)
	clean := true
	record.Attrs(func(a slog.Attr) bool {
		if a.Value.Kind() == slog.KindGroup || a.Value.Kind() == slog.KindLogValuer {
			clean = false
			return false
		}
		if _, ok := h.reserved[a.Key]; ok || slices.Contains(seen, a.Key) {
			clean = false
			return false
		}
		seen = append(seen, a.Key)
		return true
	})
	return clean
}

// WithAttrs returns a handler that adds attrs to every record. The attrs are
// held, not forwarded: see the type documentation.
func (h *DedupHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	attrs = filterValidAttrs(attrs)
	if len(attrs) == 0 {
		return h
	}
	return h.derive(dedupOp{attrs: slices.Clone(attrs)})
}

// WithGroup returns a handler that nests subsequent attrs under name. An
// empty name returns the receiver, as slog's own handlers do.
func (h *DedupHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	return h.derive(dedupOp{isGroup: true, group: name})
}

// derive copies the receiver with one more held op. slices.Clip on the prior
// ops guarantees siblings derived from the same parent never share a backing
// array.
func (h *DedupHandler) derive(op dedupOp) *DedupHandler {
	return &DedupHandler{
		next:     h.next,
		pinned:   h.pinned,
		reserved: h.reserved,
		ops:      append(slices.Clip(h.ops), op),
	}
}

// Unwrap returns the handler this one decorates. It lets a logger lifecycle
// (Flush, Shutdown) traverse a chain that was assembled by hand, instead of
// stopping at the outermost handler.
func (h *DedupHandler) Unwrap() slog.Handler { return h.next }

// dedupNode is one level of the attr tree: an ordered set of entries keyed
// by attr key.
type dedupNode struct {
	keys    []string
	entries map[string]*dedupEntry
}

// dedupEntry is one key at one level: either a scalar attr or a nested node.
type dedupEntry struct {
	attr   slog.Attr  // set when node is nil
	node   *dedupNode // set for a group
	pinned bool
}

func newDedupNode() *dedupNode {
	return &dedupNode{entries: map[string]*dedupEntry{}}
}

// child returns the nested node for key, creating it as a group if the key
// is absent or currently holds a scalar (a later group replaces an earlier
// scalar, the same last-wins rule scalars follow).
func (n *dedupNode) child(key string) *dedupNode {
	if e, ok := n.entries[key]; ok {
		if e.node == nil {
			if e.pinned {
				// A pinned scalar keeps its value; the group's members have
				// nowhere to go, so they are collected in a detached node
				// and dropped at materialize time.
				return newDedupNode()
			}
			e.node = newDedupNode()
			e.attr = slog.Attr{}
		}
		return e.node
	}
	e := &dedupEntry{node: newDedupNode()}
	n.entries[key] = e
	n.keys = append(n.keys, key)
	return e.node
}

// insert adds a into the node under the resolution rules.
func (n *dedupNode) insert(a slog.Attr, pinned bool) {
	a.Value = a.Value.Resolve()
	if a.Value.Kind() == slog.KindGroup {
		members := a.Value.Group()
		if a.Key == "" {
			// slog inlines a group with an empty key.
			for _, m := range members {
				n.insert(m, pinned)
			}
			return
		}
		if len(members) == 0 {
			return // slog elides an empty group.
		}
		if e, ok := n.entries[a.Key]; ok && e.pinned && !pinned {
			return
		}
		sub := n.child(a.Key)
		for _, m := range members {
			sub.insert(m, pinned)
		}
		if pinned {
			n.entries[a.Key].pinned = true
		}
		return
	}
	if a.Equal(slog.Attr{}) {
		return // slog ignores the zero Attr.
	}
	if e, ok := n.entries[a.Key]; ok {
		if e.pinned && !pinned {
			return
		}
		e.attr = a
		e.node = nil
		e.pinned = e.pinned || pinned
		return
	}
	n.entries[a.Key] = &dedupEntry{attr: a, pinned: pinned}
	n.keys = append(n.keys, a.Key)
}

// materialize renders the tree back into attrs, in insertion order. Empty
// groups are elided. reserved is only consulted at the top level; nested
// levels pass nil.
func (n *dedupNode) materialize(reserved map[string]struct{}) []slog.Attr {
	out := make([]slog.Attr, 0, len(n.keys))
	for _, k := range n.keys {
		e := n.entries[k]
		key := k
		if _, ok := reserved[key]; ok {
			key = "attr." + key
		}
		if e.node == nil {
			a := e.attr
			a.Key = key
			out = append(out, a)
			continue
		}
		members := e.node.materialize(nil)
		if len(members) == 0 {
			continue
		}
		out = append(out, slog.Attr{Key: key, Value: slog.GroupValue(members...)})
	}
	return out
}
