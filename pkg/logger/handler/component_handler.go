package handler

import (
	"context"
	"log/slog"
	"path/filepath"
	"runtime"
	"strings"
)

// ComponentHandler enriches every record with a "component" group describing
// the call site that produced it:
//
//	component.file  base name of the source file
//	component.line  line number
//	component.func  function name without its package path prefix
//
// The call site is read from slog.Record.PC, which slog captures at the log
// call before any handler runs. Because the PC travels inside the record, the
// attribution is immune to asynchronous delivery (a BufferedHandler upstream
// calls Handle from its worker goroutine), to how deep ComponentHandler sits
// in a decorator chain, and to the layout of the calling package.
//
// Relationship with slog.HandlerOptions.AddSource: AddSource makes the
// terminal handler emit a standard "source" attr derived from the same PC, so
// it is a differently shaped duplicate of the "component" group. Keep
// AddSource off when using ComponentHandler.
type ComponentHandler struct {
	next slog.Handler
}

// NewComponentHandler wraps next so that every record carries a "component"
// group naming its call site.
func NewComponentHandler(next slog.Handler) slog.Handler {
	return &ComponentHandler{next: next}
}

// WithOffset returns a handler equivalent to the receiver.
//
// Deprecated: stack-depth compensation is no longer meaningful. Attribution
// comes from slog.Record.PC, which already points at the call site, so there
// is no stack to skip. The method is kept as a no-op for one release to stay
// source compatible and will be removed afterwards.
func (h *ComponentHandler) WithOffset(_ int) *ComponentHandler {
	return &ComponentHandler{next: h.next}
}

// Enabled reports whether the wrapped handler is enabled for level.
func (h *ComponentHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

// Handle adds the "component" group and forwards the record.
//
// A record that reached Handle was already accepted upstream, so it is always
// forwarded: dropping it here on an Enabled check would make
// delivered + dropped != produced.
//
// When record.PC is zero — records built by hand with slog.NewRecord carry no
// PC — no "component" group is added at all. Silence is honest; a
// file=unknown value is noise that breaks log parsing.
func (h *ComponentHandler) Handle(ctx context.Context, record slog.Record) error {
	if record.PC != 0 {
		frame, _ := runtime.CallersFrames([]uintptr{record.PC}).Next()
		if frame.File != "" {
			record.AddAttrs(slog.Group("component",
				slog.String("file", filepath.Base(frame.File)),
				slog.Int("line", frame.Line),
				slog.String("func", simplifyFuncName(frame.Function)),
			))
		}
	}

	return h.next.Handle(ctx, record)
}

// WithAttrs returns a handler that adds attrs to every record.
func (h *ComponentHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &ComponentHandler{next: h.next.WithAttrs(attrs)}
}

// WithGroup returns a handler that nests subsequent attrs under name.
func (h *ComponentHandler) WithGroup(name string) slog.Handler {
	return &ComponentHandler{next: h.next.WithGroup(name)}
}

// simplifyFuncName drops the package path prefix from a fully qualified
// function name, keeping the last path element: for example
// "github.com/acme/svc/orders.(*Service).Reserve" becomes
// "orders.(*Service).Reserve".
func simplifyFuncName(fn string) string {
	if i := strings.LastIndex(fn, "/"); i != -1 {
		fn = fn[i+1:]
	}
	return fn
}

// Unwrap returns the handler this one decorates. It lets a logger lifecycle
// (Flush, Shutdown) traverse a chain that was assembled by hand, instead of
// stopping at the outermost handler.
func (h *ComponentHandler) Unwrap() slog.Handler { return h.next }
