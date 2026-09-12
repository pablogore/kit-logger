package logger

import (
	"context"
	"errors"
	"fmt"
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

	// gate, when non-nil, blocks every delivery until it is closed. It lets a
	// test hold the drain open and observe the pipeline mid-shutdown.
	gate chan struct{}

	mu   sync.Mutex
	msgs []string
}

func (r *lifecycleRecorder) Enabled(context.Context, slog.Level) bool { return true }

func (r *lifecycleRecorder) Handle(_ context.Context, record slog.Record) error {
	if r.gate != nil {
		<-r.gate
	}
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

	previousLogger := peekGlobalForTest()
	previousExit := exitFunc
	t.Cleanup(func() {
		restoreGlobalForTest(previousLogger)
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

var _ ManagedLogger = (*SlogLogger)(nil)

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

// TestShutdown_AccountsForEveryProducedRecord_WithSamplingAndRateLimit extends
// TestShutdown_AccountsForEveryProducedRecord's exact-accounting invariant to
// a pipeline with sampling enabled, over 100 concurrent producers, each also
// exercising the rate limiter concurrently -- proving the combination does
// not silently lose a record anywhere in the chain, even when Shutdown lands
// mid-flight.
//
// Every producer's first call uses a fresh, producer-private rate-limit key:
// a fresh key's token bucket always starts full, so that call is guaranteed
// to be admitted regardless of scheduling. That keeps the invariant exact --
// no record can vanish into an unreported rate-limit suppression -- while
// still driving rateState's locking and bookkeeping from 100+ goroutines at
// once, concurrently with sampling and a mid-flight shutdown.
func TestShutdown_AccountsForEveryProducedRecord_WithSamplingAndRateLimit(t *testing.T) {
	rec := &lifecycleRecorder{}
	sampled := handler.NewSamplingHandler(rec, handler.SamplingConfig{
		Interval:    time.Hour,
		Probability: 0.5,
		MinLevel:    slog.LevelInfo,
	})
	buffered := handler.NewBufferedHandler(sampled, 16)

	log, ok := New(Config{Handler: buffered}).(*SlogLogger)
	require.True(t, ok)

	const (
		producers          = 120
		recordsPerProducer = 30
		produced           = producers * recordsPerProducer
	)

	baseline := runtime.NumGoroutine()

	var wg sync.WaitGroup
	wg.Add(producers)
	for p := 0; p < producers; p++ {
		p := p
		go func() {
			defer wg.Done()
			key := fmt.Sprintf("producer-%d", p)
			for i := 0; i < recordsPerProducer; i++ {
				if i == 0 {
					log.Info("produced", WithRateLimit(key, time.Hour))
					continue
				}
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
	total := uint64(delivered) + buffered.Rejected() + buffered.Dropped() + sampled.Suppressed() + log.Rejected()
	assert.Equal(t, uint64(produced), total,
		"delivered=%d bufferRejected=%d bufferDropped=%d samplingSuppressed=%d loggerRejected=%d",
		delivered, buffered.Rejected(), buffered.Dropped(), sampled.Suppressed(), log.Rejected())
	assert.Zero(t, buffered.Queued())
	assert.Zero(t, log.RateLimitConflicts(), "each producer used its own key, so no conflicting interval should ever be reported")

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

// TestWith_RepeatedCalls_DoNotLeakGoroutines pins down that deriving a child
// logger via With is a cheap, purely in-memory operation: it must not spawn
// any background goroutine per call. Only the BufferedHandler's single
// worker goroutine is expected to be alive throughout.
func TestWith_RepeatedCalls_DoNotLeakGoroutines(t *testing.T) {
	rec := &lifecycleRecorder{}
	log, _ := newBufferedLogger(t, rec, 64)

	baseline := runtime.NumGoroutine()

	const iterations = 10_000
	var child Logger = log
	for i := 0; i < iterations; i++ {
		child = child.With("iteration", i)
	}
	child.Info("final")

	require.NoError(t, log.Shutdown(context.Background()))

	assertGoroutinesSettle(t, baseline)
}

// countingCounterHook records how often each metric name was incremented.
type countingCounterHook struct {
	mu     sync.Mutex
	counts map[string]int
}

func (c *countingCounterHook) Inc(name string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.counts == nil {
		c.counts = make(map[string]int)
	}
	c.counts[name]++
}

func (c *countingCounterHook) count(name string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.counts[name]
}

// TestShutdown_CallerContextBoundsOnlyThatCaller pins the ownership split: the
// shutdown is one shared operation, and a context bounds how long its caller
// waits — not how long the drain is allowed to take.
//
// The impatient caller must not be able to freeze the result for the patient
// one, and it must not matter which of the two started the shutdown.
func TestShutdown_CallerContextBoundsOnlyThatCaller(t *testing.T) {
	rec := &lifecycleRecorder{delay: 20 * time.Millisecond}
	log, buffered := newBufferedLogger(t, rec, 64)

	sl, ok := log.(*SlogLogger)
	require.True(t, ok)

	// Roughly 200ms of drain, far beyond the impatient caller's deadline.
	const queued = 10
	for i := 0; i < queued; i++ {
		log.Info("queued")
	}

	impatientCtx, cancelImpatient := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancelImpatient()
	patientCtx, cancelPatient := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelPatient()

	// The impatient caller has to be the one that starts the shutdown, or the
	// test would pass by luck: if the patient caller started it, it would own
	// the drain and the assertions below would hold for the wrong reason.
	// Admission closes inside the start path, so isStopping is the signal that
	// the impatient caller got there first.
	impatient := make(chan error, 1)
	go func() { impatient <- log.Shutdown(impatientCtx) }()
	require.Eventually(t, sl.lifecycle.isStopping, time.Second, 100*time.Microsecond,
		"the impatient caller must be the one that starts the shutdown")

	patient := make(chan error, 1)
	go func() { patient <- log.Shutdown(patientCtx) }()

	assert.ErrorIs(t, <-impatient, context.DeadlineExceeded,
		"the impatient caller must hear about its own deadline")
	assert.NoError(t, <-patient,
		"the patient caller must get the real result, not the impatient caller's deadline")

	assert.Len(t, rec.messages(), queued,
		"an early deadline must not cut the drain short for everyone")
	assert.Zero(t, buffered.Queued())

	// A later caller inherits the completed result, not the expired deadline.
	assert.NoError(t, log.Shutdown(context.Background()))

	for i := 0; i < 5; i++ {
		log.Info("after shutdown")
	}
	assert.Equal(t, uint64(5), sl.Rejected())
	assert.Len(t, rec.messages(), queued)
	assert.Zero(t, buffered.Rejected(), "a rejected record must not have reached the buffer")
}

// TestShutdown_ClosesAdmissionBeforeDraining holds the drain open and checks the
// admission boundary from inside that window: a record submitted while the
// shutdown is in flight is rejected at the logger, so it consumes no
// rate-limit token, fires no counter, and never reaches the buffer.
func TestShutdown_ClosesAdmissionBeforeDraining(t *testing.T) {
	release := make(chan struct{})
	rec := &lifecycleRecorder{gate: release}
	counter := &countingCounterHook{}

	buffered := handler.NewBufferedHandler(rec, 8)
	hooked := handler.NewHookHandler(buffered, func(ctx context.Context, _ slog.Record) (context.Context, bool) {
		return ctx, true
	})

	sl, ok := New(Config{Handler: hooked}, WithCounterHook(counter)).(*SlogLogger)
	require.True(t, ok)

	sl.Info("accepted", WithCounter("emitted"))
	sl.Info("accepted", WithRateLimit("gate-key", time.Hour), WithCounter("emitted"))

	shutdownDone := make(chan error, 1)
	go func() { shutdownDone <- sl.Shutdown(context.Background()) }()

	require.Eventually(t, sl.lifecycle.isStopping, time.Second, time.Millisecond,
		"admission must close when the shutdown starts, not when it finishes")

	countBefore := counter.count("emitted")

	sl.Info("during drain", WithCounter("emitted"))
	sl.Info("during drain", WithRateLimit("gate-key", time.Hour), WithCounter("emitted"))

	assert.Equal(t, uint64(2), sl.Rejected())
	assert.Zero(t, buffered.Rejected(),
		"a record rejected at the logger must never reach the buffer")
	assert.Equal(t, countBefore, counter.count("emitted"),
		"a record submitted after the shutdown started must fire no counter")

	close(release)
	require.NoError(t, <-shutdownDone)
	assert.Len(t, rec.messages(), 2)
}
