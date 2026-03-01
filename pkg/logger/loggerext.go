package logger

import (
	"os"
	"sync"
)

// exitFunc is a variable that can be replaced for testing
var exitFunc = os.Exit

// ExitWithFlush terminates the program with the specified exit code after flushing the logger.
func ExitWithFlush(code int) {
	if flusher, ok := L().(interface{ Sync() error }); ok {
		_ = flusher.Sync()
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
