package handler_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pablogore/kit-logger/pkg/logger/handler"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func testCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// capture is the shared, mutex-protected sink state. Handlers derived through
// WithAttrs/WithGroup share one capture, exactly as real downstream handlers
// share their destination.
type capture struct {
	mu      sync.Mutex
	records []slog.Record
	ctxs    []context.Context

	before  func(*slog.Record) // optional mutation hook, runs before capture
	delay   time.Duration
	err     error
	release <-chan struct{}
}

// recorder is a downstream handler that captures records under a mutex, so the
// test body can read them without racing the buffer worker. It honors WithAttrs
// and WithGroup so attribute propagation is actually observable.
type recorder struct {
	cap    *capture
	attrs  []slog.Attr
	groups []string
}

func newRecorder() *recorder { return &recorder{cap: &capture{}} }

func (r *recorder) withDelay(d time.Duration) *recorder     { r.cap.delay = d; return r }
func (r *recorder) withErr(err error) *recorder             { r.cap.err = err; return r }
func (r *recorder) withRelease(c <-chan struct{}) *recorder { r.cap.release = c; return r }

func (r *recorder) withBefore(f func(*slog.Record)) *recorder { r.cap.before = f; return r }

func (r *recorder) Enabled(context.Context, slog.Level) bool { return true }

func (r *recorder) Handle(ctx context.Context, rec slog.Record) error {
	c := r.cap
	if c.release != nil {
		<-c.release
	}
	if c.delay > 0 {
		time.Sleep(c.delay)
	}
	if len(r.attrs) > 0 {
		rec.AddAttrs(r.attrs...)
	}
	if c.before != nil {
		c.before(&rec)
	}
	c.mu.Lock()
	c.records = append(c.records, rec)
	c.ctxs = append(c.ctxs, ctx)
	c.mu.Unlock()
	return c.err
}

func (r *recorder) WithAttrs(attrs []slog.Attr) slog.Handler {
	// slices.Clip so two siblings derived from the same parent cannot write
	// into a shared backing array.
	next := make([]slog.Attr, 0, len(r.attrs)+len(attrs))
	next = append(next, r.attrs...)
	next = append(next, attrs...)
	return &recorder{cap: r.cap, attrs: next, groups: r.groups}
}

func (r *recorder) WithGroup(name string) slog.Handler {
	next := make([]string, 0, len(r.groups)+1)
	next = append(next, r.groups...)
	next = append(next, name)
	return &recorder{cap: r.cap, attrs: r.attrs, groups: next}
}

func (r *recorder) len() int {
	r.cap.mu.Lock()
	defer r.cap.mu.Unlock()
	return len(r.cap.records)
}

func (r *recorder) snapshot() []slog.Record {
	r.cap.mu.Lock()
	defer r.cap.mu.Unlock()
	return append([]slog.Record(nil), r.cap.records...)
}

func (r *recorder) contexts() []context.Context {
	r.cap.mu.Lock()
	defer r.cap.mu.Unlock()
	return append([]context.Context(nil), r.cap.ctxs...)
}

func attrsOf(rec slog.Record) map[string]string {
	out := make(map[string]string, rec.NumAttrs())
	rec.Attrs(func(a slog.Attr) bool {
		out[a.Key] = a.Value.String()
		return true
	})
	return out
}

func newRecord(msg string) slog.Record {
	return slog.NewRecord(time.Now(), slog.LevelInfo, msg, 0)
}

// recordWithSpareAttrCapacity builds a record whose overflow attr slice has
// spare capacity (cap > len), which is the precondition for two copies of the
// record to write into the same backing array. slog.Record stores its first
// attrs in an inline array and spills the rest into a slice, so the front is
// filled first and the overflow is then grown one attr at a time until the
// geometric growth leaves room past the end.
func recordWithSpareAttrCapacity(msg string) slog.Record {
	r := slog.NewRecord(time.Now(), slog.LevelInfo, msg, 0)
	r.AddAttrs(
		slog.Int("f1", 1), slog.Int("f2", 2), slog.Int("f3", 3),
		slog.Int("f4", 4), slog.Int("f5", 5),
	)
	r.AddAttrs(slog.Int("b1", 1))
	r.AddAttrs(slog.Int("b2", 2))
	r.AddAttrs(slog.Int("b3", 3))
	return r
}

// settledGoroutines polls until the goroutine count stops changing, so the
// assertions below are not at the mercy of scheduler or GC timing.
func settledGoroutines() int {
	prev := -1
	for i := 0; i < 200; i++ {
		runtime.GC()
		runtime.Gosched()
		n := runtime.NumGoroutine()
		if n == prev {
			return n
		}
		prev = n
		time.Sleep(5 * time.Millisecond)
	}
	return prev
}

// ---------------------------------------------------------------------------
// goroutine / buffer ownership
// ---------------------------------------------------------------------------

func TestBufferedHandler_WithAttrs_DoesNotCreateGoroutines(t *testing.T) {
	buffered := handler.NewBufferedHandler(newRecorder(), 16)
	t.Cleanup(func() { _ = buffered.Shutdown(testCtx(t)) })

	before := settledGoroutines()

	var h slog.Handler = buffered
	for i := 0; i < 10_000; i++ {
		h = h.WithAttrs([]slog.Attr{slog.Int("i", i)})
	}
	require.NotNil(t, h)

	after := settledGoroutines()
	require.LessOrEqual(t, after, before+1,
		"WithAttrs must not spawn a goroutine per derived handler (before=%d after=%d)", before, after)
}

func TestBufferedHandler_WithGroup_DoesNotCreateGoroutines(t *testing.T) {
	buffered := handler.NewBufferedHandler(newRecorder(), 16)
	t.Cleanup(func() { _ = buffered.Shutdown(testCtx(t)) })

	before := settledGoroutines()

	var h slog.Handler = buffered
	for i := 0; i < 10_000; i++ {
		h = h.WithGroup(fmt.Sprintf("g%d", i))
	}
	require.NotNil(t, h)

	after := settledGoroutines()
	require.LessOrEqual(t, after, before+1,
		"WithGroup must not spawn a goroutine per derived handler (before=%d after=%d)", before, after)
}

func TestBufferedHandler_DerivedHandlersShareOneCore(t *testing.T) {
	// A blocked worker means nothing is ever dequeued, so a buffer of 1 fills
	// after the first accepted record.
	release := make(chan struct{})
	rec := newRecorder().withRelease(release)
	root := handler.NewBufferedHandler(rec, 1)
	t.Cleanup(func() {
		close(release)
		_ = root.Shutdown(testCtx(t))
	})

	derived := root.WithAttrs([]slog.Attr{slog.String("k", "v")})

	// Drain-blocking: fill through the derived view only.
	for i := 0; i < 20; i++ {
		_ = derived.Handle(context.Background(), newRecord("m"))
	}

	require.Positive(t, root.Dropped(),
		"overflow through a derived handler must be visible on the root: derived views must share the root core")
}

// ---------------------------------------------------------------------------
// attribute correctness across derivation
// ---------------------------------------------------------------------------

func TestBufferedHandler_SiblingsDoNotShareAttributes(t *testing.T) {
	rec := newRecorder()
	buffered := handler.NewBufferedHandler(rec, 64)
	t.Cleanup(func() { _ = buffered.Shutdown(testCtx(t)) })

	// A base with spare capacity is what makes append-aliasing observable.
	base := buffered.WithAttrs([]slog.Attr{slog.String("base", "b")})
	left := base.WithAttrs([]slog.Attr{slog.String("side", "left")})
	right := base.WithAttrs([]slog.Attr{slog.String("side", "right")})

	require.NoError(t, left.Handle(context.Background(), newRecord("left")))
	require.NoError(t, right.Handle(context.Background(), newRecord("right")))
	require.NoError(t, buffered.Flush(testCtx(t)))

	got := map[string]string{}
	for _, r := range rec.snapshot() {
		got[r.Message] = attrsOf(r)["side"]
	}
	require.Equal(t, "left", got["left"], "sibling handlers must not observe each other's attributes")
	require.Equal(t, "right", got["right"], "sibling handlers must not observe each other's attributes")
}

func TestBufferedHandler_ClonesRecordBeforeCrossingGoroutine(t *testing.T) {
	// The downstream handler mutates the record (as ComponentHandler does),
	// while the producer keeps appending to the record it already submitted.
	// Without Record.Clone() both writes target the same backing array and the
	// standard library injects a "!BUG" attr on the unsafe copy.
	rec := newRecorder().withBefore(func(r *slog.Record) {
		r.AddAttrs(slog.String("downstream", "yes"))
	})
	buffered := handler.NewBufferedHandler(rec, 64)
	t.Cleanup(func() { _ = buffered.Shutdown(testCtx(t)) })

	r := recordWithSpareAttrCapacity("clone")

	require.NoError(t, buffered.Handle(context.Background(), r))

	// Producer reuses its own record after Handle returned.
	r.AddAttrs(slog.String("producer", "yes"))

	require.NoError(t, buffered.Flush(testCtx(t)))

	snap := rec.snapshot()
	require.Len(t, snap, 1)
	got := attrsOf(snap[0])

	require.NotContains(t, got, "!BUG",
		"downstream record was mutated through a shared backing array: Handle must Clone() the record")
	require.Equal(t, "yes", got["downstream"])
	require.NotContains(t, got, "producer",
		"the producer's post-Handle mutation must not leak into the delivered record")
}

// ---------------------------------------------------------------------------
// context propagation
// ---------------------------------------------------------------------------

type ctxKey struct{}

func TestBufferedHandler_PreservesCallerContext(t *testing.T) {
	rec := newRecorder()
	buffered := handler.NewBufferedHandler(rec, 16)
	t.Cleanup(func() { _ = buffered.Shutdown(testCtx(t)) })

	ctx := context.WithValue(context.Background(), ctxKey{}, "request-42")
	require.NoError(t, buffered.Handle(ctx, newRecord("m")))
	require.NoError(t, buffered.Flush(testCtx(t)))

	ctxs := rec.contexts()
	require.Len(t, ctxs, 1)
	require.Equal(t, "request-42", ctxs[0].Value(ctxKey{}),
		"the caller's context must reach the downstream handler, not context.Background()")
}

func TestBufferedHandler_EachRecordKeepsItsOwnContextAndHandler(t *testing.T) {
	rec := newRecorder()
	buffered := handler.NewBufferedHandler(rec, 64)
	t.Cleanup(func() { _ = buffered.Shutdown(testCtx(t)) })

	for i := 0; i < 10; i++ {
		id := fmt.Sprintf("req-%d", i)
		ctx := context.WithValue(context.Background(), ctxKey{}, id)
		h := buffered.WithAttrs([]slog.Attr{slog.String("req", id)})
		require.NoError(t, h.Handle(ctx, newRecord(id)))
	}
	require.NoError(t, buffered.Flush(testCtx(t)))

	snap := rec.snapshot()
	ctxs := rec.contexts()
	require.Len(t, snap, 10)

	for i := range snap {
		want := snap[i].Message
		require.Equal(t, want, ctxs[i].Value(ctxKey{}),
			"record %q was delivered with another record's context", want)
	}
}

// ---------------------------------------------------------------------------
// Flush
// ---------------------------------------------------------------------------

func TestBufferedHandler_Flush_WaitsForDownstreamProcessing(t *testing.T) {
	rec := newRecorder().withDelay(20 * time.Millisecond)
	buffered := handler.NewBufferedHandler(rec, 64)
	t.Cleanup(func() { _ = buffered.Shutdown(testCtx(t)) })

	const n = 5
	for i := 0; i < n; i++ {
		require.NoError(t, buffered.Handle(context.Background(), newRecord("m")))
	}

	require.NoError(t, buffered.Flush(testCtx(t)))
	// Read immediately: Flush must not return while the worker is still inside
	// next.Handle for a previously accepted record.
	require.Equal(t, n, rec.len(),
		"Flush returned before every accepted record finished downstream processing")
}

func TestBufferedHandler_Flush_HonorsContextDeadline(t *testing.T) {
	release := make(chan struct{})
	rec := newRecorder().withRelease(release)
	buffered := handler.NewBufferedHandler(rec, 64)
	t.Cleanup(func() {
		close(release)
		_ = buffered.Shutdown(testCtx(t))
	})

	require.NoError(t, buffered.Handle(context.Background(), newRecord("m")))

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := buffered.Flush(ctx)
	require.Error(t, err)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Less(t, time.Since(start), 5*time.Second, "Flush must return when its context expires")
}

func TestBufferedHandler_Flush_AfterShutdownReturnsShutdownError(t *testing.T) {
	buffered := handler.NewBufferedHandler(newRecorder(), 16)
	require.NoError(t, buffered.Shutdown(testCtx(t)))

	err := buffered.Flush(testCtx(t))
	require.ErrorIs(t, err, handler.ErrShutdown)
}

// ---------------------------------------------------------------------------
// Shutdown
// ---------------------------------------------------------------------------

func TestBufferedHandler_Shutdown_DrainsAcceptedRecords(t *testing.T) {
	rec := newRecorder().withDelay(10 * time.Millisecond)
	buffered := handler.NewBufferedHandler(rec, 100)

	const n = 20
	for i := 0; i < n; i++ {
		require.NoError(t, buffered.Handle(context.Background(), newRecord("m")))
	}

	require.NoError(t, buffered.Shutdown(testCtx(t)))
	require.Equal(t, n, rec.len(),
		"Shutdown must deliver every accepted record before returning")
}

func TestBufferedHandler_Shutdown_IsIdempotent(t *testing.T) {
	buffered := handler.NewBufferedHandler(newRecorder(), 16)

	require.NoError(t, buffered.Handle(context.Background(), newRecord("m")))

	var wg sync.WaitGroup
	errs := make([]error, 100)
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = buffered.Shutdown(testCtx(t))
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		require.NoError(t, err, "concurrent Shutdown call %d must not fail", i)
	}
	// And once more, sequentially, long after the worker exited.
	require.NoError(t, buffered.Shutdown(testCtx(t)))
}

func TestBufferedHandler_Shutdown_HonorsContextDeadline(t *testing.T) {
	release := make(chan struct{})
	rec := newRecorder().withRelease(release)
	buffered := handler.NewBufferedHandler(rec, 64)
	t.Cleanup(func() {
		close(release)
		_ = buffered.Shutdown(testCtx(t))
	})

	require.NoError(t, buffered.Handle(context.Background(), newRecord("m")))

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := buffered.Shutdown(ctx)
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestBufferedHandler_Shutdown_WithExpiredContextDoesNotBlock(t *testing.T) {
	release := make(chan struct{})
	rec := newRecorder().withRelease(release)
	buffered := handler.NewBufferedHandler(rec, 64)
	t.Cleanup(func() {
		close(release)
		_ = buffered.Shutdown(testCtx(t))
	})

	require.NoError(t, buffered.Handle(context.Background(), newRecord("m")))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	start := time.Now()
	err := buffered.Shutdown(ctx)
	require.ErrorIs(t, err, context.Canceled)
	require.Less(t, time.Since(start), time.Second)
}

func TestBufferedHandler_HandleAfterShutdownDoesNotPanic(t *testing.T) {
	rec := newRecorder()
	buffered := handler.NewBufferedHandler(rec, 16)
	require.NoError(t, buffered.Shutdown(testCtx(t)))

	require.NotPanics(t, func() {
		for i := 0; i < 1000; i++ {
			err := buffered.Handle(context.Background(), newRecord("m"))
			require.ErrorIs(t, err, handler.ErrShutdown)
		}
	})
	require.Equal(t, uint64(1000), buffered.Rejected())
	require.Zero(t, rec.len(), "no record may be delivered after Shutdown completed")
}

func TestBufferedHandler_HandleDuringConcurrentShutdownDoesNotPanic(t *testing.T) {
	rec := newRecorder()
	buffered := handler.NewBufferedHandler(rec, 8)

	var wg sync.WaitGroup
	stop := make(chan struct{})
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				// Must never panic on a send to a closed channel.
				_ = buffered.Handle(context.Background(), newRecord("m"))
			}
		}()
	}

	time.Sleep(20 * time.Millisecond)
	require.NotPanics(t, func() {
		require.NoError(t, buffered.Shutdown(testCtx(t)))
	})
	close(stop)
	wg.Wait()
}

// ---------------------------------------------------------------------------
// overflow accounting
// ---------------------------------------------------------------------------

func TestBufferedHandler_Overflow_IsObservableAndExact(t *testing.T) {
	release := make(chan struct{})
	rec := newRecorder().withRelease(release)
	buffered := handler.NewBufferedHandler(rec, 4)

	const attempted = 200
	var accepted, dropped uint64
	for i := 0; i < attempted; i++ {
		err := buffered.Handle(context.Background(), newRecord("m"))
		switch {
		case err == nil:
			accepted++
		case errors.Is(err, handler.ErrBufferFull):
			dropped++
		default:
			t.Fatalf("unexpected error: %v", err)
		}
	}

	require.Equal(t, dropped, buffered.Dropped(), "Dropped() must match the rejections observed by callers")
	require.Equal(t, uint64(attempted), accepted+dropped)
	require.Positive(t, dropped, "a buffer of 4 with a blocked worker must overflow")

	close(release)
	require.NoError(t, buffered.Shutdown(testCtx(t)))
	require.Equal(t, int(accepted), rec.len(), "every accepted record must be delivered")
}

func TestBufferedHandler_Stress_NoRecordUnaccounted(t *testing.T) {
	rec := newRecorder()
	buffered := handler.NewBufferedHandler(rec, 16)

	const (
		producers = 100
		perProd   = 1000
		attempted = producers * perProd
	)

	var accepted, dropped, rejected atomic.Uint64
	var wg sync.WaitGroup
	for p := 0; p < producers; p++ {
		wg.Add(1)
		go func(p int) {
			defer wg.Done()
			h := buffered.WithAttrs([]slog.Attr{slog.Int("producer", p)})
			for i := 0; i < perProd; i++ {
				err := h.Handle(context.Background(), newRecord("m"))
				switch {
				case err == nil:
					accepted.Add(1)
				case errors.Is(err, handler.ErrBufferFull):
					dropped.Add(1)
				case errors.Is(err, handler.ErrShutdown):
					rejected.Add(1)
				default:
					panic(err)
				}
			}
		}(p)
	}
	wg.Wait()

	require.NoError(t, buffered.Shutdown(testCtx(t)))

	delivered := uint64(rec.len())
	require.Equal(t, uint64(attempted), accepted.Load()+dropped.Load()+rejected.Load(),
		"every attempted record must be accounted for")
	require.Equal(t, accepted.Load(), delivered,
		"delivered=%d accepted=%d dropped=%d rejected=%d",
		delivered, accepted.Load(), dropped.Load(), rejected.Load())
	require.Equal(t, uint64(attempted), delivered+dropped.Load()+rejected.Load())
	require.Equal(t, dropped.Load(), buffered.Dropped())
	require.Zero(t, rejected.Load(), "no shutdown happens while producers are running")
}

func TestBufferedHandler_Stress_ShutdownMidFlight(t *testing.T) {
	rec := newRecorder()
	buffered := handler.NewBufferedHandler(rec, 16)

	const producers = 50
	var accepted, dropped, rejected atomic.Uint64
	var wg sync.WaitGroup
	stop := make(chan struct{})

	for p := 0; p < producers; p++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				err := buffered.Handle(context.Background(), newRecord("m"))
				switch {
				case err == nil:
					accepted.Add(1)
				case errors.Is(err, handler.ErrBufferFull):
					dropped.Add(1)
				case errors.Is(err, handler.ErrShutdown):
					rejected.Add(1)
				default:
					panic(err)
				}
			}
		}()
	}

	time.Sleep(50 * time.Millisecond)
	require.NoError(t, buffered.Shutdown(testCtx(t)))
	close(stop)
	wg.Wait()

	require.Equal(t, int(accepted.Load()), rec.len(),
		"every record accepted before Shutdown must be delivered (accepted=%d delivered=%d dropped=%d rejected=%d)",
		accepted.Load(), rec.len(), dropped.Load(), rejected.Load())
	require.Positive(t, rejected.Load(), "producers running after Shutdown must be rejected, not dropped silently")
}

// ---------------------------------------------------------------------------
// downstream errors
// ---------------------------------------------------------------------------

func TestBufferedHandler_ReportsDownstreamErrorOnFlush(t *testing.T) {
	downstreamErr := errors.New("sink unavailable")
	rec := newRecorder().withErr(downstreamErr)
	buffered := handler.NewBufferedHandler(rec, 16)
	t.Cleanup(func() { _ = buffered.Shutdown(testCtx(t)) })

	require.NoError(t, buffered.Handle(context.Background(), newRecord("m")))

	err := buffered.Flush(testCtx(t))
	require.ErrorIs(t, err, downstreamErr,
		"a downstream failure must not be silently discarded by the worker")
	require.Equal(t, uint64(1), buffered.HandlerErrors())

	// Consume-once: the same failure is not reported again.
	require.NoError(t, buffered.Flush(testCtx(t)))
	require.Equal(t, uint64(1), buffered.HandlerErrors(), "the monotonic counter must not be consumed")
}

func TestBufferedHandler_ReportsDownstreamErrorOnShutdown(t *testing.T) {
	downstreamErr := errors.New("sink unavailable")
	rec := newRecorder().withErr(downstreamErr)
	buffered := handler.NewBufferedHandler(rec, 16)

	require.NoError(t, buffered.Handle(context.Background(), newRecord("m")))

	require.ErrorIs(t, buffered.Shutdown(testCtx(t)), downstreamErr)
	// Second Shutdown has nothing new to report.
	require.NoError(t, buffered.Shutdown(testCtx(t)))
}

// ---------------------------------------------------------------------------
// preserved behavior from the original suite
// ---------------------------------------------------------------------------

func TestBufferedHandler_EnqueuesAndDeliversInOrder(t *testing.T) {
	rec := newRecorder()
	buffered := handler.NewBufferedHandler(rec, 10)
	t.Cleanup(func() { _ = buffered.Shutdown(testCtx(t)) })

	for i := 0; i < 5; i++ {
		msg := "msg-" + string(rune('A'+i))
		require.NoError(t, buffered.Handle(context.Background(), newRecord(msg)))
	}
	require.NoError(t, buffered.Flush(testCtx(t)))

	snap := rec.snapshot()
	require.Len(t, snap, 5)
	require.Equal(t, "msg-A", snap[0].Message)
	require.Equal(t, "msg-E", snap[4].Message)
}

func TestBufferedHandler_BufferOverflowReturnsError(t *testing.T) {
	release := make(chan struct{})
	rec := newRecorder().withRelease(release)
	buffered := handler.NewBufferedHandler(rec, 2)
	t.Cleanup(func() {
		close(release)
		_ = buffered.Shutdown(testCtx(t))
	})

	var overflowErr error
	for i := 0; i < 20; i++ {
		if err := buffered.Handle(context.Background(), newRecord("msg")); err != nil {
			require.ErrorContains(t, err, "buffer full")
			overflowErr = err
			break
		}
	}
	require.Error(t, overflowErr, "expected at least one Handle to return buffer full")
	require.ErrorIs(t, overflowErr, handler.ErrBufferFull)
}

func TestBufferedHandler_Enabled(t *testing.T) {
	buffered := handler.NewBufferedHandler(newRecorder(), 5)
	t.Cleanup(func() { _ = buffered.Shutdown(testCtx(t)) })

	for _, level := range []slog.Level{slog.LevelDebug, slog.LevelInfo, slog.LevelWarn, slog.LevelError} {
		assert.True(t, buffered.Enabled(context.Background(), level),
			"BufferedHandler.Enabled should delegate to next handler")
	}
}

func TestBufferedHandler_WithAttrs_DeliversAttributes(t *testing.T) {
	rec := newRecorder()
	buffered := handler.NewBufferedHandler(rec, 5)
	t.Cleanup(func() { _ = buffered.Shutdown(testCtx(t)) })

	derived := buffered.WithAttrs([]slog.Attr{
		slog.String("key1", "value1"),
		slog.Int("key2", 42),
		slog.Bool("key3", true),
	})
	assert.IsType(t, &handler.BufferedHandler{}, derived)

	require.NoError(t, derived.Handle(context.Background(), newRecord("test message")))
	require.NoError(t, buffered.Flush(testCtx(t)))
	require.Len(t, rec.snapshot(), 1)
}

func TestBufferedHandler_WithGroup_Delivers(t *testing.T) {
	rec := newRecorder()
	buffered := handler.NewBufferedHandler(rec, 5)
	t.Cleanup(func() { _ = buffered.Shutdown(testCtx(t)) })

	derived := buffered.WithGroup("test_group")
	assert.IsType(t, &handler.BufferedHandler{}, derived)

	require.NoError(t, derived.Handle(context.Background(), newRecord("test message")))
	require.NoError(t, buffered.Flush(testCtx(t)))

	snap := rec.snapshot()
	require.Len(t, snap, 1)
	require.Equal(t, "test message", snap[0].Message)
}

func TestBufferedHandler_WithAttrs_EmptyAttrs(t *testing.T) {
	rec := newRecorder()
	buffered := handler.NewBufferedHandler(rec, 5)
	t.Cleanup(func() { _ = buffered.Shutdown(testCtx(t)) })

	derived := buffered.WithAttrs([]slog.Attr{})
	require.NotNil(t, derived)
	require.NoError(t, derived.Handle(context.Background(), newRecord("test")))
	require.NoError(t, buffered.Flush(testCtx(t)))
	require.Len(t, rec.snapshot(), 1)
}

func TestBufferedHandler_WithGroup_EmptyGroup(t *testing.T) {
	rec := newRecorder()
	buffered := handler.NewBufferedHandler(rec, 5)
	t.Cleanup(func() { _ = buffered.Shutdown(testCtx(t)) })

	derived := buffered.WithGroup("")
	require.NotNil(t, derived)
	require.NoError(t, derived.Handle(context.Background(), newRecord("test")))
	require.NoError(t, buffered.Flush(testCtx(t)))
	require.Len(t, rec.snapshot(), 1)
}

// ---------------------------------------------------------------------------
// Flush racing Shutdown (review of #20)
// ---------------------------------------------------------------------------

// A Flush that observed the running state must not be able to enqueue its
// barrier after the drain already decided the queue was empty. Enqueuing the
// barrier participates in the same read-lock handshake as Handle, so the only
// two admissible outcomes are:
//
//   - the barrier was accepted while running: Flush waits for its checkpoint
//     and reports nil or the downstream error;
//   - Shutdown won: Flush reports ErrShutdown.
//
// Never a hang, never a panic, never an unprocessed barrier.
func TestBufferedHandler_FlushConcurrentWithShutdown(t *testing.T) {
	for i := 0; i < 200; i++ {
		func() {
			rec := newRecorder().withDelay(50 * time.Microsecond)
			buffered := handler.NewBufferedHandler(rec, 8)

			// Keep the worker busy so the queue is genuinely in play.
			for j := 0; j < 8; j++ {
				_ = buffered.Handle(context.Background(), newRecord("m"))
			}

			var flushErr error
			var wg sync.WaitGroup
			wg.Add(2)
			start := make(chan struct{})

			go func() {
				defer wg.Done()
				<-start
				flushErr = buffered.Flush(ctxWithTimeout(t, 5*time.Second))
			}()
			go func() {
				defer wg.Done()
				<-start
				_ = buffered.Shutdown(ctxWithTimeout(t, 5*time.Second))
			}()

			close(start)
			wg.Wait()

			switch {
			case flushErr == nil:
				// The barrier was processed: everything accepted before the
				// Flush must have been delivered.
				require.Equal(t, 8, rec.len(),
					"iteration %d: Flush returned nil but records were not delivered", i)
			case errors.Is(flushErr, handler.ErrShutdown):
				// Shutdown won the race. Admissible.
			default:
				t.Fatalf("iteration %d: undefined Flush outcome: %v", i, flushErr)
			}

			// The observable invariant: nothing may be left behind. An
			// orphaned barrier — one enqueued after the drain decided the
			// queue was empty — shows up here.
			require.Zero(t, buffered.Queued(),
				"iteration %d: %d item(s) left unprocessed after Shutdown (orphaned flush barrier)",
				i, buffered.Queued())
		}()
	}
}

// hungSink signals when the worker has entered Handle, then blocks until
// released. It is the only way to construct a genuinely full queue: without
// the signal the worker dequeues as soon as it is scheduled and frees a slot,
// which makes a starvation test pass vacuously.
type hungSink struct {
	entered chan struct{}
	once    sync.Once
	release chan struct{}
}

func newHungSink() *hungSink {
	return &hungSink{entered: make(chan struct{}), release: make(chan struct{})}
}

func (h *hungSink) Enabled(context.Context, slog.Level) bool { return true }
func (h *hungSink) Handle(context.Context, slog.Record) error {
	h.once.Do(func() { close(h.entered) })
	<-h.release
	return nil
}
func (h *hungSink) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *hungSink) WithGroup(string) slog.Handler      { return h }

// Enqueuing the flush barrier must hold the read lock, but the hold must be
// bounded. Holding it across an unbounded blocking send lets a hung wrapped
// handler starve Shutdown of the write lock, so that Shutdown cannot even set
// the state and stops honouring its own context.
func TestBufferedHandler_ShutdownIsNotStarvedByBlockedFlush(t *testing.T) {
	sink := newHungSink()
	buffered := handler.NewBufferedHandler(sink, 2)
	t.Cleanup(func() { close(sink.release) })

	// Get the worker inside the hung sink, then fill the queue so it can
	// never drain and the barrier send can never succeed.
	require.NoError(t, buffered.Handle(context.Background(), newRecord("blocking")))
	<-sink.entered
	for buffered.Dropped() == 0 {
		_ = buffered.Handle(context.Background(), newRecord("filler"))
	}
	require.Equal(t, 2, buffered.Queued(), "the queue must be full for this test to mean anything")

	// A Flush with no deadline against a permanently full queue.
	go func() { _ = buffered.Flush(context.Background()) }()
	time.Sleep(100 * time.Millisecond) // let it reach the barrier send

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- buffered.Shutdown(ctx) }()

	select {
	case err := <-done:
		require.ErrorIs(t, err, context.DeadlineExceeded,
			"Shutdown must honour its own context even while a Flush is blocked")
	case <-time.After(5 * time.Second):
		t.Fatal("Shutdown was starved: it could not acquire the write lock held by a blocked Flush")
	}
}

// After Shutdown returns, nothing may be left in the queue. An orphaned flush
// barrier — one enqueued after the drain decided the queue was empty — is the
// observable symptom of releasing the read lock between the state check and
// the barrier send. Measured at 0.03% of iterations before the fix.
func TestBufferedHandler_ShutdownLeavesNothingQueued(t *testing.T) {
	for i := 0; i < 20000; i++ {
		rec := newRecorder()
		buffered := handler.NewBufferedHandler(rec, 4)

		var wg sync.WaitGroup
		wg.Add(2)
		start := make(chan struct{})
		go func() {
			defer wg.Done()
			<-start
			_ = buffered.Flush(ctxWithTimeout(t, 5*time.Second))
		}()
		go func() {
			defer wg.Done()
			<-start
			_ = buffered.Shutdown(ctxWithTimeout(t, 5*time.Second))
		}()
		close(start)
		wg.Wait()

		require.Zero(t, buffered.Queued(),
			"iteration %d: an item was left unprocessed after Shutdown (orphaned flush barrier)", i)
	}
}

// The consume-once contract for downstream errors, pinned under concurrency:
// exactly one of N concurrent Flush callers receives the error, the rest get
// nil, and the monotonic counter is never consumed.
func TestBufferedHandler_ConcurrentFlushReportsDownstreamErrorExactlyOnce(t *testing.T) {
	downstreamErr := errors.New("sink unavailable")
	rec := newRecorder().withErr(downstreamErr)
	buffered := handler.NewBufferedHandler(rec, 16)
	t.Cleanup(func() { _ = buffered.Shutdown(testCtx(t)) })

	require.NoError(t, buffered.Handle(context.Background(), newRecord("m")))
	// Make sure the record has been delivered (and failed) before flushing.
	require.ErrorIs(t, buffered.Flush(testCtx(t)), downstreamErr)

	// Now produce one more failure and race five Flush callers for it.
	require.NoError(t, buffered.Handle(context.Background(), newRecord("m")))

	const callers = 5
	errs := make([]error, callers)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			errs[i] = buffered.Flush(ctxWithTimeout(t, 5*time.Second))
		}(i)
	}
	close(start)
	wg.Wait()

	reported := 0
	for i, err := range errs {
		switch {
		case err == nil:
		case errors.Is(err, downstreamErr):
			reported++
		default:
			t.Fatalf("caller %d: unexpected error %v", i, err)
		}
	}
	require.Equal(t, 1, reported,
		"a downstream failure must be surfaced to exactly one Flush caller (consume-once)")
	require.Equal(t, uint64(2), buffered.HandlerErrors(),
		"the monotonic counter must count every failure and never be consumed")
}

func ctxWithTimeout(t *testing.T, d time.Duration) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), d)
	t.Cleanup(cancel)
	return ctx
}
