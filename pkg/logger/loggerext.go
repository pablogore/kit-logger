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

// ensureLoggerOnce guards EnsureLoggerOnce. It is named rather than left as a
// bare "once" because the package now has two: this one and globalOnce, which
// guards L's lazy default.
var ensureLoggerOnce sync.Once

// EnsureLoggerOnce initializes the global logger only once with the provided
// configuration. Later calls are no-ops, whatever cfg they are given.
//
// It does not install the logger as slog.Default; see SetGlobal.
func EnsureLoggerOnce(cfg Config) {
	ensureLoggerOnce.Do(func() {
		SetGlobal(New(cfg))
	})
}
