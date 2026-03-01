package handler

import (
	"context"
	"log/slog"
	"path/filepath"
	"runtime"
	"strings"
)

type ComponentHandler struct {
	next        slog.Handler
	offsetDepth int
}

func NewComponentHandler(next slog.Handler) slog.Handler {
	return &ComponentHandler{next: next}
}

func (h *ComponentHandler) WithOffset(offset int) *ComponentHandler {
	return &ComponentHandler{
		next:        h.next,
		offsetDepth: h.offsetDepth + offset,
	}
}

func (h *ComponentHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

func (h *ComponentHandler) Handle(ctx context.Context, record slog.Record) error {
	if isGraphQLLog(record.Message) {
		return h.next.Handle(ctx, record)
	}

	if !h.Enabled(ctx, record.Level) {
		return nil
	}

	const maxDepth = 25
	for i := 2 + h.offsetDepth; i < maxDepth; i++ {
		pc, file, line, ok := runtime.Caller(i)
		if !ok || isInternalFrame(file) {
			continue
		}

		funcName := "unknown"
		if fn := runtime.FuncForPC(pc); fn != nil {
			funcName = simplifyFuncName(fn.Name())
		}

		record.AddAttrs(slog.Group("component",
			slog.String("file", filepath.Base(file)),
			slog.Int("line", line),
			slog.String("func", funcName),
		))
		break
	}

	return h.next.Handle(ctx, record)
}

func (h *ComponentHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &ComponentHandler{
		next:        h.next.WithAttrs(attrs),
		offsetDepth: h.offsetDepth,
	}
}

func (h *ComponentHandler) WithGroup(name string) slog.Handler {
	return &ComponentHandler{
		next:        h.next.WithGroup(name),
		offsetDepth: h.offsetDepth,
	}
}

func isGraphQLLog(msg string) bool {
	return strings.HasPrefix(msg, "GraphQL HTTP Trace") ||
		strings.HasPrefix(msg, "GraphQL Error(s) Found")
}

func simplifyFuncName(fn string) string {
	if i := strings.LastIndex(fn, "/"); i != -1 {
		fn = fn[i+1:]
	}
	return fn
}

func isInternalFrame(file string) bool {
	base := filepath.Base(file)

	if strings.HasSuffix(base, "_test.go") {
		return false
	}

	internalPatterns := []string{
		"component_handler.go",
		"slog_logger.go",
		"SlogLogger.",
		"/log/slog/",
		"/pkg/logger/",
		"/runtime/",
		"/reflect/",
		"/testing/",
		"/vendor/",
		"/internal/",
		"/pkg/",
		"/cmd/",
		"/main.go",
		"/init.go",
		"/setup.go",
		"/bootstrap.go",
		"/startup.go",
		"/server.go",
		"/app.go",
		"/application.go",
		"/service.go",
		"/handler.go",
		"/controller.go",
		"/middleware.go",
		"/interceptor.go",
		"/grpc.go",
		"/http.go",
		"/rest.go",
		"/api.go",
		"/endpoint.go",
		"/route.go",
		"/router.go",
		"/mux.go",
		"/client.go",
	}

	for _, pattern := range internalPatterns {
		if strings.Contains(file, pattern) {
			return true
		}
	}

	return false
}
