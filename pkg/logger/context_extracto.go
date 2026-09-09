package logger

import (
	"context"
	"sync/atomic"
)

// ContextFieldExtractorFunc defines a function type for extracting fields from a context.
type ContextFieldExtractorFunc func(ctx context.Context) []any

// globalContextFieldExtractor is the process-wide fallback extractor. It is
// published through an atomic.Pointer because WithContext reads it on the
// per-log path of every consumer: a plain assignment establishes no
// happens-before edge with that read, so a reader is not guaranteed to observe
// the new function at all. That is a data race by the memory model regardless
// of how many words the value happens to occupy, and the race detector flags
// it as one.
var globalContextFieldExtractor atomic.Pointer[ContextFieldExtractorFunc]

// SetContextFieldExtractor registers a process-wide context field extractor.
// It is safe to call concurrently with WithContext.
//
// Deprecated: prefer Config.ContextFields, which gives a logger its own
// extractor. A per-instance extractor is immutable, so it needs no
// synchronization at all, cannot be swapped out from under a logger by
// unrelated code, and is testable without mutating package state. This
// function remains as a compatibility shim.
func SetContextFieldExtractor(f ContextFieldExtractorFunc) {
	if f == nil {
		globalContextFieldExtractor.Store(nil)
		return
	}
	globalContextFieldExtractor.Store(&f)
}

// contextFieldExtractor returns the process-wide extractor, or nil if none is
// registered.
func contextFieldExtractor() ContextFieldExtractorFunc {
	if p := globalContextFieldExtractor.Load(); p != nil {
		return *p
	}
	return nil
}
