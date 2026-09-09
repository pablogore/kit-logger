package logger

import (
	"context"
	"os"
	"sync"
)

// exitFunc is a variable that can be replaced for testing
var exitFunc = os.Exit

// ExitWithFlush shuts the global logger down and then terminates the program
// with the specified exit code.
//
// The drain is bounded by DefaultShutdownTimeout: a process on its way out must
// not hang on a stuck downstream handler. A logger that does not implement
// ManagedLogger falls back to Sync.
func ExitWithFlush(code int) {
	ctx, cancel := context.WithTimeout(context.Background(), DefaultShutdownTimeout)
	defer cancel()

	log := L()
	if managed, ok := log.(ManagedLogger); ok {
		_ = managed.Shutdown(ctx)
	} else {
		_ = log.Sync()
	}

	exitFunc(code)
}

var once sync.Once

// EnsureLoggerOnce initializes the global logger only once with the provided configuration.
func EnsureLoggerOnce(cfg Config) {
	once.Do(func() {
		SetGlobal(New(cfg))
	})
}
