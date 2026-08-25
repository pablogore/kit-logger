package httpmw

import (
	kitlog "github.com/pablogore/kit-logger/pkg/logger"

	"net/http"
	"time"

	"github.com/getsyntegrity/kit-core/idgen"
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

			// Wrap ResponseWriter to capture status code
			rr := &responseRecorder{ResponseWriter: w, status: 200}
			r = r.WithContext(r.Context()) // extendable to use logger.WithContext()

			next.ServeHTTP(rr, r)

			duration := time.Since(start)

			kitlog.L().InfoContext(r.Context(), "http_request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", rr.status,
				"duration_ms", duration.Milliseconds(),
				"request_id", requestID,
			)
		})
	}
}

// responseRecorder allows capturing the response status code.
type responseRecorder struct {
	http.ResponseWriter
	status int
}

// WriteHeader captures the status code and writes the header.
func (rw *responseRecorder) WriteHeader(code int) {
	rw.status = code
	rw.ResponseWriter.WriteHeader(code)
}
