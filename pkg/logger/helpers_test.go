package logger

import (
	"errors"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestString(t *testing.T) {
	attr := String("key", "value")
	assert.Equal(t, "key", attr.Key)
	assert.Equal(t, "value", attr.Value.String())
	assert.Equal(t, slog.StringValue("value"), attr.Value)
}

func TestError(t *testing.T) {
	err := errors.New("test error")
	attr := Error(err)
	assert.Equal(t, "error", attr.Key)
	assert.Equal(t, "test error", attr.Value.String())
	assert.Equal(t, slog.StringValue("test error"), attr.Value)
}

func TestErrorValue(t *testing.T) {
	err := errors.New("test error value")
	value := ErrorValue(err)
	assert.Equal(t, "test error value", value)
}

func TestInt(t *testing.T) {
	attr := Int("count", 42)
	assert.Equal(t, "count", attr.Key)
	assert.Equal(t, int64(42), attr.Value.Int64())
	assert.Equal(t, slog.Int64Value(42), attr.Value)
}

func TestInt64(t *testing.T) {
	attr := Int64("big_count", 9223372036854775807)
	assert.Equal(t, "big_count", attr.Key)
	assert.Equal(t, int64(9223372036854775807), attr.Value.Int64())
	assert.Equal(t, slog.Int64Value(9223372036854775807), attr.Value)
}

func TestBool(t *testing.T) {
	attr := Bool("enabled", true)
	assert.Equal(t, "enabled", attr.Key)
	assert.Equal(t, true, attr.Value.Bool())
	assert.Equal(t, slog.BoolValue(true), attr.Value)

	attr = Bool("disabled", false)
	assert.Equal(t, "disabled", attr.Key)
	assert.Equal(t, false, attr.Value.Bool())
	assert.Equal(t, slog.BoolValue(false), attr.Value)
}

func TestFloat64(t *testing.T) {
	attr := Float64("pi", 3.14159)
	assert.Equal(t, "pi", attr.Key)
	assert.Equal(t, 3.14159, attr.Value.Float64())
	assert.Equal(t, slog.Float64Value(3.14159), attr.Value)
}

func TestAny(t *testing.T) {
	// Test with string
	attr := Any("string_value", "hello")
	assert.Equal(t, "string_value", attr.Key)
	assert.Equal(t, "hello", attr.Value.String())

	// Test with int
	attr = Any("int_value", 123)
	assert.Equal(t, "int_value", attr.Key)
	assert.Equal(t, int64(123), attr.Value.Int64())

	// Test with bool
	attr = Any("bool_value", true)
	assert.Equal(t, "bool_value", attr.Key)
	assert.Equal(t, true, attr.Value.Bool())

	// Test with float
	attr = Any("float_value", 3.14)
	assert.Equal(t, "float_value", attr.Key)
	assert.Equal(t, 3.14, attr.Value.Float64())

	// Test with struct
	type Person struct {
		Name string
		Age  int
	}
	person := Person{Name: "John", Age: 30}
	attr = Any("person", person)
	assert.Equal(t, "person", attr.Key)
	assert.Equal(t, "{John 30}", attr.Value.String())
}

func TestInt32(t *testing.T) {
	attr := Int32("small_count", 2147483647)
	assert.Equal(t, "small_count", attr.Key)
	assert.Equal(t, int64(2147483647), attr.Value.Int64())
	assert.Equal(t, slog.Int64Value(2147483647), attr.Value)
}

func TestStrings(t *testing.T) {
	strings := []string{"apple", "banana", "cherry"}
	attr := Strings("fruits", strings)
	assert.Equal(t, "fruits", attr.Key)
	assert.Equal(t, "[apple banana cherry]", attr.Value.String())

	// Test empty slice
	emptyStrings := []string{}
	attr = Strings("empty", emptyStrings)
	assert.Equal(t, "empty", attr.Key)
	assert.Equal(t, "[]", attr.Value.String())

	// Test nil slice
	attr = Strings("nil", nil)
	assert.Equal(t, "nil", attr.Key)
	assert.Equal(t, "[]", attr.Value.String())
}

func TestNewLogger_WithNoOptions(t *testing.T) {
	logger := NewLogger()
	assert.NotNil(t, logger)
	assert.IsType(t, &SlogLogger{}, logger)
}

func TestNewLogger_WithLevelOption(t *testing.T) {
	logger := NewLogger(WithLevel("debug"))
	assert.NotNil(t, logger)
	assert.IsType(t, &SlogLogger{}, logger)
}

func TestNewLogger_WithEncodingOption(t *testing.T) {
	logger := NewLogger(WithEncoding("json"))
	assert.NotNil(t, logger)
	assert.IsType(t, &SlogLogger{}, logger)
}

func TestNewLogger_WithServiceOption(t *testing.T) {
	logger := NewLogger(WithService("test-service"))
	assert.NotNil(t, logger)
	assert.IsType(t, &SlogLogger{}, logger)
}

func TestNewLogger_WithMultipleOptions(t *testing.T) {
	logger := NewLogger(
		WithLevel("warn"),
		WithEncoding("text"),
		WithService("multi-service"),
	)
	assert.NotNil(t, logger)
	assert.IsType(t, &SlogLogger{}, logger)
}

func TestWithLevel(t *testing.T) {
	option := WithLevel("error")
	cfg := Config{}
	option(&cfg)
	assert.Equal(t, "error", cfg.LevelString)

	resolved, err := resolveLevel("Level", cfg.Level, cfg.LevelString)
	require.NoError(t, err)
	assert.Equal(t, LevelError, resolved)
}

func TestWithEncoding(t *testing.T) {
	option := WithEncoding("json")
	cfg := Config{}
	option(&cfg)
	assert.Equal(t, "json", cfg.FormatString)

	resolved, err := resolveFormat("Format", cfg.Format, cfg.FormatString)
	require.NoError(t, err)
	assert.Equal(t, FormatJSON, resolved)
}

func TestWithService_FirstTime(t *testing.T) {
	option := WithService("test-service")
	cfg := Config{}
	option(&cfg)
	assert.NotNil(t, cfg.GlobalFields)
	assert.Equal(t, "test-service", cfg.GlobalFields["service"])
}

func TestWithService_ExistingGlobalFields(t *testing.T) {
	option := WithService("new-service")
	cfg := Config{
		GlobalFields: map[string]string{
			"existing": "value",
		},
	}
	option(&cfg)
	assert.Equal(t, "new-service", cfg.GlobalFields["service"])
	assert.Equal(t, "value", cfg.GlobalFields["existing"])
}

func TestHelpers_Integration(t *testing.T) {
	// Test all helpers together
	logger := NewLogger(
		WithLevel("debug"),
		WithEncoding("text"),
		WithService("integration-test"),
	)

	// Test logging with different attribute types
	err := errors.New("integration error")

	logger.Info("integration test",
		String("string_key", "string_value"),
		Int("int_key", 42),
		Int64("int64_key", 9223372036854775807),
		Bool("bool_key", true),
		Float64("float_key", 3.14159),
		Any("any_key", map[string]string{"nested": "value"}),
		Int32("int32_key", 2147483647),
		Strings("strings_key", []string{"one", "two", "three"}),
		Error(err),
	)

	// Test error value helper
	errorValue := ErrorValue(err)
	assert.Equal(t, "integration error", errorValue)
}

func TestHelpers_EdgeCases(t *testing.T) {
	// Test with zero values
	assert.Equal(t, "key", String("key", "").Key)
	assert.Equal(t, "", String("key", "").Value.String())

	assert.Equal(t, "count", Int("count", 0).Key)
	assert.Equal(t, int64(0), Int("count", 0).Value.Int64())

	assert.Equal(t, "enabled", Bool("enabled", false).Key)
	assert.Equal(t, false, Bool("enabled", false).Value.Bool())

	assert.Equal(t, "pi", Float64("pi", 0.0).Key)
	assert.Equal(t, 0.0, Float64("pi", 0.0).Value.Float64())

	// Test with nil error
	attr := Error(nil)
	assert.Equal(t, "error", attr.Key)
	assert.Equal(t, "<nil>", attr.Value.String())

	errorValue := ErrorValue(nil)
	assert.Equal(t, "<nil>", errorValue)

	// Test with empty strings slice
	attr = Strings("empty", []string{})
	assert.Equal(t, "empty", attr.Key)
	assert.Equal(t, "[]", attr.Value.String())
}

func TestHelpers_TypeConversions(t *testing.T) {
	// Test Int32 with negative value
	attr := Int32("negative", -123)
	assert.Equal(t, "negative", attr.Key)
	assert.Equal(t, int64(-123), attr.Value.Int64())

	// Test Int32 with zero
	attr = Int32("zero", 0)
	assert.Equal(t, "zero", attr.Key)
	assert.Equal(t, int64(0), attr.Value.Int64())

	// Test Int32 with max int32
	attr = Int32("max", 2147483647)
	assert.Equal(t, "max", attr.Key)
	assert.Equal(t, int64(2147483647), attr.Value.Int64())
}

func TestHelpers_ComplexTypes(t *testing.T) {
	// Test Any with complex nested structure
	type Address struct {
		Street  string
		City    string
		Country string
	}

	type User struct {
		Name    string
		Age     int
		Address Address
		Active  bool
	}

	user := User{
		Name: "John Doe",
		Age:  30,
		Address: Address{
			Street:  "123 Main St",
			City:    "New York",
			Country: "USA",
		},
		Active: true,
	}

	attr := Any("user", user)
	assert.Equal(t, "user", attr.Key)
	assert.Contains(t, attr.Value.String(), "John Doe")
	assert.Contains(t, attr.Value.String(), "30")
	assert.Contains(t, attr.Value.String(), "true")
}
