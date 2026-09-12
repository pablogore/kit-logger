package httpmw_test

import (
	"context"
	"net/http"
	"net/http/httptest"

	"github.com/pablogore/kit-logger/pkg/logger"
	"github.com/pablogore/kit-logger/pkg/logger/httpmw"
)

// handler mirrors the README's request-ID snippet: the ID the middleware put
// in the request context is available to the wrapped handler.
func handler(w http.ResponseWriter, r *http.Request) {
	id, ok := httpmw.RequestIDFrom(r.Context())
	if ok {
		w.Header().Set("X-Handled-Request-Id", id)
	}
}

// Example mirrors the README's HTTP middleware section end to end: the
// request ID is wired into Config.ContextFields so every log line in the
// request carries it, the middleware is built from Options, and a request
// is served through it. No Output comment, since the JSON line carries a
// timestamp and a generated ID.
func Example() {
	log := logger.New(logger.Config{
		ContextFields: func(ctx context.Context) []any {
			if id, ok := httpmw.RequestIDFrom(ctx); ok {
				return []any{"request_id", id}
			}
			return nil
		},
	})

	mw := httpmw.New(httpmw.Options{
		Logger:    log,                  // nil falls back to logger.L()
		SkipPaths: []string{"/healthz"}, // served and un-logged
	})

	var next http.Handler = http.HandlerFunc(handler)
	wrapped := mw(next)

	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/orders", nil))
}
