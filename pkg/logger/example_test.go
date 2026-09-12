package logger_test

import (
	"context"
	"log/slog"
	"os"
	"time"

	"github.com/pablogore/kit-logger/pkg/logger"
	"github.com/pablogore/kit-logger/pkg/logger/handler"
)

// Example_basicUsage mirrors the README's Basic Usage section. It has no
// Output comment: the JSON line it emits carries a wall-clock timestamp, so
// asserting output isn't possible. What this buys instead is compilation --
// go vet and go test ./... fail the moment this drifts from the real import
// path or the real Logger method signatures, which is exactly the drift the
// README had (a pre-rename import path, and Info called with a context
// argument it does not take).
func Example_basicUsage() {
	log := logger.New(logger.Config{
		Level:  logger.LevelInfo,
		Format: logger.FormatJSON,
		GlobalFields: map[string]string{
			"service": "my-api",
			"env":     "prod",
		},
	})

	logger.SetGlobal(log)
	log.Info("starting service")
}

// Example_structuredLogging mirrors the README's structured logging usage:
// a non-default level and format, plus fields passed at the call site.
func Example_structuredLogging() {
	log := logger.New(logger.Config{
		Level:  logger.LevelDebug,
		Format: logger.FormatText,
	})

	log.Info("user login", "user_id", "123", "ip", "192.168.1.1")
}

// Example_globalFields mirrors the README's Global fields section: a global
// field replaces a colliding record attribute instead of being emitted next
// to it, and the handler is usable on its own around a bare slog handler,
// where record attrs nest under WithGroup while the global fields stay at
// the top level.
func Example_globalFields() {
	log := logger.New(logger.Config{
		Format:       logger.FormatJSON,
		GlobalFields: map[string]string{"env": "prod", "service": "checkout"},
	})

	log.Info("config reloaded", "env", "canary")

	h := handler.NewGlobalFieldsHandler(slog.NewJSONHandler(os.Stdout, nil),
		map[string]string{"env": "prod"}, true)

	slog.New(h).WithGroup("request").Info("handled", "method", "GET")
}

// Example_writer mirrors the README's Destination section: Config.Writer
// redirects the pipeline's output without changing anything else about it.
func Example_writer() {
	log := logger.New(logger.Config{
		Level:  logger.LevelInfo,
		Format: logger.FormatJSON,
		Writer: os.Stderr, // keeps stdout free for a CLI's own machine-readable output
	})

	log.Info("written to stderr")
}

// Example_lifecycle mirrors the README's Lifecycle section: a buffered
// logger is drained explicitly through ManagedLogger before the process
// exits, so the in-flight tail is not lost.
func Example_lifecycle() {
	log := logger.New(logger.Config{BufferSize: 4096, Format: logger.FormatJSON})
	logger.SetGlobal(log)

	if managed, ok := log.(logger.ManagedLogger); ok {
		defer func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = managed.Shutdown(ctx)
		}()
	}

	log.Info("buffered, delivered on shutdown")
}

// Example_globalLogger mirrors the README's Globals section: SetGlobal is
// safe to call concurrently with L(), and installs nothing into slog's
// process-wide default.
func Example_globalLogger() {
	log := logger.New(logger.Config{Level: logger.LevelInfo, Format: logger.FormatJSON})
	logger.SetGlobal(log) // safe concurrently with L(), even under load

	logger.L().Info("through the global accessor")
}

// exampleRequestIDKey and requestIDFrom stand in for whatever a host application
// uses to carry a request ID in a context.
type exampleRequestIDKey struct{}

func requestIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(exampleRequestIDKey{}).(string)
	return id
}

// Example_contextFields mirrors the README's context-fields section: the
// extractor runs on WithContext and on every *Context log method alike.
func Example_contextFields() {
	log := logger.New(logger.Config{
		ContextFields: func(ctx context.Context) []any {
			return []any{"request_id", requestIDFrom(ctx)}
		},
	})

	ctx := context.WithValue(context.Background(), exampleRequestIDKey{}, "req-42")

	log.WithContext(ctx).Info("handling request") // carries request_id
	log.InfoContext(ctx, "handling request")      // carries request_id too
}
