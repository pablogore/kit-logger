package logger

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSetContextFieldExtractor(t *testing.T) {
	// Test setting context field extractor
	extractorCalled := false
	extractor := func(ctx context.Context) []any {
		extractorCalled = true
		return []any{"key", "value"}
	}

	SetContextFieldExtractor(extractor)

	// Verify global extractor is set
	assert.NotNil(t, contextFieldExtractor())

	// Test that extractor is called
	ctx := context.Background()
	result := contextFieldExtractor()(ctx)

	assert.True(t, extractorCalled)
	assert.Equal(t, []any{"key", "value"}, result)
}

func TestSetContextFieldExtractor_Nil(t *testing.T) {
	// Test setting nil extractor
	SetContextFieldExtractor(nil)

	// Verify global extractor is set to nil
	assert.Nil(t, contextFieldExtractor())
}

func TestContextFieldExtractor_Integration(t *testing.T) {
	// Test integration with logger
	extractor := func(ctx context.Context) []any {
		if userID, ok := ctx.Value("user_id").(string); ok {
			return []any{"user_id", userID}
		}
		return nil
	}

	SetContextFieldExtractor(extractor)

	// Create logger
	cfg := Config{Level: LevelDebug}
	logger := New(cfg)

	// Test with context that has user_id
	ctx := context.WithValue(context.Background(), "user_id", "12345")
	loggerWithCtx := logger.WithContext(ctx)

	// The logger should now have the user_id field from context
	loggerWithCtx.Info("test message")

	// Test with context without user_id
	ctx2 := context.Background()
	loggerWithCtx2 := logger.WithContext(ctx2)
	loggerWithCtx2.Info("test message without user_id")
}

func TestContextFieldExtractor_MultipleFields(t *testing.T) {
	// Test extractor that returns multiple fields
	extractor := func(ctx context.Context) []any {
		fields := []any{}

		if userID, ok := ctx.Value("user_id").(string); ok {
			fields = append(fields, "user_id", userID)
		}

		if requestID, ok := ctx.Value("request_id").(string); ok {
			fields = append(fields, "request_id", requestID)
		}

		return fields
	}

	SetContextFieldExtractor(extractor)

	// Test with multiple context values
	ctx := context.WithValue(context.Background(), "user_id", "12345")
	ctx = context.WithValue(ctx, "request_id", "req-67890")

	result := contextFieldExtractor()(ctx)
	expected := []any{"user_id", "12345", "request_id", "req-67890"}

	assert.Equal(t, expected, result)
}

func TestContextFieldExtractor_EmptyContext(t *testing.T) {
	// Test extractor with empty context
	extractor := func(ctx context.Context) []any {
		return []any{"default", "value"}
	}

	SetContextFieldExtractor(extractor)

	ctx := context.Background()
	result := contextFieldExtractor()(ctx)

	assert.Equal(t, []any{"default", "value"}, result)
}

func TestContextFieldExtractor_ComplexTypes(t *testing.T) {
	// Test extractor with complex types
	extractor := func(ctx context.Context) []any {
		fields := []any{}

		if user, ok := ctx.Value("user").(map[string]interface{}); ok {
			fields = append(fields, "user_name", user["name"])
			fields = append(fields, "user_role", user["role"])
		}

		return fields
	}

	SetContextFieldExtractor(extractor)

	user := map[string]interface{}{
		"name": "John Doe",
		"role": "admin",
	}
	ctx := context.WithValue(context.Background(), "user", user)

	result := contextFieldExtractor()(ctx)
	expected := []any{"user_name", "John Doe", "user_role", "admin"}

	assert.Equal(t, expected, result)
}
