// Package testdata provides a call-site proxy used to prove end-to-end that
// source attribution points at the frame that called the logger, however deep
// that call sits below the handler pipeline.
package testdata

import (
	"log/slog"
	"path/filepath"
	"runtime"
)

// LogInvoker logs through a nested helper so that the attributed call site is
// several frames below the caller of Invoke.
type LogInvoker struct{}

// Invoke emits one record from a nested helper and returns the file base name
// and line that the record must be attributed to.
func (LogInvoker) Invoke(logger *slog.Logger) (file string, line int) {
	return deeperCall(logger)
}

func deeperCall(logger *slog.Logger) (file string, line int) {
	_, self, at, _ := runtime.Caller(0)
	logger.Info("testing component")
	return filepath.Base(self), at + 1
}
