// Package billing is a fixture that logs from a file under a pkg/ directory
// named handler.go. Both the directory and the file name were on the removed
// attribution denylist.
package billing

import (
	"log/slog"
	"path/filepath"
	"runtime"
)

// Log emits one record and returns the file base name and line the record must
// be attributed to.
func Log(logger *slog.Logger) (file string, line int) {
	_, self, at, _ := runtime.Caller(0)
	logger.Info("invoice charged")
	return filepath.Base(self), at + 1
}
