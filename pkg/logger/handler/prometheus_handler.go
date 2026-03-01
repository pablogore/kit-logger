package handler

import (
	"context"
	"log/slog"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
)

// LogCounter is a Prometheus counter vector to count the total number of slog log entries.
var LogCounter = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "slog_logged_total",
		Help: "Total number of slog log entries.",
	},
	[]string{"level"},
)

var registerOnce sync.Once

func init() {
	registerOnce.Do(func() {
		// Use Register instead of MustRegister to avoid panics
		if err := prometheus.Register(LogCounter); err != nil {
			// If the metric is already registered, that's fine
			if are, ok := err.(prometheus.AlreadyRegisteredError); ok {
				// Use the existing metric
				LogCounter = are.ExistingCollector.(*prometheus.CounterVec)
			}
		}
	})
}

// PrometheusHandler is a log handler that increments a Prometheus counter for each log entry.
type PrometheusHandler struct {
	next slog.Handler
}

func NewPrometheusHandler(next slog.Handler) slog.Handler {
	return &PrometheusHandler{next: next}
}

func (h *PrometheusHandler) Handle(ctx context.Context, r slog.Record) error {
	LogCounter.WithLabelValues(r.Level.String()).Inc()
	return h.next.Handle(ctx, r)
}

func (h *PrometheusHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

func (h *PrometheusHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &PrometheusHandler{next: h.next.WithAttrs(attrs)}
}

func (h *PrometheusHandler) WithGroup(name string) slog.Handler {
	return &PrometheusHandler{next: h.next.WithGroup(name)}
}
