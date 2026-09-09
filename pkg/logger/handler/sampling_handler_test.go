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
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// helpers
//
// These are named with a sam* prefix to stay disjoint from the doubles in the
// other test files of this package.
// ---------------------------------------------------------------------------

// samSink counts and records what reaches it. Derived sinks share one counter,
// so attribute derivation does not hide deliveries.
type samSink struct {
	mu       sync.Mutex
	messages []string
	onHandle func(context.Context, slog.Record)
}

func (s *samSink) Enabled(context.Context, slog.Level) bool { return true }

func (s *samSink) Handle(ctx context.Context, r slog.Record) error {
	if s.onHandle != nil {
		s.onHandle(ctx, r)
	}
	s.mu.Lock()
	s.messages = append(s.messages, r.Message)
	s.mu.Unlock()
	return nil
}

func (s *samSink) WithAttrs([]slog.Attr) slog.Handler { return s }
func (s *samSink) WithGroup(string) slog.Handler      { return s }

func (s *samSink) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.messages)
}

func (s *samSink) countOf(msg string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, m := range s.messages {
		if m == msg {
			n++
		}
	}
	return n
}

func samRecord(level slog.Level, msg string) slog.Record {
	return slog.NewRecord(time.Now(), level, msg, 0)
}

// samSettledGoroutines polls until the goroutine count stops changing.
func samSettledGoroutines() int {
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

// samClock is a deterministic, settable clock.
type samClock struct {
	mu  sync.Mutex
	now time.Time
}

func newSamClock() *samClock {
	return &samClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
}
func (c *samClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}
func (c *samClock) advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

// samBackwardsClock returns a strictly *decreasing* timestamp on every call.
// It models the worst case the KITLOG-GO-004 timestamp defect exposed: a
// reading that is older than one already stored in the sampling state.
type samBackwardsClock struct {
	base time.Time
	n    atomic.Int64
}

func newSamBackwardsClock() *samBackwardsClock {
	return &samBackwardsClock{base: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
}
func (c *samBackwardsClock) Now() time.Time {
	return c.base.Add(-time.Duration(c.n.Add(1)) * time.Millisecond)
}

// ---------------------------------------------------------------------------
// MinLevel
// ---------------------------------------------------------------------------

func TestSamplingHandler_BelowMinLevel_BypassesSampling(t *testing.T) {
	sink := &samSink{}
	clock := newSamClock()
	h := handler.NewSamplingHandler(sink, handler.SamplingConfig{
		Interval:    time.Hour,
		MinLevel:    slog.LevelWarn,
		Probability: 1,
		Now:         clock.Now,
	})

	// Info is below MinLevel: never sampled, even with a one-hour interval.
	for i := 0; i < 50; i++ {
		require.NoError(t, h.Handle(context.Background(), samRecord(slog.LevelInfo, "info")))
	}
	require.Equal(t, 50, sink.count())
	require.Zero(t, h.Suppressed())
}

func TestSamplingHandler_AtMinLevel_IsSampled(t *testing.T) {
	sink := &samSink{}
	clock := newSamClock()
	h := handler.NewSamplingHandler(sink, handler.SamplingConfig{
		Interval:    time.Hour,
		MinLevel:    slog.LevelWarn,
		Probability: 1,
		Now:         clock.Now,
	})

	for i := 0; i < 50; i++ {
		require.NoError(t, h.Handle(context.Background(), samRecord(slog.LevelWarn, "warn")))
	}
	require.Equal(t, 1, sink.count(), "the interval must admit exactly one record per key")
	require.Equal(t, uint64(49), h.Suppressed())
}

// ---------------------------------------------------------------------------
// sampling key semantics — the headline defect
// ---------------------------------------------------------------------------

func TestSamplingHandler_DistinctMessagesAtSameLevelDoNotSuppressEachOther(t *testing.T) {
	sink := &samSink{}
	clock := newSamClock()
	h := handler.NewSamplingHandler(sink, handler.SamplingConfig{
		Interval:    time.Minute,
		MinLevel:    slog.LevelWarn,
		Probability: 1,
		Now:         clock.Now,
	})

	ctx := context.Background()
	require.NoError(t, h.Handle(ctx, samRecord(slog.LevelWarn, "database timeout")))
	require.NoError(t, h.Handle(ctx, samRecord(slog.LevelWarn, "cache unavailable")))
	require.NoError(t, h.Handle(ctx, samRecord(slog.LevelWarn, "circuit breaker opened")))
	require.NoError(t, h.Handle(ctx, samRecord(slog.LevelWarn, "certificate expiring")))

	require.Equal(t, 4, sink.count(),
		"a WARN with sampling key A must not suppress a WARN with sampling key B")
	require.Equal(t, 1, sink.countOf("database timeout"))
	require.Equal(t, 1, sink.countOf("cache unavailable"))
	require.Equal(t, 1, sink.countOf("circuit breaker opened"))
	require.Equal(t, 1, sink.countOf("certificate expiring"))
	require.Zero(t, h.Suppressed())
}

func TestSamplingHandler_SameMessageWithinIntervalIsSuppressed(t *testing.T) {
	sink := &samSink{}
	clock := newSamClock()
	h := handler.NewSamplingHandler(sink, handler.SamplingConfig{
		Interval:    time.Minute,
		MinLevel:    slog.LevelWarn,
		Probability: 1,
		Now:         clock.Now,
	})

	ctx := context.Background()
	for i := 0; i < 10; i++ {
		require.NoError(t, h.Handle(ctx, samRecord(slog.LevelWarn, "database timeout")))
		clock.advance(time.Second)
	}
	require.Equal(t, 1, sink.count())

	clock.advance(time.Minute)
	require.NoError(t, h.Handle(ctx, samRecord(slog.LevelWarn, "database timeout")))
	require.Equal(t, 2, sink.count(), "the key must be admitted again once the interval elapsed")
}

func TestSamplingHandler_SameMessageDifferentLevelsAreIndependent(t *testing.T) {
	sink := &samSink{}
	clock := newSamClock()
	h := handler.NewSamplingHandler(sink, handler.SamplingConfig{
		Interval:    time.Minute,
		MinLevel:    slog.LevelInfo,
		Probability: 1,
		Now:         clock.Now,
	})

	ctx := context.Background()
	require.NoError(t, h.Handle(ctx, samRecord(slog.LevelInfo, "same text")))
	require.NoError(t, h.Handle(ctx, samRecord(slog.LevelWarn, "same text")))
	require.NoError(t, h.Handle(ctx, samRecord(slog.LevelError, "same text")))
	require.Equal(t, 3, sink.count(), "the default key includes the level")
}

func TestSamplingHandler_CustomKeyFunc(t *testing.T) {
	sink := &samSink{}
	clock := newSamClock()
	// Key by an explicit attribute, ignoring the message entirely.
	h := handler.NewSamplingHandler(sink, handler.SamplingConfig{
		Interval:    time.Minute,
		MinLevel:    slog.LevelInfo,
		Probability: 1,
		Now:         clock.Now,
		KeyFunc: func(_ context.Context, r slog.Record) string {
			key := "none"
			r.Attrs(func(a slog.Attr) bool {
				if a.Key == "sampling_key" {
					key = a.Value.String()
					return false
				}
				return true
			})
			return key
		},
	})

	ctx := context.Background()
	mk := func(msg, k string) slog.Record {
		r := samRecord(slog.LevelInfo, msg)
		r.AddAttrs(slog.String("sampling_key", k))
		return r
	}
	require.NoError(t, h.Handle(ctx, mk("first text", "A")))
	require.NoError(t, h.Handle(ctx, mk("different text", "A"))) // same key: suppressed
	require.NoError(t, h.Handle(ctx, mk("third text", "B")))     // different key: emitted

	require.Equal(t, 2, sink.count())
	require.Equal(t, 1, sink.countOf("first text"))
	require.Equal(t, 0, sink.countOf("different text"))
	require.Equal(t, 1, sink.countOf("third text"))
}

func TestSamplingHandler_DefaultSamplingKeyIsLevelAndMessage(t *testing.T) {
	a := handler.DefaultSamplingKey(context.Background(), samRecord(slog.LevelWarn, "msg"))
	b := handler.DefaultSamplingKey(context.Background(), samRecord(slog.LevelWarn, "msg"))
	c := handler.DefaultSamplingKey(context.Background(), samRecord(slog.LevelWarn, "other"))
	d := handler.DefaultSamplingKey(context.Background(), samRecord(slog.LevelError, "msg"))

	require.Equal(t, a, b)
	require.NotEqual(t, a, c, "the key must depend on the message")
	require.NotEqual(t, a, d, "the key must depend on the level")
	require.Contains(t, a, "WARN")
	require.Contains(t, a, "msg")
}

// ---------------------------------------------------------------------------
// probability
// ---------------------------------------------------------------------------

func TestSamplingHandler_ZeroProbabilityMeansUnsetAndEmitsEverything(t *testing.T) {
	sink := &samSink{}
	// Probability left at its zero value, as logger.Config{Sampling:{Enabled:true}}
	// produces. On HEAD this discarded every record at or above MinLevel.
	h := handler.NewSamplingHandler(sink, handler.SamplingConfig{
		MinLevel: slog.LevelInfo,
	})

	ctx := context.Background()
	for i := 0; i < 100; i++ {
		require.NoError(t, h.Handle(ctx, samRecord(slog.LevelInfo, fmt.Sprintf("m%d", i))))
	}
	require.Equal(t, 100, sink.count(),
		"an unset Probability must not silence the service")
	require.Zero(t, h.Suppressed())
}

func TestSamplingHandler_ProbabilityGateIsDeterministicWithInjectedRand(t *testing.T) {
	ctx := context.Background()

	t.Run("rand above probability drops", func(t *testing.T) {
		sink := &samSink{}
		h := handler.NewSamplingHandler(sink, handler.SamplingConfig{
			MinLevel:    slog.LevelInfo,
			Probability: 0.5,
			Rand:        func() float64 { return 0.9 },
		})
		for i := 0; i < 100; i++ {
			require.NoError(t, h.Handle(ctx, samRecord(slog.LevelInfo, fmt.Sprintf("m%d", i))))
		}
		require.Equal(t, 0, sink.count())
		require.Equal(t, uint64(100), h.Suppressed())
	})

	t.Run("rand below probability emits", func(t *testing.T) {
		sink := &samSink{}
		h := handler.NewSamplingHandler(sink, handler.SamplingConfig{
			MinLevel:    slog.LevelInfo,
			Probability: 0.5,
			Rand:        func() float64 { return 0.1 },
		})
		for i := 0; i < 100; i++ {
			require.NoError(t, h.Handle(ctx, samRecord(slog.LevelInfo, fmt.Sprintf("m%d", i))))
		}
		require.Equal(t, 100, sink.count())
		require.Zero(t, h.Suppressed())
	})

	t.Run("boundary rand equal to probability drops", func(t *testing.T) {
		sink := &samSink{}
		h := handler.NewSamplingHandler(sink, handler.SamplingConfig{
			MinLevel:    slog.LevelInfo,
			Probability: 0.5,
			Rand:        func() float64 { return 0.5 },
		})
		require.NoError(t, h.Handle(ctx, samRecord(slog.LevelInfo, "m")))
		require.Equal(t, 0, sink.count(), "the gate admits rand < probability")
	})

	t.Run("probability one never consults rand", func(t *testing.T) {
		sink := &samSink{}
		var calls atomic.Int64
		h := handler.NewSamplingHandler(sink, handler.SamplingConfig{
			MinLevel:    slog.LevelInfo,
			Probability: 1,
			Rand:        func() float64 { calls.Add(1); return 0.99 },
		})
		for i := 0; i < 100; i++ {
			require.NoError(t, h.Handle(ctx, samRecord(slog.LevelInfo, fmt.Sprintf("m%d", i))))
		}
		require.Equal(t, 100, sink.count())
		require.Zero(t, calls.Load(), "Probability 1 must not pay for randomness")
	})
}

// ---------------------------------------------------------------------------
// validation
// ---------------------------------------------------------------------------

func TestSamplingConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     handler.SamplingConfig
		wantErr []string
	}{
		{"zero value is valid", handler.SamplingConfig{}, nil},
		{"valid", handler.SamplingConfig{Interval: time.Second, Probability: 0.5, MaxKeys: 10}, nil},
		{"probability above one", handler.SamplingConfig{Probability: 1.5}, []string{"Probability"}},
		{"probability negative", handler.SamplingConfig{Probability: -0.1}, []string{"Probability"}},
		{"interval negative", handler.SamplingConfig{Interval: -time.Second}, []string{"Interval"}},
		{"maxkeys negative", handler.SamplingConfig{MaxKeys: -1}, []string{"MaxKeys"}},
		{
			"all at once",
			handler.SamplingConfig{Interval: -time.Second, Probability: 2, MaxKeys: -5},
			[]string{"Interval", "Probability", "MaxKeys"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.cfg.Validate()
			if tc.wantErr == nil {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			for _, want := range tc.wantErr {
				require.Contains(t, err.Error(), want,
					"Validate must report every invalid field, not just the first")
			}
		})
	}
}

func TestSamplingHandler_InvalidConfigClampsToEmitting(t *testing.T) {
	sink := &samSink{}
	h, err := handler.NewSamplingHandlerWithError(sink, handler.SamplingConfig{
		Interval:    -time.Hour,
		Probability: 42,
		MaxKeys:     -1,
		MinLevel:    slog.LevelInfo,
	})
	require.Error(t, err, "invalid configuration must be reported")
	require.NotNil(t, h, "the handler must remain usable so no record is lost")

	ctx := context.Background()
	for i := 0; i < 100; i++ {
		require.NoError(t, h.Handle(ctx, samRecord(slog.LevelInfo, "m")))
	}
	require.Equal(t, 100, sink.count(), "clamped defaults must emit, never silence")
}

// ---------------------------------------------------------------------------
// shared state across derivation
// ---------------------------------------------------------------------------

func TestSamplingHandler_SharesStateAcrossWithAttrs(t *testing.T) {
	sink := &samSink{}
	clock := newSamClock()
	root := handler.NewSamplingHandler(sink, handler.SamplingConfig{
		Interval:    time.Minute,
		MinLevel:    slog.LevelInfo,
		Probability: 1,
		Now:         clock.Now,
	})

	ctx := context.Background()
	require.NoError(t, root.Handle(ctx, samRecord(slog.LevelInfo, "repeated")))

	derived := root.WithAttrs([]slog.Attr{slog.String("request_id", "abc")})
	require.NoError(t, derived.Handle(ctx, samRecord(slog.LevelInfo, "repeated")))

	require.Equal(t, 1, sink.count(),
		"WithAttrs must share the sampling state; deriving a logger must not reset sampling")
	require.Equal(t, uint64(1), root.Suppressed())
}

func TestSamplingHandler_SharesStateAcrossWithGroup(t *testing.T) {
	sink := &samSink{}
	clock := newSamClock()
	root := handler.NewSamplingHandler(sink, handler.SamplingConfig{
		Interval:    time.Minute,
		MinLevel:    slog.LevelInfo,
		Probability: 1,
		Now:         clock.Now,
	})

	ctx := context.Background()
	require.NoError(t, root.Handle(ctx, samRecord(slog.LevelInfo, "repeated")))
	require.NoError(t, root.WithGroup("g").Handle(ctx, samRecord(slog.LevelInfo, "repeated")))

	require.Equal(t, 1, sink.count(), "WithGroup must share the sampling state")
}

func TestSamplingHandler_SharesStateAcrossDeepDerivationChain(t *testing.T) {
	sink := &samSink{}
	clock := newSamClock()
	root := handler.NewSamplingHandler(sink, handler.SamplingConfig{
		Interval:    time.Minute,
		MinLevel:    slog.LevelInfo,
		Probability: 1,
		Now:         clock.Now,
	})

	var h slog.Handler = root
	for i := 0; i < 100; i++ {
		h = h.WithAttrs([]slog.Attr{slog.Int("i", i)}).WithGroup(fmt.Sprintf("g%d", i))
	}

	ctx := context.Background()
	require.NoError(t, root.Handle(ctx, samRecord(slog.LevelInfo, "repeated")))
	require.NoError(t, h.Handle(ctx, samRecord(slog.LevelInfo, "repeated")))
	require.Equal(t, 1, sink.count())
}

// ---------------------------------------------------------------------------
// the lock must be released before the wrapped handler runs
// ---------------------------------------------------------------------------

func TestSamplingHandler_LockIsNotHeldDuringDownstreamHandle_Reentrant(t *testing.T) {
	sink := &samSink{}
	var h *handler.SamplingHandler

	// The downstream handler re-enters the sampler. If the sampler held its
	// mutex across next.Handle, this would deadlock on a non-reentrant mutex.
	var reentered atomic.Bool
	sink.onHandle = func(ctx context.Context, r slog.Record) {
		if reentered.CompareAndSwap(false, true) {
			_ = h.Handle(ctx, samRecord(slog.LevelInfo, "reentrant"))
		}
	}

	// A positive interval is required: with Interval 0 the sampler keeps no
	// state and takes no lock, so the test would pass vacuously.
	h = handler.NewSamplingHandler(sink, handler.SamplingConfig{
		Interval:    time.Hour,
		MinLevel:    slog.LevelInfo,
		Probability: 1,
	})

	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = h.Handle(context.Background(), samRecord(slog.LevelInfo, "outer"))
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("deadlock: the sampler's lock is held while the wrapped handler runs")
	}
	require.True(t, reentered.Load())
	require.Equal(t, 2, sink.count())
}

func TestSamplingHandler_LockIsNotHeldDuringDownstreamHandle_Concurrent(t *testing.T) {
	blocked := make(chan struct{})
	release := make(chan struct{})
	var blockedOnce sync.Once

	sink := &samSink{}
	sink.onHandle = func(_ context.Context, r slog.Record) {
		if r.Message == "slow" {
			blockedOnce.Do(func() { close(blocked) })
			<-release
		}
	}

	// A positive interval is required so the sampler actually takes its lock.
	h := handler.NewSamplingHandler(sink, handler.SamplingConfig{
		Interval:    time.Hour,
		MinLevel:    slog.LevelInfo,
		Probability: 1,
	})

	go func() { _ = h.Handle(context.Background(), samRecord(slog.LevelInfo, "slow")) }()
	<-blocked // the worker is now inside the wrapped handler

	// A different key must still be able to make its sampling decision.
	fast := make(chan struct{})
	go func() {
		defer close(fast)
		_ = h.Handle(context.Background(), samRecord(slog.LevelInfo, "fast"))
	}()

	select {
	case <-fast:
	case <-time.After(5 * time.Second):
		close(release)
		t.Fatal("a blocked downstream handler must not block unrelated sampling decisions")
	}
	close(release)
}

// ---------------------------------------------------------------------------
// concurrency: the timestamp-ordering regression (found while implementing #1)
// ---------------------------------------------------------------------------

// On HEAD, time.Now() was captured before acquiring the mutex, so a goroutine
// could compare its timestamp against a strictly later one written by the
// goroutine that won the lock. The subtraction went negative and, with
// Interval == 0, "negative < 0" discarded the record. Measured loss was up to
// 62% with sampling configured to drop nothing.
func TestSamplingHandler_ConcurrentSameKeyWithZeroIntervalDeliversEverything(t *testing.T) {
	const producers = 200

	sink := &samSink{}
	h := handler.NewSamplingHandler(sink, handler.SamplingConfig{
		Interval:    0,
		MinLevel:    slog.LevelInfo,
		Probability: 1,
	})

	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < producers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start // maximise contention on the sampler
			_ = h.Handle(context.Background(), samRecord(slog.LevelInfo, "same key"))
		}()
	}
	close(start)
	wg.Wait()

	require.Equal(t, producers, sink.count(),
		"Interval 0 and Probability 1 drop nothing: the answer is exactly %d, deterministically", producers)
	require.Zero(t, h.Suppressed())
}

func TestSamplingHandler_ConcurrentDifferentLevelsWithZeroIntervalDeliversEverything(t *testing.T) {
	const perLevel = 100
	levels := []slog.Level{slog.LevelInfo, slog.LevelWarn, slog.LevelError}

	sink := &samSink{}
	h := handler.NewSamplingHandler(sink, handler.SamplingConfig{
		Interval:    0,
		MinLevel:    slog.LevelInfo,
		Probability: 1,
	})

	var wg sync.WaitGroup
	start := make(chan struct{})
	for _, lvl := range levels {
		for i := 0; i < perLevel; i++ {
			wg.Add(1)
			go func(l slog.Level) {
				defer wg.Done()
				<-start
				_ = h.Handle(context.Background(), samRecord(l, "concurrent"))
			}(lvl)
		}
	}
	close(start)
	wg.Wait()

	require.Equal(t, perLevel*len(levels), sink.count())
	require.Zero(t, h.Suppressed())
}

// A clock that walks backwards is the adversarial form of the same defect: a
// timestamp older than the one already stored. Elapsed time is clamped at zero,
// so with Interval 0 nothing may be discarded.
func TestSamplingHandler_NonMonotonicClockNeverDropsWithZeroInterval(t *testing.T) {
	sink := &samSink{}
	h := handler.NewSamplingHandler(sink, handler.SamplingConfig{
		Interval:    0,
		MinLevel:    slog.LevelInfo,
		Probability: 1,
		Now:         newSamBackwardsClock().Now,
	})

	ctx := context.Background()
	for i := 0; i < 500; i++ {
		require.NoError(t, h.Handle(ctx, samRecord(slog.LevelInfo, "same key")))
	}
	require.Equal(t, 500, sink.count(),
		"a negative elapsed time must never discard a record")
	require.Zero(t, h.Suppressed())
}

func TestSamplingHandler_NonMonotonicClockConcurrentNeverDropsWithZeroInterval(t *testing.T) {
	const producers = 200

	sink := &samSink{}
	h := handler.NewSamplingHandler(sink, handler.SamplingConfig{
		Interval:    0,
		MinLevel:    slog.LevelInfo,
		Probability: 1,
		Now:         newSamBackwardsClock().Now,
	})

	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < producers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_ = h.Handle(context.Background(), samRecord(slog.LevelInfo, "same key"))
		}()
	}
	close(start)
	wg.Wait()

	require.Equal(t, producers, sink.count())
	require.Zero(t, h.Suppressed())
}

// With a frozen clock and a positive interval the answer is also exact: the
// first record for the key is admitted and every other one is suppressed.
func TestSamplingHandler_ConcurrentSameKeyWithFrozenClockAdmitsExactlyOne(t *testing.T) {
	const producers = 200

	sink := &samSink{}
	clock := newSamClock()
	h := handler.NewSamplingHandler(sink, handler.SamplingConfig{
		Interval:    time.Hour,
		MinLevel:    slog.LevelInfo,
		Probability: 1,
		Now:         clock.Now,
	})

	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < producers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_ = h.Handle(context.Background(), samRecord(slog.LevelInfo, "same key"))
		}()
	}
	close(start)
	wg.Wait()

	require.Equal(t, 1, sink.count())
	require.Equal(t, uint64(producers-1), h.Suppressed())
}

// ---------------------------------------------------------------------------
// bounded key state
// ---------------------------------------------------------------------------

func TestSamplingHandler_ZeroIntervalKeepsNoState(t *testing.T) {
	sink := &samSink{}
	h := handler.NewSamplingHandler(sink, handler.SamplingConfig{
		Interval:    0,
		MinLevel:    slog.LevelInfo,
		Probability: 1,
	})

	ctx := context.Background()
	for i := 0; i < 10_000; i++ {
		require.NoError(t, h.Handle(ctx, samRecord(slog.LevelInfo, fmt.Sprintf("event-%d", i))))
	}
	require.Equal(t, 10_000, sink.count())
	require.Zero(t, h.TrackedKeys(),
		"with no interval there is nothing to remember: the sampler must store no keys")
}

func TestSamplingHandler_TrackedKeysAreBounded(t *testing.T) {
	const (
		maxKeys  = 64
		distinct = 100_000
	)

	sink := &samSink{}
	clock := newSamClock()
	h := handler.NewSamplingHandler(sink, handler.SamplingConfig{
		Interval:    time.Hour, // nothing goes stale, so eviction must do the work
		MinLevel:    slog.LevelInfo,
		Probability: 1,
		Now:         clock.Now,
		MaxKeys:     maxKeys,
	})

	ctx := context.Background()
	for i := 0; i < distinct; i++ {
		require.NoError(t, h.Handle(ctx, samRecord(slog.LevelInfo, fmt.Sprintf("event-%d", i))))
	}

	require.LessOrEqual(t, h.TrackedKeys(), maxKeys,
		"%d distinct keys must not grow the sampling state past MaxKeys", distinct)
	require.Positive(t, h.Evicted(), "eviction must be observable")
	require.Equal(t, distinct, sink.count(), "distinct keys are all emitted; eviction must not drop records")
}

func TestSamplingHandler_StaleKeysAreReclaimedWithoutEviction(t *testing.T) {
	sink := &samSink{}
	clock := newSamClock()
	h := handler.NewSamplingHandler(sink, handler.SamplingConfig{
		Interval:    time.Second,
		MinLevel:    slog.LevelInfo,
		Probability: 1,
		Now:         clock.Now,
		MaxKeys:     8,
	})

	ctx := context.Background()
	for i := 0; i < 1000; i++ {
		require.NoError(t, h.Handle(ctx, samRecord(slog.LevelInfo, fmt.Sprintf("event-%d", i))))
		clock.advance(2 * time.Second) // every stored key is immediately stale
	}
	require.LessOrEqual(t, h.TrackedKeys(), 8)
	require.Equal(t, 1000, sink.count())
}

func TestSamplingHandler_DefaultMaxKeysIsApplied(t *testing.T) {
	sink := &samSink{}
	clock := newSamClock()
	h := handler.NewSamplingHandler(sink, handler.SamplingConfig{
		Interval:    time.Hour,
		MinLevel:    slog.LevelInfo,
		Probability: 1,
		Now:         clock.Now,
		// MaxKeys unset
	})

	ctx := context.Background()
	for i := 0; i < handler.DefaultSamplingMaxKeys*2; i++ {
		require.NoError(t, h.Handle(ctx, samRecord(slog.LevelInfo, fmt.Sprintf("event-%d", i))))
	}
	require.LessOrEqual(t, h.TrackedKeys(), handler.DefaultSamplingMaxKeys)
}

func TestSamplingHandler_EvictionStartsNoGoroutine(t *testing.T) {
	sink := &samSink{}
	clock := newSamClock()
	h := handler.NewSamplingHandler(sink, handler.SamplingConfig{
		Interval:    time.Hour,
		MinLevel:    slog.LevelInfo,
		Probability: 1,
		Now:         clock.Now,
		MaxKeys:     16,
	})

	before := samSettledGoroutines()
	ctx := context.Background()
	for i := 0; i < 50_000; i++ {
		require.NoError(t, h.Handle(ctx, samRecord(slog.LevelInfo, fmt.Sprintf("event-%d", i))))
	}
	after := samSettledGoroutines()
	require.LessOrEqual(t, after, before+1, "eviction must not spawn goroutines (before=%d after=%d)", before, after)
}

// ---------------------------------------------------------------------------
// counters
// ---------------------------------------------------------------------------

func TestSamplingHandler_SuppressedCounterIsExact(t *testing.T) {
	sink := &samSink{}
	clock := newSamClock()
	h := handler.NewSamplingHandler(sink, handler.SamplingConfig{
		Interval:    time.Hour,
		MinLevel:    slog.LevelInfo,
		Probability: 1,
		Now:         clock.Now,
	})

	ctx := context.Background()
	const n = 1000
	for i := 0; i < n; i++ {
		require.NoError(t, h.Handle(ctx, samRecord(slog.LevelInfo, "same")))
	}
	require.Equal(t, 1, sink.count())
	require.Equal(t, uint64(n-1), h.Suppressed())
	require.Equal(t, uint64(n), uint64(sink.count())+h.Suppressed(),
		"every record at or above MinLevel is either emitted or counted as suppressed")
}

func TestSamplingHandler_CountersAreSharedAcrossDerivedHandlers(t *testing.T) {
	sink := &samSink{}
	clock := newSamClock()
	root := handler.NewSamplingHandler(sink, handler.SamplingConfig{
		Interval:    time.Hour,
		MinLevel:    slog.LevelInfo,
		Probability: 1,
		Now:         clock.Now,
	})
	derived := root.WithAttrs([]slog.Attr{slog.String("k", "v")})

	ctx := context.Background()
	require.NoError(t, root.Handle(ctx, samRecord(slog.LevelInfo, "same")))
	for i := 0; i < 10; i++ {
		require.NoError(t, derived.Handle(ctx, samRecord(slog.LevelInfo, "same")))
	}
	require.Equal(t, uint64(10), root.Suppressed(),
		"suppression through a derived handler must be visible on the root")
}

// ---------------------------------------------------------------------------
// plumbing through slog.Logger
// ---------------------------------------------------------------------------

func TestSamplingHandler_ThroughSlogLogger(t *testing.T) {
	sink := &samSink{}
	clock := newSamClock()
	h := handler.NewSamplingHandler(sink, handler.SamplingConfig{
		Interval:    time.Minute,
		MinLevel:    slog.LevelWarn,
		Probability: 1,
		Now:         clock.Now,
	})
	lg := slog.New(h)

	lg.Warn("database timeout")
	lg.Warn("database timeout")
	lg.Warn("cache unavailable")
	lg.Info("routine")

	require.Equal(t, 1, sink.countOf("database timeout"))
	require.Equal(t, 1, sink.countOf("cache unavailable"))
	require.Equal(t, 1, sink.countOf("routine"), "Info is below MinLevel and bypasses sampling")
}

func TestSamplingHandler_ErrorFromDownstreamIsPropagated(t *testing.T) {
	want := errors.New("sink failed")
	h := handler.NewSamplingHandler(samFailingSink{want}, handler.SamplingConfig{
		MinLevel:    slog.LevelInfo,
		Probability: 1,
	})
	require.ErrorIs(t, h.Handle(context.Background(), samRecord(slog.LevelInfo, "m")), want)
}

type samFailingSink struct{ err error }

func (samFailingSink) Enabled(context.Context, slog.Level) bool    { return true }
func (s samFailingSink) Handle(context.Context, slog.Record) error { return s.err }
func (s samFailingSink) WithAttrs([]slog.Attr) slog.Handler        { return s }
func (s samFailingSink) WithGroup(string) slog.Handler             { return s }

func TestSamplingHandler_EnabledDelegatesToNext(t *testing.T) {
	h := handler.NewSamplingHandler(samDisabledSink{}, handler.SamplingConfig{})
	require.False(t, h.Enabled(context.Background(), slog.LevelError))

	h2 := handler.NewSamplingHandler(&samSink{}, handler.SamplingConfig{})
	require.True(t, h2.Enabled(context.Background(), slog.LevelDebug))
}

type samDisabledSink struct{}

func (samDisabledSink) Enabled(context.Context, slog.Level) bool  { return false }
func (samDisabledSink) Handle(context.Context, slog.Record) error { return nil }
func (s samDisabledSink) WithAttrs([]slog.Attr) slog.Handler      { return s }
func (s samDisabledSink) WithGroup(string) slog.Handler           { return s }
