package httpmw

import (
	"bufio"
	"bytes"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// These tests drive responseRecorder directly. The optional interfaces it has
// to preserve are a property of the wrapper, not of the middleware, and testing
// them here says so -- and says it without a logger, a request or a handler in
// the way.

func TestResponseRecorder_PreservesFlusher(t *testing.T) {
	underlying := httptest.NewRecorder()
	rr := &responseRecorder{ResponseWriter: underlying}

	// The whole defect: embedding the http.ResponseWriter interface promotes
	// only Header, Write and WriteHeader, so this assertion used to fail and
	// every SSE handler behind the middleware broke.
	var w http.ResponseWriter = rr
	f, ok := w.(http.Flusher)
	require.True(t, ok, "http.Flusher must survive the wrapper")

	require.NotPanics(t, f.Flush)
	require.True(t, underlying.Flushed, "Flush must reach the underlying writer")
}

func TestResponseRecorder_PreservesHijacker(t *testing.T) {
	underlying := &hijackableWriter{ResponseRecorder: httptest.NewRecorder()}

	var w http.ResponseWriter = &responseRecorder{ResponseWriter: underlying}
	hj, ok := w.(http.Hijacker)
	require.True(t, ok, "http.Hijacker must survive the wrapper")

	conn, buf, err := hj.Hijack()
	require.NoError(t, err)
	require.NotNil(t, conn)
	require.NotNil(t, buf)
	require.True(t, underlying.hijackCalled, "Hijack must reach the underlying writer")
}

func TestResponseRecorder_PreservesPusher(t *testing.T) {
	underlying := &pushableWriter{ResponseRecorder: httptest.NewRecorder()}

	var w http.ResponseWriter = &responseRecorder{ResponseWriter: underlying}
	p, ok := w.(http.Pusher)
	require.True(t, ok, "http.Pusher must survive the wrapper")

	require.NoError(t, p.Push("/style.css", nil))
	require.Equal(t, []string{"/style.css"}, underlying.pushed)
}

func TestResponseRecorder_PreservesReaderFrom(t *testing.T) {
	underlying := &readerFromWriter{ResponseRecorder: httptest.NewRecorder()}
	rr := &responseRecorder{ResponseWriter: underlying}

	var w http.ResponseWriter = rr
	rf, ok := w.(io.ReaderFrom)
	require.True(t, ok, "io.ReaderFrom must survive the wrapper")

	n, err := rf.ReadFrom(strings.NewReader("payload"))
	require.NoError(t, err)
	require.EqualValues(t, len("payload"), n)
	require.True(t, underlying.readFromCalled, "the underlying fast path must be used when it exists")
	require.EqualValues(t, len("payload"), rr.bytes)
}

func TestResponseRecorder_ReaderFromFallsBackAndStillCounts(t *testing.T) {
	underlying := &plainWriter{header: http.Header{}}
	rr := &responseRecorder{ResponseWriter: underlying}

	n, err := rr.ReadFrom(strings.NewReader("payload"))

	require.NoError(t, err)
	require.EqualValues(t, len("payload"), n)
	require.Equal(t, "payload", underlying.body.String())
	require.EqualValues(t, len("payload"), rr.bytes, "the fallback path must count bytes too")
	require.Equal(t, http.StatusOK, rr.statusCode(), "ReadFrom implies the header net/http would have sent")
}

func TestResponseRecorder_FlushIsANoOpWhenTheWriterCannotFlush(t *testing.T) {
	// Declaring Flush unconditionally makes the type assertion always succeed,
	// even for a writer that cannot flush. The honest consequence is that it
	// must do nothing rather than panic.
	rr := &responseRecorder{ResponseWriter: &plainWriter{header: http.Header{}}}

	require.NotPanics(t, rr.Flush)
}

func TestResponseRecorder_HijackReportsUnsupported(t *testing.T) {
	rr := &responseRecorder{ResponseWriter: &plainWriter{header: http.Header{}}}

	conn, buf, err := rr.Hijack()

	require.ErrorIs(t, err, http.ErrNotSupported)
	require.Nil(t, conn)
	require.Nil(t, buf)
}

func TestResponseRecorder_PushReportsUnsupported(t *testing.T) {
	rr := &responseRecorder{ResponseWriter: &plainWriter{header: http.Header{}}}

	require.ErrorIs(t, rr.Push("/x", nil), http.ErrNotSupported)
}

func TestResponseRecorder_FirstWriteHeaderWins(t *testing.T) {
	underlying := httptest.NewRecorder()
	rr := &responseRecorder{ResponseWriter: underlying}

	rr.WriteHeader(http.StatusCreated)
	rr.WriteHeader(http.StatusInternalServerError)

	// net/http ignores a superfluous WriteHeader, so a recorder that kept the
	// last one would report a status the client never received.
	require.Equal(t, http.StatusCreated, rr.statusCode())
	require.Equal(t, http.StatusCreated, underlying.Code)
}

func TestResponseRecorder_ImplicitOK(t *testing.T) {
	t.Run("a bare Write sends 200", func(t *testing.T) {
		rr := &responseRecorder{ResponseWriter: httptest.NewRecorder()}

		_, err := rr.Write([]byte("body"))

		require.NoError(t, err)
		require.Equal(t, http.StatusOK, rr.statusCode())
	})

	t.Run("writing nothing at all still means 200", func(t *testing.T) {
		rr := &responseRecorder{ResponseWriter: httptest.NewRecorder()}

		require.Equal(t, http.StatusOK, rr.statusCode())
		require.Zero(t, rr.bytes)
	})
}

func TestResponseRecorder_CountsBytes(t *testing.T) {
	rr := &responseRecorder{ResponseWriter: httptest.NewRecorder()}

	for _, chunk := range []string{"one", "two", "three"} {
		n, err := rr.Write([]byte(chunk))
		require.NoError(t, err)
		require.Equal(t, len(chunk), n)
	}

	require.EqualValues(t, len("onetwothree"), rr.bytes)
}

func TestResponseRecorder_Unwrap(t *testing.T) {
	underlying := httptest.NewRecorder()
	rr := &responseRecorder{ResponseWriter: underlying}

	// http.NewResponseController walks Unwrap to reach the real writer, which
	// is how a caller can still find out whether an operation is genuinely
	// supported despite the unconditional method set above.
	require.Same(t, underlying, rr.Unwrap())
}

// ---------------------------------------------------------------------------
// Test doubles
// ---------------------------------------------------------------------------

// plainWriter implements http.ResponseWriter and nothing else, so the wrapper's
// fallback paths are exercised.
type plainWriter struct {
	header http.Header
	body   bytes.Buffer
	status int
}

func (p *plainWriter) Header() http.Header         { return p.header }
func (p *plainWriter) Write(b []byte) (int, error) { return p.body.Write(b) }
func (p *plainWriter) WriteHeader(status int)      { p.status = status }

type hijackableWriter struct {
	*httptest.ResponseRecorder
	hijackCalled bool
}

func (h *hijackableWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h.hijackCalled = true
	client, server := net.Pipe()
	go func() { _ = server.Close() }()
	return client, bufio.NewReadWriter(bufio.NewReader(client), bufio.NewWriter(client)), nil
}

type pushableWriter struct {
	*httptest.ResponseRecorder
	pushed []string
}

func (p *pushableWriter) Push(target string, _ *http.PushOptions) error {
	p.pushed = append(p.pushed, target)
	return nil
}

type readerFromWriter struct {
	*httptest.ResponseRecorder
	readFromCalled bool
}

func (r *readerFromWriter) ReadFrom(src io.Reader) (int64, error) {
	r.readFromCalled = true
	return io.Copy(r.ResponseRecorder.Body, src)
}
