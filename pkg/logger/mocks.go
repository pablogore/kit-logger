package logger

import (
	"context"
)

// MockContextFieldExtractorFunc implements ContextFieldExtractorFunc for testing.
type MockContextFieldExtractorFunc struct {
	ExecuteFunc func(ctx context.Context) []any
}

// Execute executes the context field extractor function.
func (m *MockContextFieldExtractorFunc) Execute(ctx context.Context) []any {
	if m.ExecuteFunc != nil {
		return m.ExecuteFunc(ctx)
	}
	return []any{}
}

// AsFunc returns the mock as a ContextFieldExtractorFunc.
func (m *MockContextFieldExtractorFunc) AsFunc() ContextFieldExtractorFunc {
	return func(ctx context.Context) []any {
		return m.Execute(ctx)
	}
}

// NewMockContextFieldExtractorFunc creates a new mock ContextFieldExtractorFunc.
func NewMockContextFieldExtractorFunc() *MockContextFieldExtractorFunc {
	return &MockContextFieldExtractorFunc{}
}
