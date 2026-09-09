package logger

import (
	"context"
	"errors"
	"log/slog"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pablogore/kit-logger/pkg/logger/handler"
)

// lifecycleRecorder is a leaf handler that records every record it is given and
// can be made deliberately slow, so a test can tell "Shutdown waited" apart from
// "Shutdown returned before the worker got there".
type lifecycleRecorder struct {
	delay time.Duration
	err   error

	mu   sync.Mutex
	msgs []string
}

func (r *lifecycleRecorder) Enabled(context.Context, slog.Level) bool { return true }

func (r *lifecycleRecorder) Handle(_ context.Context, record slog.Record) error {
	if r.delay > 0 {
		time.Sleep(r.delay)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.msgs = append(r.msgs, record.Message)
	return r.err
}

func (r *lifecycleRecorder) WithAttrs([]slog.Attr) slog.Handler { return r }

func (r *lifecycleRecorder) WithGroup(string) slog.Handler { return r }

func (r *lifecycleRecorder) messages() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.msgs...)
}

// newBufferedLogger builds the pipeline by hand — recorder behind a buffer,
// behind a hook — and hands it to New as a pre-built handler. The lifecycle
// handlers are then found through Unwrap, which is the path a consumer building
// its own chain takes.
func newBufferedLogger(t *testing.T, rec *lifecycleRecorder, size int) (ManagedLogger, *handler.BufferedHandler) {
	t.Helper()

	buffered := handler.NewBufferedHandler(rec, size)
	hooked := handler.NewHookHandler(buffered, func(ctx context.Context, _ slog.Record) (context.Context, bool) {
		return ctx, true
	})

	log, ok := New(Config{Handler: hooked}).(ManagedLogger)
	require.True(t, ok, "New must return a ManagedLogger")
	return log, buffered
}

func TestShutdown_DeliversEveryAcceptedRecord(t *testing.T) {
	rec := &lifecycleRecorder{delay: 5 * time.Millisecond}
	log, buffered := newBufferedLogger(t, rec, 64)

	const total = 20
	for i := 0; i < total; i++ {
		log.Info("queued")
	}

	require.NoError(t, log.Shutdown(context.Background()))

	assert.Len(t, rec.messages(), total, "Shutdown must return only after every accepted record is delivered")
	assert.Zero(t, buffered.Queued())
	assert.Zero(t, buffered.Dropped())
}

func TestShutdown_ExpiredContextDoesNotBlock(t *testing.T) {
	rec := &lifecycleRecorder{delay: time.Second}
	log, _ := newBufferedLogger(t, rec, 64)

	log.Info("slow")

	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- log.Shutdown(ctx) }()

	select {
	case err := <-done:
		assert.ErrorIs(t, err, context.DeadlineExceeded)
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Shutdown blocked on an already-expired context")
	}
}

func TestShutdown_IsIdempotentUnderConcurrency(t *testing.T) {
	rec := &lifecycleRecorder{}
	log, _ := newBufferedLogger(t, rec, 8)

	const callers = 100
	results := make([]error, callers)

	var wg sync.WaitGroup
	wg.Add(callers)
	for i := 0; i < callers; i++ {
		go func(i int) {
			defer wg.Done()
			results[i] = log.Shutdown(context.Background())
		}(i)
	}
	wg.Wait()

	for i, err := range results {
		assert.NoError(t, err, "caller %d saw a different result", i)
	}
}

func TestFlush_DrainsWithoutClosingTheLogger(t *testing.T) {
	rec := &lifecycleRecorder{delay: 2 * time.Millisecond}
	log, _ := newBufferedLogger(t, rec, 64)
	t.Cleanup(func() { _ = log.Shutdown(context.Background()) })

	for i := 0; i < 5; i++ {
		log.Info("before flush")
	}
	require.NoError(t, log.Flush(context.Background()))
	require.Len(t, rec.messages(), 5)

	log.Info("after flush")
	require.NoError(t, log.Flush(context.Background()))
	assert.Len(t, rec.messages(), 6, "Flush must not close the logger")
}

func TestFlush_ReportsDownstreamErrors(t *testing.T) {
	downstream := errors.New("downstream exploded")
	rec := &lifecycleRecorder{err: downstream}
	log, _ := newBufferedLogger(t, rec, 8)
	t.Cleanup(func() { _ = log.Shutdown(context.Background()) })

	log.Info("boom")

	assert.ErrorIs(t, log.Flush(context.Background()), downstream)
}

func TestLoggingAfterShutdown_IsRejectedAndSafe(t *testing.T) {
	rec := &lifecycleRecorder{}
	log, _ := newBufferedLogger(t, rec, 8)

	require.NoError(t, log.Shutdown(context.Background()))
	delivered := len(rec.messages())

	done := make(chan struct{})
	go func() {
		defer close(done)
		assert.NotPanics(t, func() {
			log.Info("after shutdown")
			log.Error("after shutdown")
			log.Log(context.Background(), slog.LevelWarn, "after shutdown")
		})
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("logging after Shutdown blocked")
	}

	assert.Len(t, rec.messages(), delivered, "no record may be delivered after Shutdown")
	assert.ErrorIs(t, log.Flush(context.Background()), ErrLoggerShutdown)
	assert.ErrorIs(t, log.Sync(), ErrLoggerShutdown)
}

func TestShutdown_FromDerivedLoggerDrainsTheSharedBuffer(t *testing.T) {
	rec := &lifecycleRecorder{delay: 2 * time.Millisecond}
	log, _ := newBufferedLogger(t, rec, 64)

	derived, ok := log.With("request_id", "abc").(ManagedLogger)
	require.True(t, ok, "a derived logger must stay a ManagedLogger")

	for i := 0; i < 10; i++ {
		log.Info("root")
	}
	require.NoError(t, derived.Shutdown(context.Background()))

	assert.Len(t, rec.messages(), 10, "Shutdown on a derived logger must drain the shared buffer")
	assert.ErrorIs(t, log.Flush(context.Background()), ErrLoggerShutdown, "the root logger must be shut down too")
}

// TestNew_CapturesTheBufferedHandlerBehindAHook pins the exact configuration
// that the old Sync() could never reach: the buffer is not the outermost
// handler, because a hook wraps it.
func TestNew_CapturesTheBufferedHandlerBehindAHook(t *testing.T) {
	log := New(Config{
		BufferSize: 8,
		Hook: func(ctx context.Context, _ slog.Record) (context.Context, bool) {
			return ctx, true
		},
	})

	sl, ok := log.(*SlogLogger)
	require.True(t, ok)
	require.NotNil(t, sl.lifecycle)
	require.Len(t, sl.lifecycle.handlers, 1, "New must capture the buffered handler it built")
	assert.IsType(t, &handler.BufferedHandler{}, sl.lifecycle.handlers[0])

	require.NoError(t, sl.Shutdown(context.Background()))
	assert.ErrorIs(t, sl.Flush(context.Background()), ErrLoggerShutdown)
}

func TestNew_WithoutBufferHasNoLifecycleHandlers(t *testing.T) {
	rec := &lifecycleRecorder{}
	log := New(Config{Handler: rec})

	sl, ok := log.(*SlogLogger)
	require.True(t, ok)
	assert.Empty(t, sl.lifecycle.handlers)

	assert.NoError(t, sl.Flush(context.Background()))
	assert.NoError(t, sl.Shutdown(context.Background()))
}

func TestExitWithFlush_DrainsBeforeExiting(t *testing.T) {
	rec := &lifecycleRecorder{delay: 5 * time.Millisecond}
	log, _ := newBufferedLogger(t, rec, 64)

	previousLogger := defaultLogger
	previousExit := exitFunc
	t.Cleanup(func() {
		defaultLogger = previousLogger
		exitFunc = previousExit
	})

	SetGlobal(log)

	var deliveredAtExit int
	var exitCode int
	exitFunc = func(code int) {
		deliveredAtExit = len(rec.messages())
		exitCode = code
	}

	for i := 0; i < 10; i++ {
		log.Info("pending")
	}
	ExitWithFlush(42)

	assert.Equal(t, 42, exitCode)
	assert.Equal(t, 10, deliveredAtExit, "ExitWithFlush must drain before calling exitFunc")
}

func TestCollectFlushers_TraversesFanOut(t *testing.T) {
	first := handler.NewBufferedHandler(&lifecycleRecorder{}, 4)
	second := handler.NewBufferedHandler(&lifecycleRecorder{}, 4)
	t.Cleanup(func() {
		_ = first.Shutdown(context.Background())
		_ = second.Shutdown(context.Background())
	})

	chain := handler.NewHookHandler(
		handler.NewMultiHandler(first, second),
		func(ctx context.Context, _ slog.Record) (context.Context, bool) { return ctx, true },
	)

	assert.Len(t, collectFlushers(chain), 2)
}

func TestCollectFlushers_StopsAtAnOpaqueHandler(t *testing.T) {
	// A third-party handler that does not implement Unwrap severs the chain.
	// That is the reason New captures what it builds instead of relying on
	// traversal.
	opaque := &lifecycleRecorder{}
	buffered := handler.NewBufferedHandler(opaque, 4)
	t.Cleanup(func() { _ = buffered.Shutdown(context.Background()) })

	assert.Len(t, collectFlushers(buffered), 1)
	assert.Empty(t, collectFlushers(opaque))
}

var (
	_ ManagedLogger = (*SlogLogger)(nil)
	_ ManagedLogger = (*MockLogger)(nil)
)

// TestShutdown_AccountsForEveryProducedRecord is the accounting proof: with
// producers still logging while Shutdown runs, every record a consumer
// submitted ends up in exactly one bucket — delivered, rejected by the buffer,
// dropped for lack of room, or rejected by the shut-down logger.
func TestShutdown_AccountsForEveryProducedRecord(t *testing.T) {
	rec := &lifecycleRecorder{}
	log, buffered := newBufferedLogger(t, rec, 16)

	sl, ok := log.(*SlogLogger)
	require.True(t, ok)

	const (
		producers          = 50
		recordsPerProducer = 40
		produced           = producers * recordsPerProducer
	)

	baseline := runtime.NumGoroutine()

	var wg sync.WaitGroup
	wg.Add(producers)
	for p := 0; p < producers; p++ {
		go func() {
			defer wg.Done()
			for i := 0; i < recordsPerProducer; i++ {
				log.Info("produced")
			}
		}()
	}

	// Shut down mid-flight, not after the producers are done.
	time.Sleep(time.Millisecond)
	shutdownErr := log.Shutdown(context.Background())
	wg.Wait()

	require.NoError(t, shutdownErr)

	delivered := len(rec.messages())
	total := uint64(delivered) + buffered.Rejected() + buffered.Dropped() + sl.Rejected()
	assert.Equal(t, uint64(produced), total,
		"delivered=%d bufferRejected=%d dropped=%d loggerRejected=%d",
		delivered, buffered.Rejected(), buffered.Dropped(), sl.Rejected())
	assert.Zero(t, buffered.Queued())

	assertGoroutinesSettle(t, baseline)
}

func assertGoroutinesSettle(t *testing.T, baseline int) {
	t.Helper()

	deadline := time.Now().Add(time.Second)
	for {
		current := runtime.NumGoroutine()
		if current <= baseline {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("goroutines did not settle: baseline %d, still %d", baseline, current)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestMockLogger_LifecycleIsHonest(t *testing.T) {
	mock := NewMockLogger()

	require.NoError(t, mock.Flush(context.Background()))
	require.NoError(t, mock.Sync())
	assert.Equal(t, 2, mock.FlushCalls(), "Sync must count as a Flush")

	mock.FlushErr = errors.New("flush failed")
	assert.ErrorIs(t, mock.Flush(context.Background()), mock.FlushErr)

	mock.Info("before shutdown")
	require.Len(t, mock.Entries, 1)

	require.NoError(t, mock.Shutdown(context.Background()))
	assert.Equal(t, 1, mock.ShutdownCalls())

	mock.Info("after shutdown")
	assert.Len(t, mock.Entries, 1, "a shut-down mock must not record new entries")
	assert.Equal(t, []error{ErrLoggerShutdown}, mock.RejectedErrors())
	assert.ErrorIs(t, mock.Flush(context.Background()), ErrLoggerShutdown)
}
