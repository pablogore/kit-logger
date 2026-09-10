// Package httpmw provides an HTTP middleware that logs each request.
package httpmw

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/getsyntegrity/kit-core/idgen"

	kitlog "github.com/pablogore/kit-logger/pkg/logger"
)

// Middleware returns an HTTP middleware that logs each request.
func Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			// Request ID for traceability
			requestID := r.Header.Get("X-Request-Id")
			if requestID == "" {
				requestID = idgen.MustNewULID()
			}

			rr := &responseRecorder{ResponseWriter: w}

			next.ServeHTTP(rr, r)

			duration := time.Since(start)

			kitlog.L().InfoContext(r.Context(), "http_request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", rr.statusCode(),
				"duration_ms", duration.Milliseconds(),
				"bytes_written", rr.bytes,
				"request_id", requestID,
			)
		})
	}
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
