package logger_test

import (
	"github.com/pablogore/kit-logger/pkg/logger"
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
		Level:  "info",
		Format: "json",
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
		Level:  "debug",
		Format: "text",
	})

	log.Info("user login", "user_id", "123", "ip", "192.168.1.1")
}
