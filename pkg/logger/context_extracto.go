package logger

import "context"

// ContextFieldExtractorFunc defines a function type for extracting fields from a context.
type ContextFieldExtractorFunc func(ctx context.Context) []any

var globalContextFieldExtractor ContextFieldExtractorFunc

// SetContextFieldExtractor allows the consumer to register their custom context field extractor function.
func SetContextFieldExtractor(f ContextFieldExtractorFunc) {
	globalContextFieldExtractor = f
}
