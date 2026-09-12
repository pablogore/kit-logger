// Package prometheus provides opt-in Prometheus instrumentation for
// kit-logger. Nothing in pkg/logger or pkg/logger/handler imports
// client_golang -- importing this package is what pulls it in, and only for
// callers who actually want metrics.
package prometheus

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/prometheus/client_golang/prometheus"
)

// MetricName is the counter New registers: total records handled, by level.
const MetricName = "kitlogger_records_total"

// Options configures New. Namespace and Subsystem are optional Prometheus
// naming-convention prefixes; the zero value names the metric plainly as
// MetricName.
type Options struct {
	Namespace string
	Subsystem string
}

// handler counts records by level and forwards them to next. Each instance
// owns its own counter -- there is no package-level collector -- so two
// handlers built against two different registries never share state.
type handlerImpl struct {
	next    slog.Handler
	counter *prometheus.CounterVec
}

// New returns a slog.Handler that counts records by level and forwards them
// to next, registering its counter with reg.
//
// A nil reg skips registration entirely: the handler still counts, but the
// counter is never exposed through any registry. That is a deliberate mode,
// not a degraded one -- useful for tests, or for a caller that wants the
// counting behavior without committing to where it is exposed.
//
// If reg already holds a collector under the same descriptor, New reuses it
// when it is a *prometheus.CounterVec, and returns a descriptive error
// naming the actual type otherwise. It never panics: this is the checked
// counterpart to the unchecked type assertion in the deprecated
// handler.NewPrometheusHandler this package replaces.
func New(next slog.Handler, reg prometheus.Registerer, opts Options) (slog.Handler, error) {
	counter := prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: opts.Namespace,
		Subsystem: opts.Subsystem,
		Name:      MetricName,
		Help:      "Total number of log records handled, by level.",
	}, []string{"level"})

	if reg != nil {
		if err := reg.Register(counter); err != nil {
			var are prometheus.AlreadyRegisteredError
			if !errors.As(err, &are) {
				return nil, err
			}
			existing, ok := are.ExistingCollector.(*prometheus.CounterVec)
			if !ok {
				return nil, fmt.Errorf("kit-logger/prometheus: %q is already registered as %T, not *prometheus.CounterVec: %w",
					MetricName, are.ExistingCollector, err)
			}
			counter = existing
		}
	}

	return &handlerImpl{next: next, counter: counter}, nil
}

// MustNew is New, but panics instead of returning an error. For a caller that
// wants a registration collision to fail fast at startup rather than be
// handled.
func MustNew(next slog.Handler, reg prometheus.Registerer, opts Options) slog.Handler {
	h, err := New(next, reg, opts)
	if err != nil {
		panic(err)
	}
	return h
}

func (h *handlerImpl) Handle(ctx context.Context, r slog.Record) error {
	h.counter.WithLabelValues(r.Level.String()).Inc()
	return h.next.Handle(ctx, r)
}

func (h *handlerImpl) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

func (h *handlerImpl) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &handlerImpl{next: h.next.WithAttrs(attrs), counter: h.counter}
}

func (h *handlerImpl) WithGroup(name string) slog.Handler {
	return &handlerImpl{next: h.next.WithGroup(name), counter: h.counter}
}

// Unwrap returns the handler this one decorates. It lets a logger lifecycle
// (Flush, Shutdown) traverse a chain assembled by hand instead of stopping
// at this handler.
func (h *handlerImpl) Unwrap() slog.Handler { return h.next }
