package prometheus_test

import (
	"log/slog"

	"github.com/pablogore/kit-logger/pkg/logger"
	kitprom "github.com/pablogore/kit-logger/pkg/logger/prometheus"
	"github.com/prometheus/client_golang/prometheus"
)

// Example mirrors the README's Prometheus Metrics section: instrumentation is
// opt-in through Config.MetricsHandler, and the registry is passed
// explicitly. No Output comment, since the JSON line carries a timestamp;
// the point is that the snippet compiles against the real seam.
func Example() {
	log := logger.New(logger.Config{
		MetricsHandler: func(next slog.Handler) (slog.Handler, error) {
			return kitprom.New(next, prometheus.DefaultRegisterer, kitprom.Options{})
		},
	})

	log.Info("counted in kitlogger_records_total")
}
