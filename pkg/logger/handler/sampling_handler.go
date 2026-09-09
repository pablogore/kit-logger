package handler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"slices"
	"sync"
	"sync/atomic"
	"time"
)

// DefaultSamplingMaxKeys bounds how many distinct sampling keys are tracked.
const DefaultSamplingMaxKeys = 4096

// SamplingKeyFunc derives the identity that a record is sampled by. Two records
// with different keys never suppress each other.
type SamplingKeyFunc func(ctx context.Context, record slog.Record) string

// DefaultSamplingKey keys a record by level and message.
//
// This is the useful default: it thins out a repeating event while leaving
// distinct events independent, so a "database timeout" warning cannot silence
// an unrelated "cache unavailable" warning.
//
// It is exported so callers can compose or inspect that identity. A nil
// KeyFunc does not call it: the handler then uses an allocation-free internal
// representation of the same (level, message) identity, which is equivalent in
// meaning but not in cost. Setting KeyFunc to DefaultSamplingKey is therefore
// the slower way to ask for the default.
func DefaultSamplingKey(_ context.Context, record slog.Record) string {
	return record.Level.String() + "\x00" + record.Message
}

// SamplingConfig defines the configuration for the SamplingHandler.
type SamplingConfig struct {
	// Interval is the minimum time between two records sharing a sampling key.
	// Zero disables interval-based sampling. Must not be negative.
	Interval time.Duration

	// MinLevel is the level below which records bypass sampling entirely.
	MinLevel slog.Level

	// Probability is the chance in [0,1] that a record passes the probability
	// gate.
	//
	// Zero means "unset" and is treated as 1 (emit everything). A sampler that
	// silently discards every record is indistinguishable from a
	// misconfiguration, so this field fails toward emitting; use Enabled=false
	// on logger.Config to turn sampling off. Values outside [0,1] are rejected
	// by Validate.
	Probability float64

	// KeyFunc derives the sampling key. When nil, records are keyed by level
	// and message (equivalent to DefaultSamplingKey, but without allocating).
	//
	// A custom KeyFunc owns the identity completely, including whether records
	// at different levels share a key.
	KeyFunc SamplingKeyFunc

	// Now supplies the current time. Defaults to time.Now. It is called while
	// the sampler's lock is held, so it must be cheap and must not re-enter the
	// logger.
	Now func() time.Time

	// Rand returns a pseudo-random value in [0,1). Defaults to
	// math/rand/v2.Float64.
	//
	// Unlike Now, it is called *outside* the sampler's lock -- the probability
	// gate deliberately runs before any state is touched -- so it may be called
	// concurrently from many goroutines and must be safe for concurrent use. It
	// must also be cheap, must not block and must not re-enter the logger.
	Rand func() float64

	// MaxKeys bounds the number of tracked sampling keys. Defaults to
	// DefaultSamplingMaxKeys. Must not be negative.
	//
	// Its invalid-value policy is deliberately *not* the one Interval and
	// Probability follow. Those two govern emission, so they fail toward
	// emitting: a misconfiguration must never silence a service. MaxKeys
	// governs memory instead, so it fails toward a bounded safe resource
	// default: any non-positive value -- zero meaning unset, negative meaning
	// invalid -- becomes DefaultSamplingMaxKeys. That substitution changes only
	// how many distinct keys are remembered, never which records are emitted.
	//
	// Validate still reports a negative MaxKeys, so NewSamplingHandlerWithError
	// surfaces it while NewSamplingHandler applies the default silently.
	MaxKeys int
}

// Validate reports every problem with the configuration.
func (c SamplingConfig) Validate() error {
	var errs []error
	if c.Interval < 0 {
		errs = append(errs, fmt.Errorf("Interval: must be >= 0, got %s", c.Interval))
	}
	if c.Probability < 0 || c.Probability > 1 {
		errs = append(errs, fmt.Errorf("Probability: must be in [0,1], got %v", c.Probability))
	}
	if c.MaxKeys < 0 {
		errs = append(errs, fmt.Errorf("MaxKeys: must be >= 0, got %d", c.MaxKeys))
	}
	return errors.Join(errs...)
}

// samplingKey is the internal identity a record is sampled by.
//
// Keeping level and text separate is what makes the default key allocation
// free: both parts are already available on the record, so nothing has to be
// concatenated into a new string on the hot path.
type samplingKey struct {
	level slog.Level
	text  string
}

// samplingState is the sampling decision state. It is shared by every handler
// derived through WithAttrs and WithGroup, so deriving a logger does not reset
// sampling.
type samplingState struct {
	mu      sync.Mutex
	last    map[samplingKey]time.Time
	maxKeys int

	// evictBuf is reused across evictions so the batch pass allocates nothing.
	evictBuf []time.Time

	suppressed atomic.Uint64
	evicted    atomic.Uint64
}

// SamplingHandler controls log frequency per sampling key.
//
// Records at or above MinLevel are sampled by the key returned by KeyFunc:
// within Interval, only the first record for a key is emitted. Records below
// MinLevel bypass sampling.
//
// The wrapped handler is never called while the sampler's lock is held.
type SamplingHandler struct {
	next  slog.Handler
	state *samplingState

	// Resolved configuration; defaults are applied once at construction so the
	// hot path has no nil checks.
	interval    time.Duration
	minLevel    slog.Level
	probability float64
	keyFunc     SamplingKeyFunc
	now         func() time.Time
	rand        func() float64
}

// NewSamplingHandler creates a handler with frequency and probability control.
//
// Invalid values in cfg are replaced by safe defaults under two distinct
// policies, because the fields govern different things:
//
//   - Interval and Probability govern emission and fail toward emitting, so a
//     misconfiguration can never silence a service.
//   - MaxKeys governs memory and falls back to the bounded
//     DefaultSamplingMaxKeys. This is a resource default, not an emitting one;
//     it changes how many keys are tracked, never which records are emitted.
//
// Use NewSamplingHandlerWithError, or SamplingConfig.Validate, to detect
// invalid values instead of having them replaced.
func NewSamplingHandler(next slog.Handler, cfg SamplingConfig) *SamplingHandler {
	h, _ := newSamplingHandler(next, cfg)
	return h
}

// NewSamplingHandlerWithError is NewSamplingHandler with configuration
// validation. On error it still returns a usable handler built from clamped
// defaults, so a caller that chooses to log the problem and continue does not
// lose records.
func NewSamplingHandlerWithError(next slog.Handler, cfg SamplingConfig) (*SamplingHandler, error) {
	return newSamplingHandler(next, cfg)
}

func newSamplingHandler(next slog.Handler, cfg SamplingConfig) (*SamplingHandler, error) {
	err := cfg.Validate()

	interval := cfg.Interval
	if interval < 0 {
		interval = 0
	}
	probability := cfg.Probability
	if probability <= 0 || probability > 1 {
		// Zero is "unset"; out-of-range is invalid. Both fail toward emitting.
		probability = 1
	}
	// Not an emitting default: MaxKeys bounds memory, not emission. Zero is
	// unset and negative is invalid, and both fall back to the same bounded
	// value -- Validate is what tells the two apart for a caller who asks.
	maxKeys := cfg.MaxKeys
	if maxKeys <= 0 {
		maxKeys = DefaultSamplingMaxKeys
	}
	// Left nil on purpose: a nil KeyFunc selects the allocation-free
	// (level, message) key. DefaultSamplingKey remains exported so callers can
	// compose or inspect the default identity.
	keyFunc := cfg.KeyFunc
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	randFn := cfg.Rand
	if randFn == nil {
		randFn = rand.Float64
	}

	return &SamplingHandler{
		next: next,
		state: &samplingState{
			last:    make(map[samplingKey]time.Time),
			maxKeys: maxKeys,
		},
		interval:    interval,
		minLevel:    cfg.MinLevel,
		probability: probability,
		keyFunc:     keyFunc,
		now:         now,
		rand:        randFn,
	}, err
}

// Enabled reports whether the wrapped handler is enabled for level.
func (h *SamplingHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

// Handle applies sampling and, if the record passes, delegates to the wrapped
// handler. The sampler's lock is always released before the wrapped handler is
// called.
func (h *SamplingHandler) Handle(ctx context.Context, record slog.Record) error {
	if record.Level < h.minLevel {
		return h.next.Handle(ctx, record) // Below MinLevel: never sampled.
	}
	if !h.allow(ctx, record) {
		return nil
	}
	return h.next.Handle(ctx, record) // Lock is not held here.
}

// allow makes the sampling decision.
//
// The probability gate runs first and needs no state, which keeps the
// caller-supplied Rand out of the critical section.
//
// When Interval is zero there is no interval state to keep: elapsed time can
// never be less than zero, so nothing could ever be suppressed. The sampler
// then takes no lock, computes no key and stores nothing — which makes
// "Interval 0 delivers everything" structural rather than merely fixed.
//
// Otherwise the clock is read inside the same critical section that reads and
// writes state.last. That is what prevents a goroutine from capturing a
// timestamp, losing the lock race, and then comparing against a strictly later
// timestamp written by the winner. Elapsed time is additionally clamped at zero
// so a non-monotonic clock cannot produce a negative interval and discard a
// record that nothing asked to discard.
func (h *SamplingHandler) allow(ctx context.Context, record slog.Record) bool {
	if h.probability < 1 && h.rand() >= h.probability {
		h.state.suppressed.Add(1)
		return false
	}
	if h.interval <= 0 {
		return true
	}

	// The default key costs no allocation; a custom KeyFunc owns the identity
	// entirely, including whether two levels share it.
	var key samplingKey
	if h.keyFunc == nil {
		key = samplingKey{level: record.Level, text: record.Message}
	} else {
		key = samplingKey{text: h.keyFunc(ctx, record)}
	}
	s := h.state

	s.mu.Lock()
	now := h.now() // Inside the lock, together with the read of s.last.

	if last, ok := s.last[key]; ok {
		elapsed := now.Sub(last)
		if elapsed < 0 {
			elapsed = 0
		}
		if elapsed < h.interval {
			s.mu.Unlock()
			s.suppressed.Add(1)
			return false
		}
		s.last[key] = now
		s.mu.Unlock()
		return true
	}

	s.evictLocked(now, h.interval)
	s.last[key] = now
	s.mu.Unlock()
	return true
}

// evictLocked makes room for new keys when the map is at capacity.
//
// It first drops entries that can no longer suppress anything, then falls back
// to dropping the oldest quarter in a single pass, so the O(n) cost is
// amortised over maxKeys/4 insertions instead of being paid on every one. It
// never starts a goroutine.
func (s *samplingState) evictLocked(now time.Time, interval time.Duration) {
	if len(s.last) < s.maxKeys {
		return
	}
	before := len(s.last)

	for k, t := range s.last {
		if now.Sub(t) >= interval {
			delete(s.last, k)
		}
	}
	if len(s.last) < s.maxKeys {
		s.evicted.Add(uint64(before - len(s.last)))
		return
	}

	// Keep the newest three quarters.
	target := s.maxKeys - s.maxKeys/4
	if target < 1 {
		target = 1
	}

	s.evictBuf = s.evictBuf[:0]
	for _, t := range s.last {
		s.evictBuf = append(s.evictBuf, t)
	}
	slices.SortFunc(s.evictBuf, func(a, b time.Time) int { return a.Compare(b) })
	cutoff := s.evictBuf[len(s.evictBuf)-target]
	for k, t := range s.last {
		if t.Before(cutoff) {
			delete(s.last, k)
		}
	}

	// Many entries can share the cutoff instant (a frozen or coarse clock), in
	// which case the pass above deletes nothing. Fall back to dropping
	// arbitrary entries so the bound always holds.
	for k := range s.last {
		if len(s.last) <= target {
			break
		}
		delete(s.last, k)
	}

	s.evicted.Add(uint64(before - len(s.last)))
}

// Suppressed is the number of records discarded by sampling.
func (h *SamplingHandler) Suppressed() uint64 { return h.state.suppressed.Load() }

// Evicted is the number of tracked sampling keys reclaimed to stay within
// MaxKeys.
func (h *SamplingHandler) Evicted() uint64 { return h.state.evicted.Load() }

// TrackedKeys is the number of sampling keys currently tracked. It never
// exceeds MaxKeys.
func (h *SamplingHandler) TrackedKeys() int {
	h.state.mu.Lock()
	defer h.state.mu.Unlock()
	return len(h.state.last)
}

// WithAttrs returns a handler that shares this handler's sampling state, so
// deriving a logger does not reset sampling.
func (h *SamplingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	derived := *h
	derived.next = h.next.WithAttrs(attrs)
	return &derived
}

// WithGroup returns a handler that shares this handler's sampling state, so
// deriving a logger does not reset sampling.
func (h *SamplingHandler) WithGroup(name string) slog.Handler {
	derived := *h
	derived.next = h.next.WithGroup(name)
	return &derived
}

// Unwrap returns the handler this one decorates. It lets a logger lifecycle
// (Flush, Shutdown) traverse a chain that was assembled by hand, instead of
// stopping at the outermost handler.
func (h *SamplingHandler) Unwrap() slog.Handler { return h.next }
