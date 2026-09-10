// Package httpmw provides an HTTP middleware that logs one line per request.
//
// It observes; it never alters. The response writer it hands the next handler
// preserves the optional interfaces the standard library defines, panics are
// logged and re-panicked rather than swallowed, and nothing here changes a
// status code or a body.
package httpmw

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/getsyntegrity/kit-core/idgen"

	kitlog "github.com/pablogore/kit-logger/pkg/logger"
)

// DefaultRequestIDHeader is the header read for an inbound request ID and
// written back on the response.
const DefaultRequestIDHeader = "X-Request-Id"

// maxRequestIDLen bounds an inbound request ID.
//
// The header is attacker-controlled and its value goes straight into the log
// stream, so an oversized or non-printable value is replaced with a generated
// one rather than echoed. Without the bound, a client could put a megabyte of
// arbitrary bytes into every log line of a service.
const maxRequestIDLen = 200

// Options configures New. The zero value is usable: it logs to the process-wide
// logger, reads and echoes X-Request-Id, generates an ID when none arrives, and
// picks the level from the status.
type Options struct {
	// Logger receives the request lines. When nil, the process-wide logger is
	// resolved once, at construction -- not per request.
	Logger kitlog.Logger

	// RequestIDHeader is the header read for an inbound ID and written back.
	// Empty means DefaultRequestIDHeader.
	RequestIDHeader string

	// DisableResponseIDHeader stops the middleware writing the ID back on the
	// response. The ID is still put in the context and still logged.
	//
	// Phrased as an opt-out because the useful behaviour is the default, and a
	// bool field documented as "defaults to true" is a field whose zero value
	// lies about itself.
	DisableResponseIDHeader bool

	// IDGen generates a request ID when none arrives. Empty means a
	// non-panicking ULID generator. A generator that fails or panics does not
	// fail the request: it is served and logged with an empty request_id.
	IDGen func() (string, error)

	// LevelFor maps the response status to a level. Empty means 5xx to Error,
	// 4xx to Warn, everything else to Info. A recovered panic is always logged
	// at Error and does not consult this function.
	LevelFor func(status int) slog.Level

	// SkipPaths are request paths that produce no log line, matched exactly.
	// The request is still served, still carries a request ID, and still gets
	// the response header.
	SkipPaths []string
}

// requestIDKey is the context key for the request ID. It is an unexported
// struct type so no other package can collide with it or forge a value.
type requestIDKey struct{}

// RequestIDFrom returns the request ID the middleware attached to ctx.
//
// The second result reports whether one was found, which is what distinguishes
// "no middleware in this chain" from "the ID is the empty string because
// generation failed".
func RequestIDFrom(ctx context.Context) (string, bool) {
	if ctx == nil {
		return "", false
	}
	id, ok := ctx.Value(requestIDKey{}).(string)
	return id, ok
}

// Middleware returns an HTTP middleware that logs each request to the
// process-wide logger.
//
// Deprecated: use New, which takes an explicit logger. This is New(Options{})
// and remains for source compatibility.
func Middleware() func(http.Handler) http.Handler {
	return New(Options{})
}

// New returns an HTTP middleware that logs one line per request.
func New(opts Options) func(http.Handler) http.Handler {
	header := opts.RequestIDHeader
	if header == "" {
		header = DefaultRequestIDHeader
	}

	idGen := opts.IDGen
	if idGen == nil {
		idGen = newULID
	}

	levelFor := opts.LevelFor
	if levelFor == nil {
		levelFor = defaultLevelFor
	}

	skip := make(map[string]struct{}, len(opts.SkipPaths))
	for _, p := range opts.SkipPaths {
		skip[p] = struct{}{}
	}

	// The logger is resolved once rather than per request. A middleware built
	// with an explicit logger must never reach for the global, and one built
	// without one should not pay for the lookup on every request.
	log := opts.Logger

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			requestID := resolveRequestID(r.Header.Get(header), idGen)
			if !opts.DisableResponseIDHeader && requestID != "" {
				// Before the handler runs: a header set after WriteHeader never
				// reaches the client.
				w.Header().Set(header, requestID)
			}

			ctx := context.WithValue(r.Context(), requestIDKey{}, requestID)
			r = r.WithContext(ctx)

			rr := &responseRecorder{ResponseWriter: w}

			_, skipped := skip[r.URL.Path]

			// The whole tail runs from a defer so that a panicking handler
			// produces exactly the same line a completed one does, plus the
			// panic fields -- and produces it exactly once.
			defer func() {
				if skipped {
					// No recover(): a panic keeps unwinding with its original
					// stack intact, which is exactly what "do not log this
					// path" should mean.
					return
				}

				rec := recover()

				status := rr.statusCode()
				if rec != nil && !rr.wroteHeader {
					// Nothing reached the client, and net/http will abandon the
					// connection. 500 is the outcome the caller experiences; a
					// logged 200 would be a lie about a request that failed
					// hardest. When a status *was* written, that status stands.
					status = http.StatusInternalServerError
				}

				fields := []any{
					"method", r.Method,
					"path", r.URL.Path,
					"status", status,
					"duration_ms", time.Since(start).Milliseconds(),
					"bytes_written", rr.bytes,
					"request_id", requestID,
				}
				if err := ctx.Err(); err != nil {
					fields = append(fields, "context_err", err.Error())
				}

				if rec == nil {
					logger(log).Log(ctx, levelFor(status), "http_request", fields...)
					return
				}

				fields = append(fields, "panic", panicValue(rec))
				if rec != http.ErrAbortHandler {
					// ErrAbortHandler is net/http's quiet abort signal, not a
					// defect: a stack trace for it is noise.
					fields = append(fields, "stack", string(debug.Stack()))
				}
				logger(log).Log(ctx, slog.LevelError, "http_request", fields...)

				// Re-panic: a logging middleware observes the failure path, it
				// does not change it. The original stack is already captured in
				// the line above, because this new panic loses it.
				panic(rec)
			}()

			next.ServeHTTP(rr, r)
		})
	}
}

// logger resolves the logger for one line, falling back to the process-wide one
// only when no logger was injected.
func logger(log kitlog.Logger) kitlog.Logger {
	if log != nil {
		return log
	}
	return kitlog.L()
}

// defaultLevelFor maps a status to a level: server errors are errors, client
// errors are warnings, everything else is informational.
func defaultLevelFor(status int) slog.Level {
	switch {
	case status >= http.StatusInternalServerError:
		return slog.LevelError
	case status >= http.StatusBadRequest:
		return slog.LevelWarn
	default:
		return slog.LevelInfo
	}
}

// newULID is the default ID generator. idgen panics when its entropy reader
// fails, so the panic is converted into an error here: an ID-generation failure
// must cost a log field, not a request.
func newULID() (id string, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			id, err = "", fmt.Errorf("httpmw: id generation panicked: %v", rec)
		}
	}()
	return idgen.NewULID(), nil
}

// resolveRequestID reuses a usable inbound ID and otherwise generates one.
//
// A generator that fails or panics yields an empty ID. The request is still
// served and still logged; only the correlation field is lost.
func resolveRequestID(inbound string, gen func() (string, error)) string {
	if usableRequestID(inbound) {
		return inbound
	}

	id, err := safeGen(gen)
	if err != nil {
		return ""
	}
	if !usableRequestID(id) {
		return ""
	}
	return id
}

// safeGen contains a panicking custom generator the same way newULID contains
// the default one.
func safeGen(gen func() (string, error)) (id string, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			id, err = "", fmt.Errorf("httpmw: id generation panicked: %v", rec)
		}
	}()
	return gen()
}

// usableRequestID reports whether a request ID can be put in a log line and a
// response header as-is: non-empty, bounded, and printable ASCII only.
//
// Control characters are rejected because a header value carrying a newline can
// forge log entries in any line-oriented format.
func usableRequestID(id string) bool {
	if id == "" || len(id) > maxRequestIDLen {
		return false
	}
	for i := 0; i < len(id); i++ {
		if id[i] < 0x20 || id[i] > 0x7e {
			return false
		}
	}
	return true
}

// panicValue renders a recovered value for the log field, preferring an error's
// message over its %v form.
func panicValue(rec any) string {
	if err, ok := rec.(error); ok {
		return err.Error()
	}
	return fmt.Sprint(rec)
}

// responseRecorder wraps an http.ResponseWriter to capture the status and the
// number of bytes written.
//
// It implements http.Flusher, http.Hijacker, http.Pusher and io.ReaderFrom
// explicitly, because embedding the http.ResponseWriter interface promotes only
// Header, Write and WriteHeader -- so a wrapper that embeds it destroys
// streaming, WebSocket upgrades, HTTP/2 push and the io.Copy fast path for
// every handler underneath.
//
// The trade-off, stated plainly: declaring those methods unconditionally means
// a downstream type assertion now always succeeds, even when the underlying
// writer does not support the operation. Flush then does nothing, Hijack and
// Push return http.ErrNotSupported, and ReadFrom falls back to io.Copy. The
// alternative -- generating the 16 permutations of wrapper types -- buys an
// honest assertion at a cost this package does not need to pay, because Unwrap
// is implemented and http.NewResponseController is the supported way to ask
// whether an operation is really available.
type responseRecorder struct {
	http.ResponseWriter

	status      int
	bytes       int64
	wroteHeader bool
}

var (
	_ http.Flusher  = (*responseRecorder)(nil)
	_ http.Hijacker = (*responseRecorder)(nil)
	_ http.Pusher   = (*responseRecorder)(nil)
	_ io.ReaderFrom = (*responseRecorder)(nil)
)

// statusCode is the status actually sent. A handler that writes nothing at all
// still produces a 200 on the wire, so that is what gets logged.
func (rw *responseRecorder) statusCode() int {
	if !rw.wroteHeader {
		return http.StatusOK
	}
	return rw.status
}

// WriteHeader records the first status and forwards it.
//
// Only the first call counts, matching net/http: a superfluous WriteHeader is
// ignored on the wire, and a recorder that took the last one would make the log
// disagree with what the client received.
func (rw *responseRecorder) WriteHeader(code int) {
	if rw.wroteHeader {
		return
	}
	rw.wroteHeader = true
	rw.status = code
	rw.ResponseWriter.WriteHeader(code)
}

// Write forwards the bytes and counts them, marking the implicit 200 that
// net/http would send.
func (rw *responseRecorder) Write(b []byte) (int, error) {
	if !rw.wroteHeader {
		rw.WriteHeader(http.StatusOK)
	}
	n, err := rw.ResponseWriter.Write(b)
	rw.bytes += int64(n)
	return n, err
}

// Flush forwards to the underlying writer when it can flush, and does nothing
// when it cannot.
func (rw *responseRecorder) Flush() {
	if f, ok := rw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Hijack forwards to the underlying writer, or reports that hijacking is
// unsupported.
func (rw *responseRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if h, ok := rw.ResponseWriter.(http.Hijacker); ok {
		return h.Hijack()
	}
	return nil, nil, fmt.Errorf("httpmw: underlying ResponseWriter is not an http.Hijacker: %w", http.ErrNotSupported)
}

// Push forwards to the underlying writer, or reports that push is unsupported.
func (rw *responseRecorder) Push(target string, opts *http.PushOptions) error {
	if p, ok := rw.ResponseWriter.(http.Pusher); ok {
		return p.Push(target, opts)
	}
	return http.ErrNotSupported
}

// ReadFrom uses the underlying writer's fast path when it has one and falls
// back to a plain copy otherwise. Either way the bytes are counted.
func (rw *responseRecorder) ReadFrom(src io.Reader) (int64, error) {
	if !rw.wroteHeader {
		rw.WriteHeader(http.StatusOK)
	}

	if rf, ok := rw.ResponseWriter.(io.ReaderFrom); ok {
		n, err := rf.ReadFrom(src)
		rw.bytes += n
		return n, err
	}

	// struct{ io.Writer } hides this type's own ReadFrom from io.Copy, which
	// would otherwise call straight back into here forever.
	n, err := io.Copy(struct{ io.Writer }{rw.ResponseWriter}, src)
	rw.bytes += n
	return n, err
}

// Unwrap returns the wrapped writer so http.NewResponseController can reach the
// real one and report honestly whether an operation is supported.
func (rw *responseRecorder) Unwrap() http.ResponseWriter { return rw.ResponseWriter }
