package handler

import (
	"context"
	"log/slog"
	"path/filepath"
	"runtime"
	"strings"
)

// SourceHandler enriches every record with a "source" group describing the
// call site that produced it, in the shape slog.HandlerOptions.AddSource
// uses:
//
//	source.function  function name without its package path prefix
//	source.file      base name of the source file
//	source.line      line number
//
// The group is named "source" because that is the key slog reserves for
// exactly this information, so a consumer that already parses slog's own
// AddSource output parses this too. It leaves "component" free for what
// most services use it for: naming the subsystem that logged.
//
// The call site is read from slog.Record.PC, which slog captures at the log
// call before any handler runs. Because the PC travels inside the record, the
// attribution is immune to asynchronous delivery (a BufferedHandler upstream
// calls Handle from its worker goroutine), to how deep SourceHandler sits in
// a decorator chain, and to the layout of the calling package.
//
// Keep slog.HandlerOptions.AddSource off on the terminal handler when using
// SourceHandler; the two would emit the same key twice.
type SourceHandler struct {
	next slog.Handler
}

// NewSourceHandler wraps next so that every record carries a "source" group
// naming its call site.
func NewSourceHandler(next slog.Handler) slog.Handler {
	return &SourceHandler{next: next}
}

// Enabled reports whether the wrapped handler is enabled for level.
func (h *SourceHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

// Handle adds the "source" group and forwards the record.
//
// A record that reached Handle was already accepted upstream, so it is always
// forwarded. When record.PC is zero (records built by hand with
// slog.NewRecord carry no PC) no group is added: silence is honest, and a
// file=unknown value is noise that breaks log parsing.
func (h *SourceHandler) Handle(ctx context.Context, record slog.Record) error {
	if record.PC != 0 {
		frame, _ := runtime.CallersFrames([]uintptr{record.PC}).Next()
		if frame.File != "" {
			record.AddAttrs(slog.Group(slog.SourceKey,
				slog.String("function", shortFuncName(frame.Function)),
				slog.String("file", filepath.Base(frame.File)),
				slog.Int("line", frame.Line),
			))
		}
	}
	return h.next.Handle(ctx, record)
}

// WithAttrs returns a handler that adds attrs to every record.
func (h *SourceHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &SourceHandler{next: h.next.WithAttrs(attrs)}
}

// WithGroup returns a handler that nests subsequent attrs under name.
func (h *SourceHandler) WithGroup(name string) slog.Handler {
	return &SourceHandler{next: h.next.WithGroup(name)}
}

// Unwrap returns the handler this one decorates. It lets a logger lifecycle
// (Flush, Shutdown) traverse a chain that was assembled by hand, instead of
// stopping at the outermost handler.
func (h *SourceHandler) Unwrap() slog.Handler { return h.next }

// shortFuncName drops the package path prefix from a fully qualified
// function name and the compiler-generated closure suffixes, keeping the
// last path element: "github.com/acme/svc/orders.(*Service).Reserve" becomes
// "orders.(*Service).Reserve", and "httpmw.New.func1.1.1" becomes
// "httpmw.New". A closure has no name a reader can search for; the function
// that defined it does.
func shortFuncName(fn string) string {
	fn = simplifyFuncName(fn)
	for {
		i := strings.LastIndex(fn, ".")
		if i == -1 {
			return fn
		}
		suffix := fn[i+1:]
		if !isClosureSuffix(suffix) {
			return fn
		}
		fn = fn[:i]
	}
}

// isClosureSuffix reports whether s is a compiler-generated closure name
// segment: "func1", "func2", or a bare index such as "1" produced by a
// closure nested inside another.
func isClosureSuffix(s string) bool {
	s = strings.TrimPrefix(s, "func")
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
