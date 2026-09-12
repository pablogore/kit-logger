package logger

import (
	"log/slog"
)

// String creates a string attribute for logging
func String(key, value string) slog.Attr {
	return slog.String(key, value)
}

// Error creates an error attribute for logging
func Error(err error) slog.Attr {
	if err == nil {
		return slog.String("error", "<nil>")
	}
	return slog.String("error", err.Error())
}

// ErrorValue creates an error value for logging
func ErrorValue(err error) any {
	if err == nil {
		return "<nil>"
	}
	return err.Error()
}

// Int creates an integer attribute for logging
func Int(key string, value int) slog.Attr {
	return slog.Int(key, value)
}

// Int64 creates an int64 attribute for logging
func Int64(key string, value int64) slog.Attr {
	return slog.Int64(key, value)
}

// Bool creates a boolean attribute for logging
func Bool(key string, value bool) slog.Attr {
	return slog.Bool(key, value)
}

// Float64 creates a float64 attribute for logging
func Float64(key string, value float64) slog.Attr {
	return slog.Float64(key, value)
}

// Any creates an any attribute for logging
func Any(key string, value any) slog.Attr {
	return slog.Any(key, value)
}

// Int32 creates an int32 attribute for logging
func Int32(key string, value int32) slog.Attr {
	return slog.Int(key, int(value))
}

// Strings creates a strings attribute for logging
func Strings(key string, values []string) slog.Attr {
	return slog.Any(key, values)
}

// NewLogger creates a new logger with default configuration
func NewLogger(options ...func(*Config)) Logger {
	cfg := Config{}
	for _, option := range options {
		option(&cfg)
	}
	return New(cfg)
}

// WithLevel sets the log level by name, delegating to ParseLevel through
// Config's deprecated LevelString bridge -- a typo surfaces as a Validate
// error instead of silently falling back to info.
func WithLevel(level string) func(*Config) {
	return func(cfg *Config) {
		cfg.LevelString = level
	}
}

// WithEncoding sets the log encoding by name, delegating to ParseFormat
// through Config's deprecated FormatString bridge -- see WithLevel.
func WithEncoding(encoding string) func(*Config) {
	return func(cfg *Config) {
		cfg.FormatString = encoding
	}
}

// WithService sets the service name
func WithService(service string) func(*Config) {
	return func(cfg *Config) {
		if cfg.GlobalFields == nil {
			cfg.GlobalFields = make(map[string]string)
		}
		cfg.GlobalFields["service"] = service
	}
}
