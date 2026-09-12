package handler

import "log/slog"

// NewPrometheusHandler is deprecated: Prometheus instrumentation moved to
// pkg/logger/prometheus, so this package no longer needs client_golang as a
// dependency. This function is now a no-op: it registers nothing, counts
// nothing, and returns next unchanged.
//
// Deprecated: use pkg/logger/prometheus.New, wired through
// Config.MetricsHandler, instead:
//
//	cfg.MetricsHandler = func(next slog.Handler) (slog.Handler, error) {
//	    return kitprom.New(next, prometheus.DefaultRegisterer, kitprom.Options{})
//	}
//
// This function will be removed in a future release.
func NewPrometheusHandler(next slog.Handler) slog.Handler {
	return next
}
