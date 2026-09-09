package logger

import (
	"fmt"
	"os"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/time/rate"
)

// DefaultRateLimitMaxKeys bounds how many distinct rate-limit keys are tracked.
const DefaultRateLimitMaxKeys = 4096

// CounterHook is called when a rate-limited log line is emitted (not suppressed).
// Implementations may increment a metric; kit-logger does not depend on any metrics package.
type CounterHook interface {
	Inc(name string)
}

// RateLimitOpt is the value returned by WithRateLimit. Do not use directly.
type RateLimitOpt struct {
	Key      string
	Interval time.Duration
}

func (RateLimitOpt) _logOption() {}

// WithRateLimit limits this log line to one emission per key per interval.
// Pass as an additional argument to any Logger method, e.g.:
//
//	logger.WarnContext(ctx, "invalid event", logger.WithRateLimit("key", time.Minute))
//
// One key, one interval. The interval is established the first time a key is
// seen and never changes afterwards. If two call sites present the same key
// with different intervals, the first one presented wins -- deterministically,
// rather than depending on which goroutine got there first with which value --
// and the conflict is counted by SlogLogger.RateLimitConflicts and reported on
// stderr once per key. Give the two call sites different keys if they really
// want different intervals.
//
// The interval must be greater than zero. A non-positive one is not treated as
// "limit everything" and not silently accepted either: rate.Every reads it as
// an infinite rate, so the limiter would allow every event while the call site
// reads as rate limited. Such a key is therefore tracked but never limited,
// counted by SlogLogger.RateLimitInvalid and reported on stderr once per key. A
// zero interval is exactly what a caller gets from an unset configuration
// field, which is why it fails loudly instead of quietly.
//
// "Once per key" means once per tracked entry, not once for the lifetime of the
// process: the set of tracked keys is bounded, so a key that is evicted and
// presented again is reported again. Reporting exactly once forever would need
// an unbounded record of which keys have already been reported, which is the
// leak this bound exists to prevent. The counters never reset and are the
// reliable signal.
//
// suppressed_count belongs to the next record the limiter admits after a run of
// suppressions, never to the record that preceded them. It means "records
// suppressed for this key since the previous emission of this key", so the very
// first emission of a key carries no such field, and neither does an emission
// that follows a window in which nothing was suppressed. It is reset to zero on
// the emission that reports it, so consecutive emissions never double-count. It
// is also lost if that emitting record is later dropped further down the
// pipeline -- by FilterHandler or SamplingHandler, for instance -- because the
// counter has already been cleared by the time those handlers decide.
//
// Keys are bounded. At most Config.RateLimit.MaxKeys of them are tracked
// (DefaultRateLimitMaxKeys by default); once the map is full, keys whose
// interval has elapsed are reclaimed first and the oldest are dropped after
// that. Keys are caller data, so building one per tenant, per request path or
// per user is unbounded by construction: the bound keeps that from becoming a
// memory leak, but a key that has been evicted has also forgotten its limiter,
// so the next record for it is emitted. Prefer a low-cardinality key, and watch
// SlogLogger.RateLimitEvicted and SlogLogger.RateLimitTrackedKeys to find out
// when cardinality has outgrown the bound.
func WithRateLimit(key string, interval time.Duration) RateLimitOpt {
	return RateLimitOpt{Key: key, Interval: interval}
}

// CounterOpt is the value returned by WithCounter. Do not use directly.
type CounterOpt struct {
	MetricName string
}

func (CounterOpt) _logOption() {}

// WithCounter requests that the counter hook be called with metricName when this log is emitted.
// Has no effect if no CounterHook was set on the logger.
func WithCounter(metricName string) CounterOpt {
	return CounterOpt{MetricName: metricName}
}

type rateEntry struct {
	// limiter is nil for an invalid entry (see interval), and only for one.
	// Such an entry always allows, and the limiter is never consulted.
	limiter *rate.Limiter

	// interval is the interval this key was created with. It is kept so that a
	// later, conflicting value can be detected and ignored rather than
	// silently changing an existing key's behavior, and so that eviction can
	// tell whether the entry can still suppress anything.
	//
	// A non-positive interval is the invalid marker -- no separate bool. The
	// entry exists anyway, so that reporting the misconfiguration once per key
	// costs one bounded map slot instead of a second, unbounded set of
	// already-warned keys.
	interval time.Duration

	// suppressed counts records dropped for this key since its last emission.
	// It is reported and cleared by the next emission, which is what makes an
	// entry holding a non-zero count too valuable to evict cheaply. It stays
	// zero for an invalid entry, which suppresses nothing.
	suppressed int64

	// lastSeen is refreshed on every hit, so eviction can order keys by
	// recency without keeping a second index.
	lastSeen time.Time

	// conflictWarned gates the "this key already has an interval" warning. It
	// is a plain bool because it is only ever touched under rateState.mu, and
	// it lives on the entry rather than on the state so that one noisy key
	// cannot silence the report for every other key.
	conflictWarned bool
}

// rateState holds per-key limiters; used by SlogLogger.
//
// It is deliberately shared by every logger derived through With: a
// per-request logger must not come with a fresh set of limiters, or the rate
// limit would be defeated by the very pattern it exists to protect.
//
// The map is bounded by maxKeys and reclaimed in place, under the same mutex
// that guards a decision. Nothing here starts a goroutine: a cleanup goroutine
// would need a lifecycle of its own, and this library has already paid for
// that mistake once.
type rateState struct {
	mu       sync.Mutex
	limiters map[string]*rateEntry
	maxKeys  int

	// evictBuf is reused across evictions so the batch pass allocates nothing.
	evictBuf []time.Time

	// now is read inside the critical section and also drives the limiters, so
	// a test can make every decision deterministic. Defaults to time.Now.
	now func() time.Time

	// warn reports a misconfiguration. Defaults to a stderr line, because a
	// logger cannot report its own misconfiguration through itself without
	// risking re-entry. Injectable so tests do not have to read stderr.
	warn func(string)

	evicted        atomic.Uint64
	invalidConfigs atomic.Uint64 // keys created with a non-positive interval
	conflicts      atomic.Uint64 // same key presented with a different interval
}

// newRateState creates the per-key limiter state bounded by maxKeys.
//
// Any non-positive maxKeys -- zero meaning unset, negative meaning invalid --
// becomes DefaultRateLimitMaxKeys. This is exactly the policy documented for
// SamplingConfig.MaxKeys and for the same reason: maxKeys governs memory, not
// emission, so it fails toward a bounded safe resource default instead of
// toward emitting. The substitution changes only how many distinct keys are
// remembered, never which records are emitted.
func newRateState(maxKeys int) *rateState {
	if maxKeys <= 0 {
		maxKeys = DefaultRateLimitMaxKeys
	}
	return &rateState{
		limiters: make(map[string]*rateEntry),
		maxKeys:  maxKeys,
		now:      time.Now,
		warn:     func(msg string) { fmt.Fprintln(os.Stderr, "kit-logger: "+msg) },
	}
}

// shouldLog returns whether to emit, suppressed count to add, and whether to add the field.
// Caller must not hold any lock when emitting the log.
//
// The clock is read inside the same critical section that reads and writes the
// map, and it is the clock the limiter itself is driven with: AllowN(now, 1)
// rather than Allow(). Letting the limiter read time.Now on its own would put a
// second, unsynchronized clock behind the decision -- untestable, and free to
// disagree with the timestamps eviction orders keys by.
//
// Both misconfigurations it reports -- a non-positive interval and a key
// presented with two different intervals -- are warned about once per *entry*,
// not once per process. That is the honest bound: the map is finite, so if the
// key is evicted and comes back the warning comes back with it. Warning exactly
// once for all time would need an unbounded set of already-warned keys, which
// is the leak this whole change exists to remove. The counters, which never
// reset, are the reliable signal.
func (s *rateState) shouldLog(rateLimit *RateLimitOpt) (allow bool, suppressed int64, addSuppressedField bool) {
	// A nil state is a SlogLogger assembled by hand rather than through New. It
	// has no limiters, so it limits nothing -- which must mean "emit", never
	// "panic on the logging path".
	if s == nil || rateLimit == nil || rateLimit.Key == "" {
		return true, 0, false
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()

	e, ok := s.limiters[rateLimit.Key]
	if ok {
		// Clamp a clock that went backwards -- a fake one, or a real one read
		// without a monotonic component -- against this entry's own timestamp.
		// x/time/rate clamps internally for its token bookkeeping; this clamp
		// protects the lastSeen ordering eviction depends on, which would
		// otherwise mark a just-touched entry as the oldest in the map.
		if now.Before(e.lastSeen) {
			now = e.lastSeen
		}
		e.lastSeen = now
		if e.interval != rateLimit.Interval {
			// First interval wins, including when the first one was invalid:
			// the key's behavior is decided once and does not change under it.
			s.conflicts.Add(1)
			if !e.conflictWarned {
				e.conflictWarned = true
				s.warn(fmt.Sprintf("WithRateLimit(%q, %s): key already rate limited at %s; keeping the first interval",
					rateLimit.Key, rateLimit.Interval, e.interval))
			}
		}
	} else {
		s.evictLocked(now)
		e = &rateEntry{interval: rateLimit.Interval, lastSeen: now}
		if rateLimit.Interval > 0 {
			e.limiter = rate.NewLimiter(rate.Every(rateLimit.Interval), 1)
		} else {
			// Fail loudly toward emitting. The entry is still created, with no
			// limiter: it costs one slot of the bounded map and buys a report
			// that fires once per key instead of once per record.
			s.invalidConfigs.Add(1)
			s.warn(fmt.Sprintf("WithRateLimit(%q, %s): interval must be > 0; this call is not rate limited",
				rateLimit.Key, rateLimit.Interval))
		}
		s.limiters[rateLimit.Key] = e
	}

	if e.limiter == nil {
		return true, 0, false // Invalid interval: tracked, reported, not limited.
	}

	allow = e.limiter.AllowN(now, 1)
	if allow {
		suppressed = e.suppressed
		if suppressed > 0 {
			addSuppressedField = true
			e.suppressed = 0
		}
	} else {
		e.suppressed++
	}
	return allow, suppressed, addSuppressedField
}

// evictLocked makes room for a new key when the map is at capacity.
//
// It runs only on the insertion path, so its O(n) cost is amortised over
// maxKeys/4 insertions instead of being paid on every log call, and it never
// starts a goroutine.
//
// The first pass drops only entries that can no longer suppress anything *and*
// carry no unreported count. An entry whose interval has elapsed will emit on
// its next record anyway, so forgetting it changes nothing an operator can
// observe -- unless it still holds a suppressed count, in which case deleting
// it would silently lose records that were already dropped. The second pass has
// no such luxury: the bound has to hold, so it drops by recency and may take an
// unreported count with it. That is why the counters exist.
//
// There is no TTL knob and no idle sweep. An entry whose interval has elapsed
// with no suppression debt already holds no observable information, so semantic
// expiry does what a TTL would -- and it does it without asking an operator to
// guess a second duration. The oldest-quarter fallback covers the case a TTL
// could not: every interval huge, nothing semantically dead, and the bound
// still has to hold.
//
// Invalid entries (interval <= 0, no limiter) need no special case here. Their
// elapsed time is trivially >= their non-positive interval and their suppressed
// count is always zero, so they are always the first thing the pass reclaims.
func (s *rateState) evictLocked(now time.Time) {
	if len(s.limiters) < s.maxKeys {
		return
	}
	before := len(s.limiters)

	for k, e := range s.limiters {
		elapsed := now.Sub(e.lastSeen)
		if elapsed < 0 {
			// A non-monotonic clock must not make an entry look older than it
			// is, the same way sampling clamps its elapsed at zero.
			elapsed = 0
		}
		if elapsed >= e.interval && e.suppressed == 0 {
			delete(s.limiters, k)
		}
	}
	if len(s.limiters) < s.maxKeys {
		s.evicted.Add(uint64(before - len(s.limiters)))
		return
	}

	// Keep the newest three quarters, and never more than maxKeys-1: the
	// caller is about to insert, and the bound is on the map *after* that
	// insert. For a very small maxKeys the two rules collide -- maxKeys of 1
	// cannot keep anything and still take a new key -- and the bound wins.
	target := s.maxKeys - s.maxKeys/4
	if target > s.maxKeys-1 {
		target = s.maxKeys - 1
	}
	if target < 0 {
		target = 0
	}
	if target == 0 {
		clear(s.limiters)
		s.evicted.Add(uint64(before))
		return
	}

	s.evictBuf = s.evictBuf[:0]
	for _, e := range s.limiters {
		s.evictBuf = append(s.evictBuf, e.lastSeen)
	}
	slices.SortFunc(s.evictBuf, func(a, b time.Time) int { return a.Compare(b) })
	cutoff := s.evictBuf[len(s.evictBuf)-target]
	for k, e := range s.limiters {
		if e.lastSeen.Before(cutoff) {
			delete(s.limiters, k)
		}
	}

	// Many entries can share the cutoff instant (a frozen or coarse clock), in
	// which case the pass above deletes nothing. Fall back to dropping
	// arbitrary entries so the bound always holds.
	for k := range s.limiters {
		if len(s.limiters) <= target {
			break
		}
		delete(s.limiters, k)
	}

	s.evicted.Add(uint64(before - len(s.limiters)))
}

// extractLogOptions removes rate-limit and counter options from args and returns filtered args plus options.
//
// Guarantees:
//   - Normal fields are never lost: every non-RateLimitOpt/non-CounterOpt element is appended to filtered.
//   - Order of args is preserved: iteration is over the original slice; only option types are skipped.
//   - No duplication: each option type overwrites the previous (last wins); non-option args appear exactly once in filtered.
func extractLogOptions(args []any) (filtered []any, rateLimit *RateLimitOpt, counter *CounterOpt) {
	for _, a := range args {
		switch o := a.(type) {
		case RateLimitOpt:
			rateLimit = &o
		case CounterOpt:
			counter = &o
		default:
			filtered = append(filtered, a)
		}
	}
	return filtered, rateLimit, counter
}
