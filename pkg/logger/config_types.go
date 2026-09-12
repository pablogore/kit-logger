package logger

import (
	"fmt"
	"log/slog"
	"strings"
)

// Level is kit-logger's typed severity level for Config and SamplingConfig.
//
// Its values map exactly onto slog.Level's own scale (LevelDebug ==
// Level(slog.LevelDebug), and so on), so it converts to and from slog.Level
// with a plain type conversion and preserves any custom offset a caller
// already relies on -- e.g. a Sampling.MinLevel that sits between two named
// levels. The zero value is LevelInfo, matching slog's own "info is free"
// convention, so a zero-value Config keeps behaving like it always did.
type Level int8

const (
	LevelDebug Level = Level(slog.LevelDebug)
	LevelInfo  Level = Level(slog.LevelInfo) // zero value: the safe default.
	LevelWarn  Level = Level(slog.LevelWarn)
	LevelError Level = Level(slog.LevelError)
)

// String returns the canonical lowercase name ParseLevel accepts for l, or
// "level(N)" for a value with no name of its own (e.g. one built by adding an
// offset to a named constant).
func (l Level) String() string {
	switch l {
	case LevelDebug:
		return "debug"
	case LevelInfo:
		return "info"
	case LevelWarn:
		return "warn"
	case LevelError:
		return "error"
	default:
		return fmt.Sprintf("level(%d)", int8(l))
	}
}

// slogLevel converts l to the slog.Level it was built from.
func (l Level) slogLevel() slog.Level { return slog.Level(l) }

// ParseLevel parses a level name, returning an error for anything it does
// not recognize. The empty string is not an error: it returns LevelInfo, the
// explicit "unset" default, with a nil error -- unlike an unrecognized name,
// which is never silently coerced into a level.
func ParseLevel(s string) (Level, error) {
	switch strings.ToLower(s) {
	case "":
		return LevelInfo, nil
	case "debug":
		return LevelDebug, nil
	case "info":
		return LevelInfo, nil
	case "warn", "warning":
		return LevelWarn, nil
	case "error":
		return LevelError, nil
	default:
		return LevelInfo, fmt.Errorf("logger: invalid level %q", s)
	}
}

// UnmarshalText implements encoding.TextUnmarshaler, so Level decodes from
// JSON and YAML with the same rules ParseLevel applies.
func (l *Level) UnmarshalText(b []byte) error {
	parsed, err := ParseLevel(string(b))
	if err != nil {
		return err
	}
	*l = parsed
	return nil
}

// MarshalText implements encoding.TextMarshaler.
func (l Level) MarshalText() ([]byte, error) {
	return []byte(l.String()), nil
}

// Format is the terminal encoding New's default handler writes.
type Format uint8

const (
	FormatText Format = iota // zero value: the safe default.
	FormatJSON
	// FormatConsole is a human-first layout for development: short local
	// time, a colored level, the message, then key=value pairs. Not for
	// production shippers, which want FormatJSON. See handler.ConsoleHandler.
	FormatConsole
)

// String returns the canonical lowercase name ParseFormat accepts for f, or
// "format(N)" for a value with no name of its own (e.g. an out-of-range
// Format that failed Validate but was constructed anyway).
func (f Format) String() string {
	switch f {
	case FormatText:
		return "text"
	case FormatJSON:
		return "json"
	case FormatConsole:
		return "console"
	default:
		return fmt.Sprintf("format(%d)", uint8(f))
	}
}

// ParseFormat parses a format name case-insensitively, returning an error for
// anything it does not recognize. The empty string returns FormatText with a
// nil error -- the explicit "unset" default.
func ParseFormat(s string) (Format, error) {
	switch strings.ToLower(s) {
	case "", "text":
		return FormatText, nil
	case "json":
		return FormatJSON, nil
	case "console":
		return FormatConsole, nil
	default:
		return FormatText, fmt.Errorf("logger: invalid format %q", s)
	}
}

// UnmarshalText implements encoding.TextUnmarshaler, so Format decodes from
// JSON and YAML with the same rules ParseFormat applies.
func (f *Format) UnmarshalText(b []byte) error {
	parsed, err := ParseFormat(string(b))
	if err != nil {
		return err
	}
	*f = parsed
	return nil
}

// MarshalText implements encoding.TextMarshaler.
func (f Format) MarshalText() ([]byte, error) {
	return []byte(f.String()), nil
}

// resolveLevel reconciles a typed Level with its deprecated string bridge.
//
// An empty legacy string is a no-op: typed is returned unchanged. A
// non-empty legacy string that fails to parse is always an error, regardless
// of typed.
//
// Conflict detection between typed and legacy is only possible when typed
// holds a non-zero value. A plain Go field cannot distinguish "the caller
// left this at its zero value" from "the caller explicitly chose the zero
// value" -- Config{LevelString: "debug"} and Config{Level: LevelInfo,
// LevelString: "debug"} are the exact same struct value. So when typed ==
// LevelInfo, presence cannot be determined: the parsed legacy value is used
// as the effective result even if it differs from LevelInfo, and no
// conflict is reported. That is a deliberate, documented limitation of this
// bridge, not a best-effort guarantee that every contradiction is caught --
// see TestConfig_ZeroValueLevelCannotDetectLegacyConflict. When typed is not
// LevelInfo, it can only have gotten there through an explicit assignment,
// so presence is unambiguous: the two must then agree, or resolveLevel
// reports the conflict rather than picking one silently.
func resolveLevel(label string, typed Level, legacy string) (Level, error) {
	if legacy == "" {
		return typed, nil
	}
	parsed, err := ParseLevel(legacy)
	if err != nil {
		return typed, fmt.Errorf("Config: %sString: %w", label, err)
	}
	if typed != LevelInfo && typed != parsed {
		return typed, fmt.Errorf("Config: %s (%s) and %sString (%q) disagree", label, typed, label, legacy)
	}
	return parsed, nil
}

// resolveFormat is resolveLevel's counterpart for Format/FormatString: the
// same zero-value presence limitation applies, with FormatText standing in
// for LevelInfo -- see TestConfig_ZeroValueFormatCannotDetectLegacyConflict.
func resolveFormat(label string, typed Format, legacy string) (Format, error) {
	if legacy == "" {
		return typed, nil
	}
	parsed, err := ParseFormat(legacy)
	if err != nil {
		return typed, fmt.Errorf("Config: %sString: %w", label, err)
	}
	if typed != FormatText && typed != parsed {
		return typed, fmt.Errorf("Config: %s (%s) and %sString (%q) disagree", label, typed, label, legacy)
	}
	return parsed, nil
}
