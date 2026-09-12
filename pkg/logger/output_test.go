package logger

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// jsonKeys returns the top-level member names of one JSON object in the
// order they appear, keeping repeats, which is exactly what a map decode
// would hide. Duplicate members are the defect this file exists to catch.
func jsonKeys(t *testing.T, line string) []string {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(line))
	tok, err := dec.Token()
	require.NoError(t, err)
	require.Equal(t, json.Delim('{'), tok)

	var keys []string
	depth := 0
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			return keys
		}
		require.NoError(t, err)
		switch v := tok.(type) {
		case json.Delim:
			switch v {
			case '{', '[':
				depth++
			case '}', ']':
				depth--
			}
		case string:
			if depth == 0 && dec.More() {
				keys = append(keys, v)
				// Skip the value so a string value is not mistaken for a key.
				var skip json.RawMessage
				require.NoError(t, dec.Decode(&skip))
			}
		}
	}
}

func lines(buf *bytes.Buffer) []string {
	return strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n")
}

type ctxKey string

// TestOutput_NoDuplicateKeysAcrossWithContextAndRecord is the readability
// contract: the ergonomic paths -- With, a context extractor, the *Context
// methods and the call site -- may all mention the same key, and the line
// still carries it once, with the most specific value.
func TestOutput_NoDuplicateKeysAcrossWithContextAndRecord(t *testing.T) {
	var buf bytes.Buffer
	log := New(Config{
		Format: FormatJSON,
		Writer: &buf,
		ContextFields: func(ctx context.Context) []any {
			if v, ok := ctx.Value(ctxKey("request_id")).(string); ok {
				return []any{"request_id", v, "tenant", "acme"}
			}
			return nil
		},
	})
	ctx := context.WithValue(context.Background(), ctxKey("request_id"), "req-from-ctx")

	log.With("request_id", "req-from-with", "user_id", 42).
		WithContext(ctx).
		InfoContext(ctx, "order created", "order_id", "ord-1", "error", "first", "error", "second")

	got := lines(&buf)
	require.Len(t, got, 1)
	assert.Equal(t, []string{"time", "level", "msg", "request_id", "user_id", "tenant", "order_id", "error"},
		jsonKeys(t, got[0]))

	var obj map[string]any
	require.NoError(t, json.Unmarshal([]byte(got[0]), &obj))
	assert.Equal(t, "req-from-ctx", obj["request_id"], "the value learned deepest in the call wins")
	assert.Equal(t, "second", obj["error"])
}

func TestOutput_GlobalFieldsComeFirstAndCannotBeOverridden(t *testing.T) {
	var buf bytes.Buffer
	log := New(Config{
		Format:       FormatJSON,
		Writer:       &buf,
		GlobalFields: map[string]string{"service": "orders", "env": "prod"},
	})

	log.With("service", "from-with").Info("config reloaded", "env", "canary", "k", "v")

	got := lines(&buf)
	require.Len(t, got, 1)
	assert.Equal(t, []string{"time", "level", "msg", "env", "service", "k"}, jsonKeys(t, got[0]),
		"global fields are the first thing after msg, in sorted key order")
	var obj map[string]any
	require.NoError(t, json.Unmarshal([]byte(got[0]), &obj))
	assert.Equal(t, "prod", obj["env"])
	assert.Equal(t, "orders", obj["service"])
}

func TestOutput_UserComponentKeyIsNotHijacked(t *testing.T) {
	var buf bytes.Buffer
	log := New(Config{Format: FormatJSON, Writer: &buf, AddSource: true})

	log.With("component", "orders").Info("hi")

	got := lines(&buf)
	require.Len(t, got, 1)
	assert.Equal(t, []string{"time", "level", "msg", "component", "source"}, jsonKeys(t, got[0]))
	var obj map[string]any
	require.NoError(t, json.Unmarshal([]byte(got[0]), &obj))
	assert.Equal(t, "orders", obj["component"])
	src, ok := obj["source"].(map[string]any)
	require.True(t, ok, "source is a group in slog's AddSource shape")
	assert.Equal(t, "output_test.go", src["file"])
	assert.Contains(t, src["function"], "TestOutput_UserComponentKeyIsNotHijacked")
	assert.NotContains(t, src["function"], "kit-logger", "no module path in the function name")
}

// TestOutput_AddSourceWinsOverAGlobalSourceField pins the one exception to
// "global fields win": a pinned GlobalFields["source"] would otherwise
// suppress the very attribution AddSource asked for, and the line would carry
// a string where a shipper expects the group.
func TestOutput_AddSourceWinsOverAGlobalSourceField(t *testing.T) {
	var buf bytes.Buffer
	log, err := NewWithError(Config{
		Format:       FormatJSON,
		Writer:       &buf,
		AddSource:    true,
		GlobalFields: map[string]string{"source": "external", "service": "orders"},
	})
	require.Error(t, err, "a global field that can never reach the line is a misconfiguration worth reporting")
	assert.Contains(t, err.Error(), `GlobalFields["source"] is ignored when AddSource is set`)

	log.Info("hi")

	got := lines(&buf)
	require.Len(t, got, 1)
	assert.Equal(t, []string{"time", "level", "msg", "service", "source"}, jsonKeys(t, got[0]),
		"exactly one source key, and it is the pipeline's")
	var obj map[string]any
	require.NoError(t, json.Unmarshal([]byte(got[0]), &obj))
	src, ok := obj["source"].(map[string]any)
	require.True(t, ok, "source must be the {function,file,line} group, not the global string")
	assert.Equal(t, "output_test.go", src["file"])
	assert.Contains(t, src["function"], "TestOutput_AddSourceWinsOverAGlobalSourceField")
}

// TestOutput_GlobalSourceFieldIsHonouredWithoutAddSource is the other half:
// with attribution off, "source" is an ordinary key and a global field may
// claim it like any other.
func TestOutput_GlobalSourceFieldIsHonouredWithoutAddSource(t *testing.T) {
	var buf bytes.Buffer
	log, err := NewWithError(Config{
		Format:       FormatJSON,
		Writer:       &buf,
		GlobalFields: map[string]string{"source": "external"},
	})
	require.NoError(t, err)

	log.Info("hi", "source", "call-site")

	got := lines(&buf)
	require.Len(t, got, 1)
	assert.Equal(t, []string{"time", "level", "msg", "source"}, jsonKeys(t, got[0]))
	assert.Contains(t, got[0], `"source":"external"`)
}

func TestOutput_SourceIsOffByDefault(t *testing.T) {
	var buf bytes.Buffer
	New(Config{Format: FormatJSON, Writer: &buf}).Info("hi", "k", "v")

	assert.Equal(t, []string{"time", "level", "msg", "k"}, jsonKeys(t, lines(&buf)[0]))
}

func TestOutput_DurationIsReadableInJSONAndText(t *testing.T) {
	var jsonBuf, textBuf bytes.Buffer
	New(Config{Format: FormatJSON, Writer: &jsonBuf}).Info("slow", "took", 1500*time.Millisecond, "duration_ms", int64(1500))
	New(Config{Format: FormatText, Writer: &textBuf}).Info("slow", "took", 1500*time.Millisecond)

	assert.Contains(t, jsonBuf.String(), `"took":"1.5s"`, "JSON renders a Duration like text does, not as nanoseconds")
	assert.Contains(t, jsonBuf.String(), `"duration_ms":1500`, "a value with the unit in its key stays numeric")
	assert.Contains(t, textBuf.String(), "took=1.5s")
}

func TestOutput_InitializationLineDoesNotShadowLevel(t *testing.T) {
	var buf bytes.Buffer
	New(Config{Format: FormatJSON, Writer: &buf, Level: LevelDebug, LogInitialization: true})

	got := lines(&buf)
	require.Len(t, got, 1)
	assert.Equal(t, []string{"time", "level", "msg", "config"}, jsonKeys(t, got[0]))
	assert.Contains(t, got[0], `"config":{"level":"debug","format":"json"}`)
}

func TestOutput_ReservedKeysAreNeverDuplicated(t *testing.T) {
	var buf bytes.Buffer
	New(Config{Format: FormatJSON, Writer: &buf}).Info("hi", "level", "shadow", "msg", "shadow", "time", "shadow")

	got := lines(&buf)
	require.Len(t, got, 1)
	assert.Equal(t, []string{"time", "level", "msg", "attr.level", "attr.msg", "attr.time"}, jsonKeys(t, got[0]))
}

func TestOutput_ConsoleFormat(t *testing.T) {
	var buf bytes.Buffer
	log := New(Config{
		Format:       FormatConsole,
		Writer:       &buf,
		GlobalFields: map[string]string{"service": "orders"},
		AddSource:    true,
	})

	log.With("request_id", "req-1").Error("payment failed", "error", errors.New("card declined"), "request_id", "req-2")

	got := lines(&buf)
	require.Len(t, got, 1)
	line := got[0]
	assert.Regexp(t, `^\d{2}:\d{2}:\d{2}\.\d{3} ERR payment failed service=orders request_id=req-2 error="card declined" source=output_test.go:\d+$`, line)
}

func TestOutput_ConsoleFormatParsesAndPrints(t *testing.T) {
	f, err := ParseFormat("console")
	require.NoError(t, err)
	assert.Equal(t, FormatConsole, f)
	assert.Equal(t, "console", FormatConsole.String())
	assert.NoError(t, Config{Format: FormatConsole}.Validate())
	assert.Error(t, Config{Format: FormatConsole + 1}.Validate())
}

func TestOutput_SinkNeverSeesWithAttrs(t *testing.T) {
	cap := newCapturingHandler()
	log := New(Config{Sink: cap})

	log.With("a", 1).Info("hi", "b", 2)

	entries := cap.getEntries()
	require.Len(t, entries, 1)
	a, ok := argValue(entries[0].Args, "a")
	require.True(t, ok, "With attrs are materialized into the record the sink receives")
	assert.Equal(t, int64(1), a)
}
