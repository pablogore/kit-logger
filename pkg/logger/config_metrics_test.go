package logger

import (
	"bytes"
	"errors"
	"log/slog"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	kitprom "github.com/pablogore/kit-logger/pkg/logger/prometheus"
)

// TestNew_ZeroConfig_RegistersNoPrometheusCollector pins KITLOG-GO-010's
// core acceptance criterion: a Config with no MetricsHandler must not
// register anything, anywhere -- not even into the process-wide default
// registry the old, unconditional PrometheusHandler used.
func TestNew_ZeroConfig_RegistersNoPrometheusCollector(t *testing.T) {
	l := New(Config{})
	l.Info("hello")

	families, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)
	for _, f := range families {
		assert.NotEqual(t, "slog_logged_total", f.GetName())
		assert.NotEqual(t, kitprom.MetricName, f.GetName())
	}
}

// TestNewWithError_MetricsHandler_WiresPrometheus proves Config.MetricsHandler
// is the seam that connects a logger to a caller-chosen registry, replacing
// the old unconditional insertion.
func TestNewWithError_MetricsHandler_WiresPrometheus(t *testing.T) {
	reg := prometheus.NewRegistry()

	l, err := NewWithError(Config{
		MetricsHandler: func(next slog.Handler) (slog.Handler, error) {
			return kitprom.New(next, reg, kitprom.Options{})
		},
	})
	require.NoError(t, err)

	l.Info("counted")
	l.Warn("also counted")

	count, err := testutil.GatherAndCount(reg, kitprom.MetricName)
	require.NoError(t, err)
	assert.Equal(t, 2, count) // two distinct label values: INFO and WARN
}

// TestNewWithError_MetricsHandlerError_IsSurfacedButLoggerStaysUsable pins
// that a MetricsHandler failure (e.g. a registry collision) is reported
// through NewWithError's error, without breaking the returned Logger --
// consistent with every other Validate-reported problem in this package.
func TestNewWithError_MetricsHandlerError_IsSurfacedButLoggerStaysUsable(t *testing.T) {
	boom := errors.New("boom: registry collision")

	l, err := NewWithError(Config{
		MetricsHandler: func(next slog.Handler) (slog.Handler, error) {
			return nil, boom
		},
	})

	require.Error(t, err)
	assert.ErrorIs(t, err, boom)
	require.NotNil(t, l)
	l.Info("still logs") // must not panic
}

// TestNewWithError_MetricsHandlerNilNilResult_IsRejectedButLoggerStaysUsable
// pins that a MetricsHandler breaking its own contract -- returning (nil,
// nil) instead of either a valid handler or an error -- must not be silently
// treated as "no instrumentation". decorate must surface a descriptive error
// while still keeping the original pipeline usable, exactly as it does for
// any other MetricsHandler failure.
func TestNewWithError_MetricsHandlerNilNilResult_IsRejectedButLoggerStaysUsable(t *testing.T) {
	var buf bytes.Buffer

	l, err := NewWithError(Config{
		Writer: &buf,
		MetricsHandler: func(next slog.Handler) (slog.Handler, error) {
			return nil, nil
		},
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "MetricsHandler")
	require.NotNil(t, l)

	l.Info("still logs") // must not panic

	assert.Contains(t, buf.String(), "still logs")
}

// TestConfig_Validate_PipelineOverrideIgnoresMetricsHandler extends the
// existing PipelineOverride ignored-fields check to the new field.
func TestConfig_Validate_PipelineOverrideIgnoresMetricsHandler(t *testing.T) {
	cfg := Config{
		PipelineOverride: slog.DiscardHandler,
		MetricsHandler: func(next slog.Handler) (slog.Handler, error) {
			return next, nil
		},
	}

	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "MetricsHandler")
}
