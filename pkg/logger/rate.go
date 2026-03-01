package logger

import (
	"sync"
	"time"

	"golang.org/x/time/rate"
)

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
	limiter   *rate.Limiter
	suppressed int64
}

// rateState holds per-key limiters; used by SlogLogger.
type rateState struct {
	mu       sync.Mutex
	limiters map[string]*rateEntry
}

func newRateState() *rateState {
	return &rateState{limiters: make(map[string]*rateEntry)}
}

// shouldLog returns whether to emit, suppressed count to add, and whether to add the field.
// Caller must not hold any lock when emitting the log.
func (s *rateState) shouldLog(rateLimit *RateLimitOpt) (allow bool, suppressed int64, addSuppressedField bool) {
	if rateLimit == nil || rateLimit.Key == "" {
		return true, 0, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.limiters[rateLimit.Key]
	if !ok {
		e = &rateEntry{limiter: rate.NewLimiter(rate.Every(rateLimit.Interval), 1)}
		s.limiters[rateLimit.Key] = e
	}
	allow = e.limiter.Allow()
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
