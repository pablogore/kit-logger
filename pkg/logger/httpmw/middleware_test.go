package httpmw_test

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	kitlog "github.com/pablogore/kit-logger/pkg/logger"
	"github.com/pablogore/kit-logger/pkg/logger/httpmw"
	"github.com/pablogore/kit-logger/pkg/logger/kitlogtest"
)

// entryFields turns a captured entry's variadic args into a map. The middleware
// logs key/value pairs, so an odd trailing arg is a bug in the middleware and
// the helper says so rather than silently dropping it.
func entryFields(t *testing.T, e kitlogtest.LogEntry) map[string]any {
	t.Helper()
	require.Zero(t, len(e.Args)%2, "log args must be key/value pairs, got %d", len(e.Args))

	fields := make(map[string]any, len(e.Args)/2)
	for i := 0; i < len(e.Args); i += 2 {
		key, ok := e.Args[i].(string)
		require.True(t, ok, "log key %v is not a string", e.Args[i])
		fields[key] = e.Args[i+1]
	}
	return fields
}

// onlyEntry returns the single log entry the middleware produced, failing if
// there is not exactly one. "Exactly one line per request" is a property worth
// asserting on its own.
func onlyEntry(t *testing.T, log *kitlogtest.MockLogger) kitlogtest.LogEntry {
	t.Helper()
	require.Len(t, log.Entries, 1, "expected exactly one log line per request")
	return log.Entries[0]
}

// serve runs one request through the middleware and returns the recorder.
func serve(h http.Handler, mw func(http.Handler) http.Handler, req *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	mw(h).ServeHTTP(w, req)
	return w
}

func handlerFunc(fn http.HandlerFunc) http.Handler { return fn }

// ---------------------------------------------------------------------------
// Optional interfaces
// ---------------------------------------------------------------------------

func TestNew_PreservesFlusher(t *testing.T) {
	log := kitlogtest.NewMockLogger()

	var (
		asserted bool
		flushed  bool
	)
	h := handlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		f, ok := w.(http.Flusher)
		asserted = ok
		if ok {
			w.Write([]byte("chunk"))
			f.Flush()
			flushed = true
		}
	})

	serve(h, httpmw.New(httpmw.Options{Logger: log}), httptest.NewRequest("GET", "/", nil))

	require.True(t, asserted, "http.Flusher must survive the wrapper")
	require.True(t, flushed)
}

func TestNew_ResponseControllerFlushWorksThroughTheWrapper(t *testing.T) {
	log := kitlogtest.NewMockLogger()

	// http.NewResponseController walks Unwrap(). This proves the wrapper
	// exposes it, which is the modern path for reaching optional behaviour.
	h := handlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		require.NoError(t, http.NewResponseController(w).Flush())
	})

	serve(h, httpmw.New(httpmw.Options{Logger: log}), httptest.NewRequest("GET", "/", nil))
}

func TestNew_ServerSentEventsStreamIncrementally(t *testing.T) {
	log := kitlogtest.NewMockLogger()

	release := make(chan struct{})
	h := handlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		f, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}
		fmt.Fprint(w, "data: first\n\n")
		f.Flush()
		<-release // the client must be able to read "first" before this returns
		fmt.Fprint(w, "data: second\n\n")
		f.Flush()
	})

	srv := httptest.NewServer(httpmw.New(httpmw.Options{Logger: log})(h))
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	require.NoError(t, err)
	defer resp.Body.Close()

	reader := bufio.NewReader(resp.Body)
	line, err := reader.ReadString('\n')
	require.NoError(t, err)
	require.Equal(t, "data: first\n", line, "the first event must arrive before the handler returns")

	close(release)
	rest, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.Contains(t, string(rest), "data: second")
}

func TestNew_ReverseProxyWorksBehindTheMiddleware(t *testing.T) {
	log := kitlogtest.NewMockLogger()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
		fmt.Fprint(w, "brewed")
	}))
	defer upstream.Close()

	target, err := url.Parse(upstream.URL)
	require.NoError(t, err)

	proxy := httptest.NewServer(httpmw.New(httpmw.Options{Logger: log})(httputil.NewSingleHostReverseProxy(target)))
	defer proxy.Close()

	resp, err := http.Get(proxy.URL)
	require.NoError(t, err)
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, http.StatusTeapot, resp.StatusCode)
	require.Equal(t, "brewed", string(body))
}

// ---------------------------------------------------------------------------
// Logger injection
// ---------------------------------------------------------------------------

func TestNew_LogsToTheInjectedLoggerAndNeverTouchesTheGlobal(t *testing.T) {
	// The global is poisoned: any call to kitlog.L() lands in this logger, and
	// the assertion below would catch it.
	poisoned := kitlogtest.NewMockLogger()
	kitlog.SetGlobal(poisoned)

	injected := kitlogtest.NewMockLogger()
	h := handlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })

	serve(h, httpmw.New(httpmw.Options{Logger: injected}), httptest.NewRequest("GET", "/", nil))

	require.Len(t, injected.Entries, 1)
	require.Empty(t, poisoned.Entries, "an injected logger must make the process-wide one unreachable")
}

func TestMiddleware_StillUsesTheGlobalLogger(t *testing.T) {
	global := kitlogtest.NewMockLogger()
	kitlog.SetGlobal(global)

	h := handlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })

	serve(h, httpmw.Middleware(), httptest.NewRequest("GET", "/test", nil))

	require.True(t, global.HasMessage("http_request"))
}

// ---------------------------------------------------------------------------
// Request ID
// ---------------------------------------------------------------------------

func TestNew_RequestIDIsReachableFromTheHandler(t *testing.T) {
	log := kitlogtest.NewMockLogger()

	var (
		seen string
		ok   bool
	)
	h := handlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen, ok = httpmw.RequestIDFrom(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	serve(h, httpmw.New(httpmw.Options{Logger: log}), httptest.NewRequest("GET", "/", nil))

	require.True(t, ok, "the handler must be able to read the request ID")
	require.NotEmpty(t, seen)
	require.Equal(t, seen, entryFields(t, onlyEntry(t, log))["request_id"],
		"the ID the handler saw and the ID on the log line must be the same one")
}

func TestRequestIDFrom_ReportsAbsence(t *testing.T) {
	id, ok := httpmw.RequestIDFrom(context.Background())

	require.False(t, ok)
	require.Empty(t, id)
}

func TestRequestIDFrom_NilContext(t *testing.T) {
	//nolint:staticcheck // passing nil is exactly the case under test
	id, ok := httpmw.RequestIDFrom(nil)

	require.False(t, ok)
	require.Empty(t, id)
}

func TestNew_UnusableGeneratedIDBecomesEmptyRatherThanGarbage(t *testing.T) {
	log := kitlogtest.NewMockLogger()

	h := handlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, ok := httpmw.RequestIDFrom(r.Context())
		require.True(t, ok, "the key is still set, so the handler can tell the middleware ran")
		require.Empty(t, id)
		w.WriteHeader(http.StatusOK)
	})

	// A generator that succeeds but returns something unloggable is held to the
	// same rule as an inbound header: it never reaches the log stream.
	mw := httpmw.New(httpmw.Options{
		Logger: log,
		IDGen:  func() (string, error) { return "bad\nid", nil },
	})

	w := serve(h, mw, httptest.NewRequest("GET", "/", nil))

	require.Empty(t, entryFields(t, onlyEntry(t, log))["request_id"])
	require.Empty(t, w.Header().Get("X-Request-Id"), "no header is set when there is no usable ID")
}

func TestNew_ReusesAnInboundRequestIDVerbatim(t *testing.T) {
	log := kitlogtest.NewMockLogger()

	h := handlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })

	req := httptest.NewRequest("POST", "/api/users", nil)
	req.Header.Set("X-Request-Id", "inbound-123")
	w := serve(h, httpmw.New(httpmw.Options{Logger: log}), req)

	require.Equal(t, "inbound-123", entryFields(t, onlyEntry(t, log))["request_id"])
	require.Equal(t, "inbound-123", w.Header().Get("X-Request-Id"))
}

func TestNew_GeneratesAnIDWhenNoneArrives(t *testing.T) {
	log := kitlogtest.NewMockLogger()

	h := handlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })

	serve(h, httpmw.New(httpmw.Options{Logger: log}), httptest.NewRequest("GET", "/", nil))

	require.NotEmpty(t, entryFields(t, onlyEntry(t, log))["request_id"])
}

func TestNew_RejectsAnUnusableInboundID(t *testing.T) {
	cases := map[string]string{
		"control characters": "abc\ndef",
		"too long":           strings.Repeat("x", 1024),
	}

	for name, inbound := range cases {
		t.Run(name, func(t *testing.T) {
			log := kitlogtest.NewMockLogger()

			h := handlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })

			req := httptest.NewRequest("GET", "/", nil)
			req.Header.Set("X-Request-Id", inbound)
			serve(h, httpmw.New(httpmw.Options{Logger: log}), req)

			// The header is attacker-controlled and goes straight into the log
			// stream, so an unusable value is replaced rather than echoed.
			got := entryFields(t, onlyEntry(t, log))["request_id"]
			require.NotEqual(t, inbound, got)
			require.NotEmpty(t, got)
		})
	}
}

func TestNew_ResponseIDHeaderIsSetBeforeTheStatus(t *testing.T) {
	log := kitlogtest.NewMockLogger()

	h := handlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// A header set after WriteHeader never reaches the client, so the
		// middleware has to set it before the handler can write the status.
		require.NotEmpty(t, w.Header().Get("X-Request-Id"),
			"the response ID must be set before the handler runs")
		w.WriteHeader(http.StatusOK)
	})

	w := serve(h, httpmw.New(httpmw.Options{Logger: log}), httptest.NewRequest("GET", "/", nil))

	require.NotEmpty(t, w.Header().Get("X-Request-Id"))
}

func TestNew_DisableResponseIDHeader(t *testing.T) {
	log := kitlogtest.NewMockLogger()

	h := handlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })

	w := serve(h, httpmw.New(httpmw.Options{Logger: log, DisableResponseIDHeader: true}), httptest.NewRequest("GET", "/", nil))

	require.Empty(t, w.Header().Get("X-Request-Id"))
	require.NotEmpty(t, entryFields(t, onlyEntry(t, log))["request_id"], "the ID is still logged")
}

func TestNew_CustomRequestIDHeader(t *testing.T) {
	log := kitlogtest.NewMockLogger()

	h := handlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-Correlation-Id", "corr-9")
	w := serve(h, httpmw.New(httpmw.Options{Logger: log, RequestIDHeader: "X-Correlation-Id"}), req)

	require.Equal(t, "corr-9", entryFields(t, onlyEntry(t, log))["request_id"])
	require.Equal(t, "corr-9", w.Header().Get("X-Correlation-Id"))
}

func TestNew_IDGenFailureDoesNotPanicAndStillServes(t *testing.T) {
	log := kitlogtest.NewMockLogger()

	h := handlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("served"))
	})

	mw := httpmw.New(httpmw.Options{
		Logger: log,
		IDGen:  func() (string, error) { return "", errors.New("entropy exhausted") },
	})

	var w *httptest.ResponseRecorder
	require.NotPanics(t, func() { w = serve(h, mw, httptest.NewRequest("GET", "/", nil)) })

	require.Equal(t, "served", w.Body.String(), "the request must still be served")
	require.Equal(t, http.StatusOK, entryFields(t, onlyEntry(t, log))["status"])
}

func TestNew_IDGenPanicIsContained(t *testing.T) {
	log := kitlogtest.NewMockLogger()

	// The default generator is idgen.NewULID, which panics if its entropy
	// reader fails. A logging middleware must not turn that into a dropped
	// request.
	h := handlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })

	mw := httpmw.New(httpmw.Options{
		Logger: log,
		IDGen:  func() (string, error) { panic("entropy source exploded") },
	})

	require.NotPanics(t, func() { serve(h, mw, httptest.NewRequest("GET", "/", nil)) })
	require.Len(t, log.Entries, 1)
}

// ---------------------------------------------------------------------------
// Status, bytes and level
// ---------------------------------------------------------------------------

func TestNew_FirstWriteHeaderWins(t *testing.T) {
	log := kitlogtest.NewMockLogger()

	h := handlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		w.WriteHeader(http.StatusInternalServerError) // net/http ignores this
	})

	w := serve(h, httpmw.New(httpmw.Options{Logger: log}), httptest.NewRequest("GET", "/", nil))

	require.Equal(t, http.StatusCreated, w.Code)
	require.Equal(t, http.StatusCreated, entryFields(t, onlyEntry(t, log))["status"],
		"the log and the wire must agree on the status")
}

func TestNew_ImplicitOKWhenTheHandlerOnlyWrites(t *testing.T) {
	log := kitlogtest.NewMockLogger()

	h := handlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("body")) })

	serve(h, httpmw.New(httpmw.Options{Logger: log}), httptest.NewRequest("GET", "/", nil))

	require.Equal(t, http.StatusOK, entryFields(t, onlyEntry(t, log))["status"])
}

func TestNew_StatusWhenTheHandlerWritesNothing(t *testing.T) {
	log := kitlogtest.NewMockLogger()

	h := handlerFunc(func(http.ResponseWriter, *http.Request) {})

	serve(h, httpmw.New(httpmw.Options{Logger: log}), httptest.NewRequest("GET", "/", nil))

	fields := entryFields(t, onlyEntry(t, log))
	require.Equal(t, http.StatusOK, fields["status"], "net/http sends 200 for a handler that writes nothing")
	require.EqualValues(t, 0, fields["bytes_written"])
}

func TestNew_CountsBytesWritten(t *testing.T) {
	log := kitlogtest.NewMockLogger()

	body := "twelve chars"
	h := handlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n, err := w.Write([]byte(body))
		require.NoError(t, err)
		require.Equal(t, len(body), n)
	})

	serve(h, httpmw.New(httpmw.Options{Logger: log}), httptest.NewRequest("GET", "/", nil))

	require.EqualValues(t, len(body), entryFields(t, onlyEntry(t, log))["bytes_written"])
}

func TestNew_LevelReflectsTheOutcome(t *testing.T) {
	cases := []struct {
		status int
		want   slog.Level
	}{
		{http.StatusOK, slog.LevelInfo},
		{http.StatusMovedPermanently, slog.LevelInfo},
		{http.StatusBadRequest, slog.LevelWarn},
		{http.StatusNotFound, slog.LevelWarn},
		{http.StatusInternalServerError, slog.LevelError},
		{http.StatusServiceUnavailable, slog.LevelError},
	}

	for _, tc := range cases {
		t.Run(http.StatusText(tc.status), func(t *testing.T) {
			log := kitlogtest.NewMockLogger()

			h := handlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(tc.status) })
			serve(h, httpmw.New(httpmw.Options{Logger: log}), httptest.NewRequest("GET", "/", nil))

			require.Equal(t, tc.want, onlyEntry(t, log).Level)
		})
	}
}

func TestNew_CustomLevelFor(t *testing.T) {
	log := kitlogtest.NewMockLogger()

	h := handlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })

	mw := httpmw.New(httpmw.Options{
		Logger:   log,
		LevelFor: func(int) slog.Level { return slog.LevelDebug },
	})
	serve(h, mw, httptest.NewRequest("GET", "/", nil))

	require.Equal(t, slog.LevelDebug, onlyEntry(t, log).Level)
}

func TestNew_LogsMethodPathAndDuration(t *testing.T) {
	log := kitlogtest.NewMockLogger()

	h := handlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(2 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	})

	serve(h, httpmw.New(httpmw.Options{Logger: log}), httptest.NewRequest("PATCH", "/api/v1/users/123", nil))

	entry := onlyEntry(t, log)
	fields := entryFields(t, entry)
	require.Equal(t, "http_request", entry.Message)
	require.Equal(t, "PATCH", fields["method"])
	require.Equal(t, "/api/v1/users/123", fields["path"])
	require.GreaterOrEqual(t, fields["duration_ms"].(int64), int64(2))
}

// ---------------------------------------------------------------------------
// Panics
// ---------------------------------------------------------------------------

func TestNew_PanicIsLoggedOnceAndRepropagated(t *testing.T) {
	log := kitlogtest.NewMockLogger()

	h := handlerFunc(func(http.ResponseWriter, *http.Request) { panic("boom") })

	require.PanicsWithValue(t, "boom", func() {
		serve(h, httpmw.New(httpmw.Options{Logger: log}), httptest.NewRequest("GET", "/explode", nil))
	}, "a logging middleware must observe the failure path, not change it")

	entry := onlyEntry(t, log)
	require.Equal(t, slog.LevelError, entry.Level)

	fields := entryFields(t, entry)
	require.Equal(t, "http_request", entry.Message)
	require.Equal(t, "boom", fields["panic"])
	require.Contains(t, fields["stack"], "httpmw_test")
	require.Equal(t, http.StatusInternalServerError, fields["status"])
	require.Equal(t, "/explode", fields["path"])
}

func TestNew_PanicAfterAStatusWasWrittenKeepsThatStatus(t *testing.T) {
	log := kitlogtest.NewMockLogger()

	h := handlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		panic("late")
	})

	require.Panics(t, func() {
		serve(h, httpmw.New(httpmw.Options{Logger: log}), httptest.NewRequest("GET", "/", nil))
	})

	// 202 already went to the client; reporting 500 would contradict the wire.
	require.Equal(t, http.StatusAccepted, entryFields(t, onlyEntry(t, log))["status"])
}

func TestNew_PanicWithANonStringValue(t *testing.T) {
	log := kitlogtest.NewMockLogger()

	h := handlerFunc(func(http.ResponseWriter, *http.Request) { panic(errors.New("typed failure")) })

	require.Panics(t, func() {
		serve(h, httpmw.New(httpmw.Options{Logger: log}), httptest.NewRequest("GET", "/", nil))
	})

	require.Equal(t, "typed failure", entryFields(t, onlyEntry(t, log))["panic"])
}

func TestNew_HttpErrAbortHandlerIsNotSwallowed(t *testing.T) {
	log := kitlogtest.NewMockLogger()

	// net/http uses ErrAbortHandler as a quiet abort signal. It must keep
	// propagating, and it is not an error worth a stack trace.
	h := handlerFunc(func(http.ResponseWriter, *http.Request) { panic(http.ErrAbortHandler) })

	require.PanicsWithValue(t, http.ErrAbortHandler, func() {
		serve(h, httpmw.New(httpmw.Options{Logger: log}), httptest.NewRequest("GET", "/", nil))
	})

	require.Len(t, log.Entries, 1)
	require.NotContains(t, entryFields(t, onlyEntry(t, log)), "stack")
}

// ---------------------------------------------------------------------------
// Context cancellation and skipping
// ---------------------------------------------------------------------------

func TestNew_CancelledRequestIsDistinguishable(t *testing.T) {
	log := kitlogtest.NewMockLogger()

	h := handlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequest("GET", "/", nil).WithContext(ctx)

	serve(h, httpmw.New(httpmw.Options{Logger: log}), req)

	require.Equal(t, context.Canceled.Error(), entryFields(t, onlyEntry(t, log))["context_err"])
}

func TestNew_CompletedRequestCarriesNoContextError(t *testing.T) {
	log := kitlogtest.NewMockLogger()

	h := handlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })

	serve(h, httpmw.New(httpmw.Options{Logger: log}), httptest.NewRequest("GET", "/", nil))

	require.NotContains(t, entryFields(t, onlyEntry(t, log)), "context_err")
}

func TestNew_SkipPaths(t *testing.T) {
	log := kitlogtest.NewMockLogger()

	h := handlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	mw := httpmw.New(httpmw.Options{Logger: log, SkipPaths: []string{"/healthz"}})

	w := serve(h, mw, httptest.NewRequest("GET", "/healthz", nil))
	require.Equal(t, "ok", w.Body.String(), "a skipped path is still served")
	require.Empty(t, log.Entries)

	serve(h, mw, httptest.NewRequest("GET", "/other", nil))
	require.Len(t, log.Entries, 1)
}

// ---------------------------------------------------------------------------
// Concurrency
// ---------------------------------------------------------------------------

func TestNew_ConcurrentRequests(t *testing.T) {
	log := kitlogtest.NewMockLogger()

	h := handlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, ok := httpmw.RequestIDFrom(r.Context())
		require.True(t, ok)
		require.NotEmpty(t, id)
		w.WriteHeader(http.StatusOK)
	})
	mw := httpmw.New(httpmw.Options{Logger: log})(h)

	const n = 100
	var wg sync.WaitGroup
	wg.Add(n)
	for i := range n {
		go func() {
			defer wg.Done()
			mw.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", fmt.Sprintf("/r/%d", i), nil))
		}()
	}
	wg.Wait()

	require.Len(t, log.Entries, n)

	ids := map[any]struct{}{}
	for _, e := range log.Entries {
		ids[entryFields(t, e)["request_id"]] = struct{}{}
	}
	require.Len(t, ids, n, "every request must get its own ID")
}

func BenchmarkMiddleware(b *testing.B) {
	log := kitlogtest.NewMockLogger()
	h := handlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})
	mw := httpmw.New(httpmw.Options{Logger: log})(h)
	req := httptest.NewRequest("GET", "/bench", nil)

	b.ReportAllocs()
	for b.Loop() {
		mw.ServeHTTP(httptest.NewRecorder(), req)
	}
}
