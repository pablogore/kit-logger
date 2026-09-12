package logger_test

import (
	"log/slog"
	"os"

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
