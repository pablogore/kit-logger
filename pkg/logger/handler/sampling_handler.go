package handler

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"log/slog"
	"sync"
	"time"
)

// SamplingConfig defines the configuration for the SamplingHandler.
type SamplingConfig struct {
	Interval    time.Duration // Minimum time between logs of the same level.
	MinLevel    slog.Level    // No sampling below this level.
	Probability float64       // Probability of allowing the log (between 0 and 1).
}

// SamplingHandler is a handler that controls log frequency and probability.
type SamplingHandler struct {
	cfg  SamplingConfig           // Configuration for sampling.
	last map[slog.Level]time.Time // Last log time for each level.
	mu   sync.Mutex               // Mutex to protect access to last.
	next slog.Handler             // The next handler in the chain.
}

// NewSamplingHandler creates a handler with frequency and probability control.
func NewSamplingHandler(next slog.Handler, cfg SamplingConfig) *SamplingHandler {
	return &SamplingHandler{
		next: next,
		cfg:  cfg,
		last: make(map[slog.Level]time.Time),
	}
}

// Enabled checks if the logging level is enabled.
func (h *SamplingHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

// Handle processes the log record with sampling control.
func (h *SamplingHandler) Handle(ctx context.Context, record slog.Record) error {
	if record.Level < h.cfg.MinLevel {
		return h.next.Handle(ctx, record) // No sampling below MinLevel.
	}

	now := time.Now()
	h.mu.Lock()
	defer h.mu.Unlock()

	// Rate limit by interval.
	if lastTime, ok := h.last[record.Level]; ok {
		if now.Sub(lastTime) < h.cfg.Interval {
			return nil // Skip log.
		}
	}

	// Probability check using crypto/rand.
	// If Probability is 1.0, we can skip the random check entirely.
	if h.cfg.Probability < 1.0 {
		randomVal, err := secureFloat64()
		if err != nil {
			// fallback: skip log if entropy source failed and probability < 1.0
			return nil
		}

		if randomVal > h.cfg.Probability {
			return nil // Skip log.
		}
	}

	h.last[record.Level] = now
	return h.next.Handle(ctx, record)
}

// secureFloat64 generates a random float64 using crypto/rand in [0,1).
func secureFloat64() (float64, error) {
	var b [8]byte
	_, err := rand.Read(b[:])
	if err != nil {
		return 0, err
	}
	n := binary.BigEndian.Uint64(b[:])
	return float64(n) / (1 << 64), nil // Normalize to [0,1)
}

// WithAttrs returns a new SamplingHandler with additional attributes.
func (h *SamplingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return NewSamplingHandler(h.next.WithAttrs(attrs), h.cfg)
}

// WithGroup returns a new SamplingHandler with a group name.
func (h *SamplingHandler) WithGroup(name string) slog.Handler {
	return NewSamplingHandler(h.next.WithGroup(name), h.cfg)
}
