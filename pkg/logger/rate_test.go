package logger

import (
	"runtime"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testEpoch is the instant every fake clock in this file starts from.
//
// It is deliberately not the zero time.Time: rate.Limiter measures how many
// tokens to restore from the distance between "now" and the last event, and a
// brand-new limiter has a zero last event. Driving it from the zero instant
// would therefore produce an elapsed of zero and deny the very first call,
// which is an artefact of the fixture rather than of the code under test.
var testEpoch = time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

// fakeClock is a manually advanced clock. It is mutex-guarded because
// rateState reads it inside its own critical section from whichever goroutine
// is logging, and the concurrency tests have many.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func newFakeClock() *fakeClock { return &fakeClock{t: testEpoch} }

func (c *fakeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// warnRecorder captures the warnings rateState would otherwise write to
// stderr, so a test can assert both the content and the one-shot guarantee.
type warnRecorder struct {
	mu   sync.Mutex
	msgs []string
}

func (w *warnRecorder) warn(msg string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.msgs = append(w.msgs, msg)
}

func (w *warnRecorder) snapshot() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]string(nil), w.msgs...)
}

// newTestRateState builds a rateState wired to a fake clock and a warning
// recorder, which is what makes every assertion below deterministic.
func newTestRateState(maxKeys int) (*rateState, *fakeClock, *warnRecorder) {
	clock := newFakeClock()
	warns := &warnRecorder{}
	s := newRateState(maxKeys)
	s.now = clock.now
	s.warn = warns.warn
	return s, clock, warns
}

func (s *rateState) trackedKeysForTest() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.limiters)
}

// TestRateState_BoundedGrowth pins the acceptance criterion: a caller that
// builds one rate-limit key per tenant, path or request cannot grow the map
// past MaxKeys, no matter how many distinct keys it presents -- and reclaiming
// them starts no goroutine.
func TestRateState_BoundedGrowth(t *testing.T) {
	const maxKeys = 64
	const keys = 1_000_000

	s, _, _ := newTestRateState(maxKeys)
	baselineGoroutines := runtime.NumGoroutine()

	for i := 0; i < keys; i++ {
		opt := RateLimitOpt{Key: "tenant:" + strconv.Itoa(i), Interval: time.Minute}
		s.shouldLog(&opt)
		if i%10_000 == 0 {
			require.LessOrEqualf(t, s.trackedKeysForTest(), maxKeys,
				"bound broken after %d distinct keys", i)
		}
	}

	assert.LessOrEqual(t, s.trackedKeysForTest(), maxKeys)
	assert.Greater(t, s.evicted.Load(), uint64(0), "eviction never ran")
	// LessOrEqual rather than Equal: the count can only be compared downwards.
	// A goroutine left running by an earlier test in this package may finish
	// while this one runs, which would make an exact match fail for a reason
	// that has nothing to do with eviction. Growth is the only direction that
	// would indict the code under test.
	assert.LessOrEqual(t, runtime.NumGoroutine(), baselineGoroutines,
		"eviction must not start a goroutine")
}

// TestRateState_SemanticallyDeadEntriesAreReclaimedFirst pins the eviction
// order: an entry past its interval can no longer suppress anything, so it is
// free to drop, while one holding an unreported suppressed count survives the
// first pass because deleting it would lose that count.
func TestRateState_SemanticallyDeadEntriesAreReclaimedFirst(t *testing.T) {
	const maxKeys = 8
	s, clock, _ := newTestRateState(maxKeys)

	for i := 0; i < maxKeys; i++ {
		opt := RateLimitOpt{Key: "k" + strconv.Itoa(i), Interval: time.Minute}
		s.shouldLog(&opt)
	}
	require.Equal(t, maxKeys, s.trackedKeysForTest())

	// One entry carries an unreported count; every entry is now past its
	// interval and therefore semantically dead.
	s.mu.Lock()
	s.limiters["k3"].suppressed = 5
	s.mu.Unlock()
	clock.advance(2 * time.Minute)

	newOpt := RateLimitOpt{Key: "fresh", Interval: time.Minute}
	s.shouldLog(&newOpt)

	s.mu.Lock()
	defer s.mu.Unlock()
	assert.Len(t, s.limiters, 2, "only the suppressed entry and the new key should remain")
	assert.Contains(t, s.limiters, "k3", "an entry with an unreported count must survive the first pass")
	assert.Contains(t, s.limiters, "fresh")
	assert.Equal(t, uint64(maxKeys-1), s.evicted.Load())
	assert.LessOrEqual(t, len(s.limiters), maxKeys)
}

// TestRateState_OldestQuarterGoesWhenNothingIsDead pins the fallback pass: with
// every interval far longer than the test, no entry is semantically dead, so
// the bound can only be held by dropping the least recently used quarter.
func TestRateState_OldestQuarterGoesWhenNothingIsDead(t *testing.T) {
	const maxKeys = 8
	s, clock, _ := newTestRateState(maxKeys)

	// Distinct lastSeen timestamps, oldest first, all with a huge interval.
	for i := 0; i < maxKeys; i++ {
		opt := RateLimitOpt{Key: "k" + strconv.Itoa(i), Interval: time.Hour}
		s.shouldLog(&opt)
		clock.advance(time.Minute)
	}
	require.Equal(t, maxKeys, s.trackedKeysForTest())

	newOpt := RateLimitOpt{Key: "fresh", Interval: time.Hour}
	s.shouldLog(&newOpt)

	s.mu.Lock()
	defer s.mu.Unlock()
	assert.NotContains(t, s.limiters, "k0", "the oldest key must go")
	assert.NotContains(t, s.limiters, "k1")
	for i := 2; i < maxKeys; i++ {
		assert.Containsf(t, s.limiters, "k"+strconv.Itoa(i), "k%d was newer than the cutoff", i)
	}
	assert.Contains(t, s.limiters, "fresh")
	assert.Equal(t, uint64(2), s.evicted.Load())
	assert.LessOrEqual(t, len(s.limiters), maxKeys)
}

// TestRateState_NonPositiveIntervalIsNotLimited pins the fail-loud policy: a
// zero or negative interval emits every time, is counted once per key, and is
// reported once per key rather than once per record.
func TestRateState_NonPositiveIntervalIsNotLimited(t *testing.T) {
	s, _, warns := newTestRateState(0)

	for i := 0; i < 50; i++ {
		zero := RateLimitOpt{Key: "zero", Interval: 0}
		negative := RateLimitOpt{Key: "negative", Interval: -time.Second}

		allow, suppressed, addField := s.shouldLog(&zero)
		require.True(t, allow, "a zero interval must not suppress")
		require.Zero(t, suppressed)
		require.False(t, addField)

		allow, _, _ = s.shouldLog(&negative)
		require.True(t, allow, "a negative interval must not suppress")
	}

	assert.Equal(t, uint64(2), s.invalidConfigs.Load(), "an invalid key is counted on creation, not per record")
	assert.Equal(t, 2, s.trackedKeysForTest(), "an invalid key is tracked so it counts against the bound")

	s.mu.Lock()
	assert.Nil(t, s.limiters["zero"].limiter, "an invalid entry must carry no limiter")
	s.mu.Unlock()

	msgs := warns.snapshot()
	require.Len(t, msgs, 2, "the invalid-interval warning must fire once per key")
	assert.Contains(t, msgs[0], `"zero"`)
	assert.Contains(t, msgs[0], "interval must be > 0")
	assert.Contains(t, msgs[1], `"negative"`)
}

// TestRateState_InvalidIntervalIsReportedAgainAfterEviction pins the honest
// bound on "once per key": the warning is tied to the entry, so a key that is
// evicted and comes back is reported again. Suppressing it for all time would
// need an unbounded set of already-warned keys.
func TestRateState_InvalidIntervalIsReportedAgainAfterEviction(t *testing.T) {
	const maxKeys = 4
	s, _, warns := newTestRateState(maxKeys)

	invalid := RateLimitOpt{Key: "bad", Interval: 0}
	s.shouldLog(&invalid)
	require.Len(t, warns.snapshot(), 1)

	// Push the invalid entry out: it is the cheapest thing to reclaim.
	for i := 0; i < 4*maxKeys; i++ {
		opt := RateLimitOpt{Key: "other" + strconv.Itoa(i), Interval: time.Hour}
		s.shouldLog(&opt)
	}
	require.NotContains(t, s.limiters, "bad")

	s.shouldLog(&invalid)
	assert.Len(t, warns.snapshot(), 2, "a re-created invalid entry reports again")
	assert.Equal(t, uint64(2), s.invalidConfigs.Load())
}

// TestRateState_ConflictingIntervalKeepsTheFirst pins "one key, one interval":
// the first interval a key is seen with is the one it keeps, and the conflict
// is counted and reported once instead of silently changing behavior.
func TestRateState_ConflictingIntervalKeepsTheFirst(t *testing.T) {
	s, clock, warns := newTestRateState(0)

	first := RateLimitOpt{Key: "db_timeout", Interval: time.Minute}
	second := RateLimitOpt{Key: "db_timeout", Interval: time.Hour}

	allow, _, _ := s.shouldLog(&first)
	require.True(t, allow)

	// One minute later the first interval has elapsed; the hour presented by
	// the second call site is ignored, so this emits.
	clock.advance(time.Minute)
	allow, _, _ = s.shouldLog(&second)
	assert.True(t, allow, "the first interval must win, not the conflicting one")

	// Immediately after, the first interval has not elapsed, so this does not.
	allow, _, _ = s.shouldLog(&second)
	assert.False(t, allow)

	s.mu.Lock()
	assert.Equal(t, time.Minute, s.limiters["db_timeout"].interval)
	s.mu.Unlock()

	assert.Equal(t, uint64(2), s.conflicts.Load(), "every conflicting call is counted")
	msgs := warns.snapshot()
	require.Len(t, msgs, 1, "the conflict warning must fire once per entry")
	assert.Contains(t, msgs[0], `"db_timeout"`)
	assert.Contains(t, msgs[0], "keeping the first interval")
}

// TestRateState_SuppressedCountIsExactAndResets pins the documented meaning of
// suppressed_count: records suppressed for this key since the previous
// emission of this key, reported once and then cleared.
func TestRateState_SuppressedCountIsExactAndResets(t *testing.T) {
	s, clock, _ := newTestRateState(0)
	opt := RateLimitOpt{Key: "quota", Interval: time.Minute}

	allow, suppressed, addField := s.shouldLog(&opt)
	require.True(t, allow)
	require.Zero(t, suppressed)
	require.False(t, addField, "the first emission has nothing to report")

	for i := 0; i < 99; i++ {
		allow, _, _ = s.shouldLog(&opt)
		require.False(t, allow)
	}

	clock.advance(time.Minute)
	allow, suppressed, addField = s.shouldLog(&opt)
	require.True(t, allow)
	assert.Equal(t, int64(99), suppressed)
	assert.True(t, addField)

	// The count was reported, so the next emission must not report it again.
	clock.advance(time.Minute)
	allow, suppressed, addField = s.shouldLog(&opt)
	require.True(t, allow)
	assert.Zero(t, suppressed, "a reported count must never be reported twice")
	assert.False(t, addField)
}

// TestRateState_ConcurrentDistinctKeysRespectTheBound is the racing version of
// TestRateState_BoundedGrowth: many goroutines inserting distinct keys at once
// must still leave the map bounded.
func TestRateState_ConcurrentDistinctKeysRespectTheBound(t *testing.T) {
	const maxKeys = 32
	const goroutines = 8
	const keysPerGoroutine = 4_000

	s, _, _ := newTestRateState(maxKeys)

	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			prefix := "g" + strconv.Itoa(id) + ":"
			for i := 0; i < keysPerGoroutine; i++ {
				opt := RateLimitOpt{Key: prefix + strconv.Itoa(i), Interval: time.Minute}
				s.shouldLog(&opt)
				if i%500 == 0 {
					assert.LessOrEqual(t, s.trackedKeysForTest(), maxKeys)
				}
			}
		}(g)
	}
	wg.Wait()

	assert.LessOrEqual(t, s.trackedKeysForTest(), maxKeys)
	assert.Greater(t, s.evicted.Load(), uint64(0))
}

// TestRateLimit_DerivedLoggerSharesRateState pins the behavior the issue calls
// correct but untested: a per-request logger built with With must not get a
// fresh limiter, or the rate limit would be trivially defeated.
func TestRateLimit_DerivedLoggerSharesRateState(t *testing.T) {
	cap := newCapturingHandler()
	parent := New(Config{Handler: cap}).(*SlogLogger)
	key := "shared_state_key"
	interval := time.Hour

	parent.Warn("from parent", WithRateLimit(key, interval))
	derived := parent.With("request_id", "abc")
	derived.Warn("from derived", WithRateLimit(key, interval))

	entries := cap.getEntries()
	require.Len(t, entries, 1, "the derived logger must share the parent's rate-limit state")
	assert.Equal(t, "from parent", entries[0].Message)
	assert.Equal(t, parent.rateState, derived.(*SlogLogger).rateState)
}

// TestRateLimit_ConfigMaxKeysIsHonored pins the resource-default policy:
// MaxKeys governs memory, so any non-positive value becomes the bounded
// default rather than an error or an unbounded map.
func TestRateLimit_ConfigMaxKeysIsHonored(t *testing.T) {
	cases := map[string]struct {
		configured int
		want       int
	}{
		"explicit": {configured: 16, want: 16},
		"unset":    {configured: 0, want: DefaultRateLimitMaxKeys},
		"negative": {configured: -1, want: DefaultRateLimitMaxKeys},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			log := New(Config{
				Handler:   newCapturingHandler(),
				RateLimit: RateLimitConfig{MaxKeys: tc.configured},
			}).(*SlogLogger)
			assert.Equal(t, tc.want, log.rateState.maxKeys)
		})
	}
}

// TestRateLimit_ObservabilityAccessors pins the operator-facing counters,
// including the hand-built logger that has no rate-limit state at all.
func TestRateLimit_ObservabilityAccessors(t *testing.T) {
	cap := newCapturingHandler()
	log := New(Config{Handler: cap, RateLimit: RateLimitConfig{MaxKeys: 8}}).(*SlogLogger)
	clock := newFakeClock()
	log.rateState.now = clock.now
	log.rateState.warn = func(string) {}

	log.Warn("invalid", WithRateLimit("bad", 0))
	log.Warn("conflict", WithRateLimit("k", time.Minute))
	log.Warn("conflict", WithRateLimit("k", time.Hour))
	for i := 0; i < 64; i++ {
		log.Warn("bulk", WithRateLimit("bulk:"+strconv.Itoa(i), time.Minute))
	}

	assert.Equal(t, uint64(1), log.RateLimitInvalid())
	assert.Equal(t, uint64(1), log.RateLimitConflicts())
	assert.Greater(t, log.RateLimitEvicted(), uint64(0))
	assert.LessOrEqual(t, log.RateLimitTrackedKeys(), 8)

	bare := &SlogLogger{}
	assert.Zero(t, bare.RateLimitTrackedKeys())
	assert.Zero(t, bare.RateLimitEvicted())
	assert.Zero(t, bare.RateLimitInvalid())
	assert.Zero(t, bare.RateLimitConflicts())
}

// BenchmarkRateLimit_Hit measures the common case: a key that is already
// tracked, so no eviction and no allocation should happen.
func BenchmarkRateLimit_Hit(b *testing.B) {
	s, _, _ := newTestRateState(0)
	opt := RateLimitOpt{Key: "hot", Interval: time.Minute}
	s.shouldLog(&opt)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.shouldLog(&opt)
	}
}

// BenchmarkRateLimit_Eviction measures the pathological case the bound exists
// for: every call presents a key that has never been seen.
func BenchmarkRateLimit_Eviction(b *testing.B) {
	s, _, _ := newTestRateState(64)
	keys := make([]string, 4096)
	for i := range keys {
		keys[i] = "k" + strconv.Itoa(i)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		opt := RateLimitOpt{Key: keys[i%len(keys)], Interval: time.Minute}
		s.shouldLog(&opt)
	}
}

// hasSuppressedCount reports whether a captured record carries the
// suppressed_count field, and its value.
func hasSuppressedCount(args []any) (int64, bool) {
	for i := 0; i+1 < len(args); i += 2 {
		if k, ok := args[i].(string); ok && k == "suppressed_count" {
			v, _ := args[i+1].(int64)
			return v, true
		}
	}
	return 0, false
}

// TestRateLimit_SuppressedCountRidesTheNextAdmittedRecord is the end-to-end
// half of TestRateState_SuppressedCountIsExactAndResets, and it pins the
// acceptance criterion that matters most about this field: *which* record
// carries it.
//
// suppressed_count belongs to the next record the limiter admits after a run of
// suppressions, never to the one that preceded them. So a hundred calls inside
// one interval emit exactly once, and that emission reports nothing -- the 99
// suppressions did not exist yet when it was made. They ride the next admitted
// record instead, and then they are gone.
//
// Asserting the absence is the point. A test that only checks that 99 shows up
// somewhere would pass just as happily against an implementation that attached
// the count to the wrong record or reported it twice.
func TestRateLimit_SuppressedCountRidesTheNextAdmittedRecord(t *testing.T) {
	cap := newCapturingHandler()
	log := New(Config{Handler: cap}).(*SlogLogger)
	clock := newFakeClock()
	log.rateState.now = clock.now

	const key = "quota"
	for i := 0; i < 100; i++ {
		log.Warn("quota exceeded", WithRateLimit(key, time.Minute))
	}

	entries := cap.getEntries()
	require.Len(t, entries, 1, "100 calls inside one interval must emit exactly once")
	_, found := hasSuppressedCount(entries[0].Args)
	assert.False(t, found, "the first emission preceded every suppression; it has nothing to report")

	clock.advance(time.Minute)
	log.Warn("quota exceeded", WithRateLimit(key, time.Minute))

	entries = cap.getEntries()
	require.Len(t, entries, 2)
	count, found := hasSuppressedCount(entries[1].Args)
	require.True(t, found, "the next admitted record must carry the suppressions")
	assert.Equal(t, int64(99), count)

	// A clean window reports nothing: the count was cleared when it was
	// reported, so it can never be counted twice.
	clock.advance(time.Minute)
	log.Warn("quota exceeded", WithRateLimit(key, time.Minute))

	entries = cap.getEntries()
	require.Len(t, entries, 3)
	_, found = hasSuppressedCount(entries[2].Args)
	assert.False(t, found, "a reported count must never be reported twice")
}
