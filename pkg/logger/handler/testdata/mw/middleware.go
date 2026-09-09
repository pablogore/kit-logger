// Package mw is a fixture that logs from a file named middleware.go, which was
// on the removed attribution denylist.
package mw

import (
	"log/slog"
	"path/filepath"
	"runtime"
)

// Log emits one record and returns the file base name and line the record must
// be attributed to.
func Log(logger *slog.Logger) (file string, line int) {
	_, self, at, _ := runtime.Caller(0)
	logger.Info("request traced")
	return filepath.Base(self), at + 1
}
