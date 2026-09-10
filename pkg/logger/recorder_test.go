package logger_test

import (
	"context"
	"log/slog"
	"sync"

	"github.com/pablogore/kit-logger/pkg/logger/kitlogtest"
)

// recorder is a sink that is safe to read from the test goroutine even when the
// records reach it from the buffered handler's worker.
type recorder struct {
	mu      sync.Mutex
	records []slog.Record
}

func (r *recorder) handler() slog.Handler {
	return kitlogtest.NewTestHandler(func(_ context.Context, rec slog.Record) {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.records = append(r.records, rec.Clone())
	})
}

func (r *recorder) find(msg string) (slog.Record, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, rec := range r.records {
		if rec.Message == msg {
			return rec, true
		}
	}
	return slog.Record{}, false
}
