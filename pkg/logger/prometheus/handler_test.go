package prometheus_test

import (
	"context"
	"log/slog"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	kitprom "github.com/pablogore/kit-logger/pkg/logger/prometheus"

	"github.com/pablogore/kit-logger/pkg/logger/kitlogtest"
)

// metricHelp matches exactly what New's CounterOpts uses. A collision test
// needs an identical descriptor (fqName + help + label names) so the
// registry reports AlreadyRegisteredError -- a mismatched descriptor (e.g. a
// different help string) is a distinct, unrecoverable registration error
// instead, which is not what these tests are pinning.
const metricHelp = "Total number of log records handled, by level."

func newCapturingHandler(captured *[]slog.Record) slog.Handler {
	return kitlogtest.NewTestHandler(func(_ context.Context, r slog.Record) {
		*captured = append(*captured, r)
	})
}

// mustFindCounterVec recovers the *CounterVec New registered with reg, by
// re-registering an identical descriptor and relying on
// AlreadyRegisteredError to hand back the original -- the same reuse path
// New itself exercises internally.
func mustFindCounterVec(t *testing.T, reg prometheus.Registerer) *prometheus.CounterVec {
	t.Helper()
	probe := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: kitprom.MetricName,
		Help: metricHelp,
	}, []string{"level"})
	err := reg.Register(probe)
	require.Error(t, err, "expected the metric to already be registered")

	var are prometheus.AlreadyRegisteredError
	require.ErrorAs(t, err, &are)
	existing, ok := are.ExistingCollector.(*prometheus.CounterVec)
	require.True(t, ok)
	return existing
}

func TestNew_RegistersAndCounts(t *testing.T) {
	reg := prometheus.NewRegistry()
	var captured []slog.Record

	h, err := kitprom.New(newCapturingHandler(&captured), reg, kitprom.Options{})
	require.NoError(t, err)

	logger := slog.New(h)
	logger.Info("hello")

	require.Len(t, captured, 1)
	assert.Equal(t, "hello", captured[0].Message)

	counter := mustFindCounterVec(t, reg)
	assert.Equal(t, float64(1), testutil.ToFloat64(counter.WithLabelValues("INFO")))
}

// TestNew_CollisionWithDifferentCollectorType pins the exact case that
// panics on HEAD: a registry that already holds a different collector type
// under the same descriptor. New must error, naming the conflicting type,
// and must not panic.
func TestNew_CollisionWithDifferentCollectorType(t *testing.T) {
	reg := prometheus.NewRegistry()
	gauge := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: kitprom.MetricName,
		Help: metricHelp,
	}, []string{"level"})
	require.NoError(t, reg.Register(gauge))

	var captured []slog.Record
	h, err := kitprom.New(newCapturingHandler(&captured), reg, kitprom.Options{})

	require.Error(t, err)
	assert.Nil(t, h)
	assert.Contains(t, err.Error(), "GaugeVec")
	assert.Contains(t, err.Error(), kitprom.MetricName)
}

func TestNew_CollisionWithSameCollectorType_Reuses(t *testing.T) {
	reg := prometheus.NewRegistry()
	existing := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: kitprom.MetricName,
		Help: metricHelp,
	}, []string{"level"})
	require.NoError(t, reg.Register(existing))

	var captured []slog.Record
	h, err := kitprom.New(newCapturingHandler(&captured), reg, kitprom.Options{})
	require.NoError(t, err)

	logger := slog.New(h)
	logger.Warn("reused")

	assert.Equal(t, float64(1), testutil.ToFloat64(existing.WithLabelValues("WARN")))
}

func TestNew_NilRegistererSkipsRegistration(t *testing.T) {
	var captured []slog.Record
	h, err := kitprom.New(newCapturingHandler(&captured), nil, kitprom.Options{})
	require.NoError(t, err)

	logger := slog.New(h)
	logger.Error("no registry")

	require.Len(t, captured, 1)

	count, err := testutil.GatherAndCount(prometheus.DefaultGatherer, kitprom.MetricName)
	require.NoError(t, err)
	assert.Equal(t, 0, count, "a nil Registerer must not touch the default registry either")
}

func TestNew_TwoRegistriesAreIndependent(t *testing.T) {
	reg1 := prometheus.NewRegistry()
	reg2 := prometheus.NewRegistry()
	var captured1, captured2 []slog.Record

	h1, err := kitprom.New(newCapturingHandler(&captured1), reg1, kitprom.Options{})
	require.NoError(t, err)
	h2, err := kitprom.New(newCapturingHandler(&captured2), reg2, kitprom.Options{})
	require.NoError(t, err)

	slog.New(h1).Info("only reg1")
	slog.New(h1).Info("only reg1 again")
	slog.New(h2).Warn("only reg2")

	c1 := mustFindCounterVec(t, reg1)
	c2 := mustFindCounterVec(t, reg2)

	assert.Equal(t, float64(2), testutil.ToFloat64(c1.WithLabelValues("INFO")))
	assert.Equal(t, float64(0), testutil.ToFloat64(c1.WithLabelValues("WARN")))
	assert.Equal(t, float64(1), testutil.ToFloat64(c2.WithLabelValues("WARN")))
}

func TestNew_MixedLevelCounts(t *testing.T) {
	reg := prometheus.NewRegistry()
	var captured []slog.Record
	h, err := kitprom.New(newCapturingHandler(&captured), reg, kitprom.Options{})
	require.NoError(t, err)

	logger := slog.New(h)
	for i := 0; i < 40; i++ {
		logger.Info("info line")
	}
	for i := 0; i < 35; i++ {
		logger.Warn("warn line")
	}
	for i := 0; i < 25; i++ {
		logger.Error("error line")
	}
	require.Len(t, captured, 100)

	counter := mustFindCounterVec(t, reg)
	assert.Equal(t, float64(40), testutil.ToFloat64(counter.WithLabelValues("INFO")))
	assert.Equal(t, float64(35), testutil.ToFloat64(counter.WithLabelValues("WARN")))
	assert.Equal(t, float64(25), testutil.ToFloat64(counter.WithLabelValues("ERROR")))
}

func TestMustNew_PanicsOnCollision(t *testing.T) {
	reg := prometheus.NewRegistry()
	gauge := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: kitprom.MetricName,
		Help: metricHelp,
	}, []string{"level"})
	require.NoError(t, reg.Register(gauge))

	var captured []slog.Record
	assert.Panics(t, func() {
		kitprom.MustNew(newCapturingHandler(&captured), reg, kitprom.Options{})
	})
}

func TestHandler_WithAttrsAndWithGroup_PreserveCounting(t *testing.T) {
	reg := prometheus.NewRegistry()
	var captured []slog.Record
	h, err := kitprom.New(newCapturingHandler(&captured), reg, kitprom.Options{})
	require.NoError(t, err)

	h = h.WithAttrs([]slog.Attr{slog.String("service", "auth")})
	h = h.WithGroup("request")

	logger := slog.New(h)
	logger.Info("grouped")

	require.Len(t, captured, 1)
	counter := mustFindCounterVec(t, reg)
	assert.Equal(t, float64(1), testutil.ToFloat64(counter.WithLabelValues("INFO")))
}

func TestHandler_Enabled_DelegatesToNext(t *testing.T) {
	base := &levelGatedHandler{level: slog.LevelWarn}
	h, err := kitprom.New(base, nil, kitprom.Options{})
	require.NoError(t, err)

	assert.False(t, h.Enabled(context.Background(), slog.LevelInfo))
	assert.True(t, h.Enabled(context.Background(), slog.LevelWarn))
}

func TestHandler_Unwrap_ReturnsNext(t *testing.T) {
	leaf := kitlogtest.NewTestHandler(func(context.Context, slog.Record) {})
	h, err := kitprom.New(leaf, nil, kitprom.Options{})
	require.NoError(t, err)

	unwrapper, ok := h.(interface{ Unwrap() slog.Handler })
	require.True(t, ok)
	assert.Same(t, leaf, unwrapper.Unwrap())
}

type levelGatedHandler struct{ level slog.Level }

func (h *levelGatedHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level
}
func (h *levelGatedHandler) Handle(context.Context, slog.Record) error { return nil }
func (h *levelGatedHandler) WithAttrs([]slog.Attr) slog.Handler        { return h }
func (h *levelGatedHandler) WithGroup(string) slog.Handler             { return h }
