package handler

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"unicode"
)

// ConsoleHandler is a terminal handler for humans: a development layout
// that puts what a reader scans for first and keeps the rest quiet.
//
//	15:04:05.000 INF order created  service=orders request_id=req-123 order_id=ord-1
//	15:04:05.001 ERR payment failed  service=orders error="card declined"
//
// The layout, left to right: local wall-clock time without the date (the
// date is the same on every line of a development session), a three-letter
// level, the message, then key=value pairs with groups flattened to dotted
// keys, as slog's text handler does. When writing to a terminal, time and
// keys are dimmed, levels are colored by severity and error values are red,
// so the eye lands on the message and on what went wrong. A "source" group
// (see SourceHandler) is rendered as source=file.go:42 at the end of the
// line.
//
// It is not a production format: it has no date, no machine-stable level
// names and no escaping beyond quoting. Ship FormatJSON in production.
//
// ConsoleHandler is safe for concurrent use. Each Handle call writes one
// line with a single Write, so lines from different goroutines never
// interleave.
type ConsoleHandler struct {
	w     io.Writer
	mu    *sync.Mutex
	level slog.Leveler
	color bool
	tfmt  string
	ops   []dedupOp
}

// ConsoleOptions configures NewConsoleHandler. The zero value is usable.
type ConsoleOptions struct {
	// Level is the minimum level to emit. nil means slog.LevelInfo.
	Level slog.Leveler

	// TimeFormat is the time layout, in time.Format syntax. Empty means
	// "15:04:05.000".
	TimeFormat string

	// NoColor disables ANSI colors even when the writer is a terminal.
	NoColor bool

	// ForceColor enables ANSI colors even when the writer is not a terminal,
	// for a pager or a CI log viewer that renders them. NoColor wins.
	ForceColor bool
}

// NewConsoleHandler returns a ConsoleHandler writing to w. Colors are on
// when w is a terminal, unless opts says otherwise.
func NewConsoleHandler(w io.Writer, opts ConsoleOptions) *ConsoleHandler {
	level := opts.Level
	if level == nil {
		level = slog.LevelInfo
	}
	tfmt := opts.TimeFormat
	if tfmt == "" {
		tfmt = "15:04:05.000"
	}
	color := opts.ForceColor || isTerminal(w)
	if opts.NoColor {
		color = false
	}
	return &ConsoleHandler{w: w, mu: &sync.Mutex{}, level: level, color: color, tfmt: tfmt}
}

// isTerminal reports whether w is a character device, which is what an
// interactive terminal is and a pipe or a file is not.
func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

// Enabled reports whether level is at or above the configured minimum.
func (h *ConsoleHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level.Level()
}

// WithAttrs returns a handler that prefixes every record with attrs.
func (h *ConsoleHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	attrs = filterValidAttrs(attrs)
	if len(attrs) == 0 {
		return h
	}
	return h.derive(dedupOp{attrs: slices.Clone(attrs)})
}

// WithGroup returns a handler that nests subsequent attrs under name.
func (h *ConsoleHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	return h.derive(dedupOp{isGroup: true, group: name})
}

func (h *ConsoleHandler) derive(op dedupOp) *ConsoleHandler {
	c := *h
	c.ops = append(slices.Clip(h.ops), op)
	return &c
}

const (
	ansiReset  = "\x1b[0m"
	ansiDim    = "\x1b[2m"
	ansiBold   = "\x1b[1m"
	ansiRed    = "\x1b[31m"
	ansiYellow = "\x1b[33m"
	ansiGreen  = "\x1b[32m"
	ansiCyan   = "\x1b[36m"
)

// Handle renders record as one line and writes it.
func (h *ConsoleHandler) Handle(_ context.Context, record slog.Record) error {
	buf := make([]byte, 0, 256)

	if !record.Time.IsZero() {
		buf = h.paint(buf, ansiDim, record.Time.Format(h.tfmt))
		buf = append(buf, ' ')
	}
	buf = h.paint(buf, levelColor(record.Level), levelLabel(record.Level))
	buf = append(buf, ' ')
	buf = h.paint(buf, ansiBold, record.Message)

	var source []byte
	prefix := ""
	for _, op := range h.ops {
		if op.isGroup {
			prefix += op.group + "."
			continue
		}
		for _, a := range op.attrs {
			buf, source = h.appendAttr(buf, source, prefix, a)
		}
	}
	record.Attrs(func(a slog.Attr) bool {
		buf, source = h.appendAttr(buf, source, prefix, a)
		return true
	})
	buf = append(buf, source...)
	buf = append(buf, '\n')

	h.mu.Lock()
	defer h.mu.Unlock()
	_, err := h.w.Write(buf)
	return err
}

// appendAttr renders a under prefix. A top-level "source" group is rendered
// into the separate source buffer so it lands at the end of the line.
func (h *ConsoleHandler) appendAttr(buf, source []byte, prefix string, a slog.Attr) ([]byte, []byte) {
	a.Value = a.Value.Resolve()
	if a.Value.Kind() == slog.KindGroup {
		members := a.Value.Group()
		if len(members) == 0 {
			return buf, source
		}
		if prefix == "" && a.Key == slog.SourceKey {
			if loc, ok := sourceLocation(members); ok {
				source = append(source, ' ')
				source = h.paint(source, ansiDim, "source="+loc)
				return buf, source
			}
		}
		p := prefix
		if a.Key != "" {
			p += a.Key + "."
		}
		for _, m := range members {
			buf, source = h.appendAttr(buf, source, p, m)
		}
		return buf, source
	}
	if a.Equal(slog.Attr{}) {
		return buf, source
	}
	buf = append(buf, ' ')
	buf = h.paint(buf, ansiDim, prefix+a.Key+"=")
	val := quoteIfNeeded(a.Value.String())
	if a.Key == "error" || a.Key == "err" {
		buf = h.paint(buf, ansiRed, val)
	} else {
		buf = append(buf, val...)
	}
	return buf, source
}

// sourceLocation renders the members of a source group as file:line.
func sourceLocation(members []slog.Attr) (string, bool) {
	var file, line string
	for _, m := range members {
		switch m.Key {
		case "file":
			file = filepath.Base(m.Value.String())
		case "line":
			line = m.Value.String()
		}
	}
	if file == "" {
		return "", false
	}
	if line == "" {
		return file, true
	}
	return file + ":" + line, true
}

// paint appends s to buf wrapped in color when colors are on.
func (h *ConsoleHandler) paint(buf []byte, color, s string) []byte {
	if !h.color {
		return append(buf, s...)
	}
	buf = append(buf, color...)
	buf = append(buf, s...)
	return append(buf, ansiReset...)
}

// levelLabel is the three-letter label for the four standard levels, and
// slog's own name for anything in between or beyond.
func levelLabel(l slog.Level) string {
	switch l {
	case slog.LevelDebug:
		return "DBG"
	case slog.LevelInfo:
		return "INF"
	case slog.LevelWarn:
		return "WRN"
	case slog.LevelError:
		return "ERR"
	}
	return l.String()
}

func levelColor(l slog.Level) string {
	switch {
	case l >= slog.LevelError:
		return ansiRed
	case l >= slog.LevelWarn:
		return ansiYellow
	case l >= slog.LevelInfo:
		return ansiGreen
	}
	return ansiCyan
}

// quoteIfNeeded quotes s the way logfmt readers expect: when it is empty or
// contains whitespace, a quote, an equals sign or a control character.
func quoteIfNeeded(s string) string {
	if s == "" {
		return `""`
	}
	if strings.ContainsFunc(s, func(r rune) bool {
		return unicode.IsSpace(r) || r == '"' || r == '=' || unicode.IsControl(r)
	}) {
		return strconv.Quote(s)
	}
	return s
}
