package handler

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// MockBufferedHandler implements slog.Handler for testing buffered handler functionality.
type MockBufferedHandler struct {
	EnabledFunc   func(ctx context.Context, level slog.Level) bool
	FlushFunc     func() error
	HandleFunc    func(ctx context.Context, r slog.Record) error
	WithAttrsFunc func(attrs []slog.Attr) slog.Handler
	WithGroupFunc func(name string) slog.Handler

	next       slog.Handler
	buffer     []slog.Record
	bufferSize int
	mu         sync.Mutex
}

// Enabled checks if the logging level is enabled.
func (m *MockBufferedHandler) Enabled(ctx context.Context, level slog.Level) bool {
	if m.EnabledFunc != nil {
		return m.EnabledFunc(ctx, level)
	}
	if m.next != nil {
		return m.next.Enabled(ctx, level)
	}
	return true
}

// Handle handles a log record.
func (m *MockBufferedHandler) Handle(ctx context.Context, r slog.Record) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.buffer) >= m.bufferSize {
		if m.HandleFunc != nil {
			return m.HandleFunc(ctx, r)
		}
		return nil
	}

	m.buffer = append(m.buffer, r)
	if m.HandleFunc != nil {
		return m.HandleFunc(ctx, r)
	}
	return nil
}

// WithAttrs returns a new handler with additional attributes.
func (m *MockBufferedHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if m.WithAttrsFunc != nil {
		return m.WithAttrsFunc(attrs)
	}
	return m
}

// WithGroup returns a new handler with a group name.
func (m *MockBufferedHandler) WithGroup(name string) slog.Handler {
	if m.WithGroupFunc != nil {
		return m.WithGroupFunc(name)
	}
	return m
}

// Flush flushes the buffer.
func (m *MockBufferedHandler) Flush() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.buffer = m.buffer[:0]
	if m.FlushFunc != nil {
		return m.FlushFunc()
	}
	return nil
}

// GetBufferSize returns the current buffer size.
func (m *MockBufferedHandler) GetBufferSize() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.buffer)
}

// GetBufferCapacity returns the buffer capacity.
func (m *MockBufferedHandler) GetBufferCapacity() int {
	return m.bufferSize
}

// NewMockBufferedHandler creates a new mock BufferedHandler.
func NewMockBufferedHandler(next slog.Handler, bufferSize int) *MockBufferedHandler {
	return &MockBufferedHandler{
		next:       next,
		buffer:     make([]slog.Record, 0, bufferSize),
		bufferSize: bufferSize,
	}
}

// MockFilterRule implements FilterRule for testing.
type MockFilterRule struct {
	Key         string
	MatchesFunc func(key, value string) bool
	Value       string
}

// Matches checks if the filter rule matches the given key and value.
func (m *MockFilterRule) Matches(key, value string) bool {
	if m.MatchesFunc != nil {
		return m.MatchesFunc(key, value)
	}
	return m.Key == key && (m.Value == "" || m.Value == value)
}

// GetKey returns the key of the filter rule.
func (m *MockFilterRule) GetKey() string {
	return m.Key
}

// GetValue returns the value of the filter rule.
func (m *MockFilterRule) GetValue() string {
	return m.Value
}

// SetKey sets the key of the filter rule.
func (m *MockFilterRule) SetKey(key string) {
	m.Key = key
}

// SetValue sets the value of the filter rule.
func (m *MockFilterRule) SetValue(value string) {
	m.Value = value
}

// NewMockFilterRule creates a new mock FilterRule.
func NewMockFilterRule(key, value string) *MockFilterRule {
	return &MockFilterRule{
		Key:   key,
		Value: value,
	}
}

// MockHookHandler implements slog.Handler for testing hook handler functionality.
type MockHookHandler struct {
	EnabledFunc   func(ctx context.Context, level slog.Level) bool
	GetHookFunc   func() HookFunc
	HandleFunc    func(ctx context.Context, r slog.Record) error
	SetHookFunc   func(hook HookFunc)
	WithAttrsFunc func(attrs []slog.Attr) slog.Handler
	WithGroupFunc func(name string) slog.Handler

	next slog.Handler
	hook HookFunc
}

// Enabled checks if the logging level is enabled.
func (m *MockHookHandler) Enabled(ctx context.Context, level slog.Level) bool {
	if m.EnabledFunc != nil {
		return m.EnabledFunc(ctx, level)
	}
	if m.next != nil {
		return m.next.Enabled(ctx, level)
	}
	return true
}

// Handle handles a log record.
func (m *MockHookHandler) Handle(ctx context.Context, r slog.Record) error {
	if m.hook != nil {
		newCtx, shouldLog := m.hook(ctx, r)
		if !shouldLog {
			return nil
		}
		ctx = newCtx
	}

	if m.HandleFunc != nil {
		return m.HandleFunc(ctx, r)
	}
	if m.next != nil {
		return m.next.Handle(ctx, r)
	}
	return nil
}

// WithAttrs returns a new handler with additional attributes.
func (m *MockHookHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if m.WithAttrsFunc != nil {
		return m.WithAttrsFunc(attrs)
	}
	return m
}

// WithGroup returns a new handler with a group name.
func (m *MockHookHandler) WithGroup(name string) slog.Handler {
	if m.WithGroupFunc != nil {
		return m.WithGroupFunc(name)
	}
	return m
}

// SetHook sets the hook function.
func (m *MockHookHandler) SetHook(hook HookFunc) {
	if m.SetHookFunc != nil {
		m.SetHookFunc(hook)
	} else {
		m.hook = hook
	}
}

// GetHook returns the current hook function.
func (m *MockHookHandler) GetHook() HookFunc {
	if m.GetHookFunc != nil {
		return m.GetHookFunc()
	}
	return m.hook
}

// NewMockHookHandler creates a new mock HookHandler.
func NewMockHookHandler(next slog.Handler, hook HookFunc) *MockHookHandler {
	return &MockHookHandler{
		next: next,
		hook: hook,
	}
}

// MockHookFunc implements HookFunc for testing.
type MockHookFunc struct {
	ExecuteFunc func(ctx context.Context, r slog.Record) (context.Context, bool)
}

// Execute executes the hook function.
func (m *MockHookFunc) Execute(ctx context.Context, r slog.Record) (context.Context, bool) {
	if m.ExecuteFunc != nil {
		return m.ExecuteFunc(ctx, r)
	}
	return ctx, true
}

// AsFunc returns the mock as a HookFunc.
func (m *MockHookFunc) AsFunc() HookFunc {
	return func(ctx context.Context, r slog.Record) (context.Context, bool) {
		return m.Execute(ctx, r)
	}
}

// NewMockHookFunc creates a new mock HookFunc.
func NewMockHookFunc() *MockHookFunc {
	return &MockHookFunc{}
}

// MockMultiHandler implements slog.Handler for testing multi handler functionality.
type MockMultiHandler struct {
	AddHandlerFunc      func(handler slog.Handler)
	EnabledFunc         func(ctx context.Context, level slog.Level) bool
	GetHandlerCountFunc func() int
	GetHandlersFunc     func() []slog.Handler
	HandleFunc          func(ctx context.Context, r slog.Record) error
	RemoveHandlerFunc   func(index int)
	WithAttrsFunc       func(attrs []slog.Attr) slog.Handler
	WithGroupFunc       func(name string) slog.Handler

	handlers []slog.Handler
}

// Enabled checks if the logging level is enabled.
func (m *MockMultiHandler) Enabled(ctx context.Context, level slog.Level) bool {
	if m.EnabledFunc != nil {
		return m.EnabledFunc(ctx, level)
	}
	for _, handler := range m.handlers {
		if handler.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

// Handle handles a log record.
func (m *MockMultiHandler) Handle(ctx context.Context, r slog.Record) error {
	if m.HandleFunc != nil {
		return m.HandleFunc(ctx, r)
	}
	var lastErr error
	for _, handler := range m.handlers {
		if err := handler.Handle(ctx, r); err != nil {
			lastErr = err
		}
	}
	return lastErr
}

// WithAttrs returns a new handler with additional attributes.
func (m *MockMultiHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if m.WithAttrsFunc != nil {
		return m.WithAttrsFunc(attrs)
	}
	newHandlers := make([]slog.Handler, len(m.handlers))
	for i, handler := range m.handlers {
		newHandlers[i] = handler.WithAttrs(attrs)
	}
	return &MockMultiHandler{handlers: newHandlers}
}

// WithGroup returns a new handler with a group name.
func (m *MockMultiHandler) WithGroup(name string) slog.Handler {
	if m.WithGroupFunc != nil {
		return m.WithGroupFunc(name)
	}
	newHandlers := make([]slog.Handler, 0, len(m.handlers))
	for _, handler := range m.handlers {
		newHandlers = append(newHandlers, handler.WithGroup(name))
	}
	return &MockMultiHandler{handlers: newHandlers}
}

// AddHandler adds a new handler to the multi handler.
func (m *MockMultiHandler) AddHandler(handler slog.Handler) {
	if m.AddHandlerFunc != nil {
		m.AddHandlerFunc(handler)
	} else {
		m.handlers = append(m.handlers, handler)
	}
}

// RemoveHandler removes a handler from the multi handler.
func (m *MockMultiHandler) RemoveHandler(index int) {
	if m.RemoveHandlerFunc != nil {
		m.RemoveHandlerFunc(index)
	} else {
		if index >= 0 && index < len(m.handlers) {
			m.handlers = append(m.handlers[:index], m.handlers[index+1:]...)
		}
	}
}

// GetHandlers returns all handlers.
func (m *MockMultiHandler) GetHandlers() []slog.Handler {
	if m.GetHandlersFunc != nil {
		return m.GetHandlersFunc()
	}
	return m.handlers
}

// GetHandlerCount returns the number of handlers.
func (m *MockMultiHandler) GetHandlerCount() int {
	if m.GetHandlerCountFunc != nil {
		return m.GetHandlerCountFunc()
	}
	return len(m.handlers)
}

// NewMockMultiHandler creates a new mock MultiHandler.
func NewMockMultiHandler(handlers ...slog.Handler) *MockMultiHandler {
	return &MockMultiHandler{
		handlers: handlers,
	}
}

// MockSamplingConfig implements SamplingConfig for testing.
type MockSamplingConfig struct {
	GetIntervalFunc    func() time.Duration
	GetMinLevelFunc    func() slog.Level
	GetProbabilityFunc func() float64
	IsValidFunc        func() bool
	SetIntervalFunc    func(interval time.Duration)
	SetMinLevelFunc    func(minLevel slog.Level)
	SetProbabilityFunc func(probability float64)
	ShouldSampleFunc   func(level slog.Level, lastLogTime time.Time) bool

	Interval    time.Duration
	MinLevel    slog.Level
	Probability float64
}

// GetInterval returns the interval of the sampling config.
func (m *MockSamplingConfig) GetInterval() time.Duration {
	if m.GetIntervalFunc != nil {
		return m.GetIntervalFunc()
	}
	return m.Interval
}

// GetProbability returns the probability of the sampling config.
func (m *MockSamplingConfig) GetProbability() float64 {
	if m.GetProbabilityFunc != nil {
		return m.GetProbabilityFunc()
	}
	return m.Probability
}

// GetMinLevel returns the minimum level of the sampling config.
func (m *MockSamplingConfig) GetMinLevel() slog.Level {
	if m.GetMinLevelFunc != nil {
		return m.GetMinLevelFunc()
	}
	return m.MinLevel
}

// SetInterval sets the interval of the sampling config.
func (m *MockSamplingConfig) SetInterval(interval time.Duration) {
	if m.SetIntervalFunc != nil {
		m.SetIntervalFunc(interval)
	} else {
		m.Interval = interval
	}
}

// SetProbability sets the probability of the sampling config.
func (m *MockSamplingConfig) SetProbability(probability float64) {
	if m.SetProbabilityFunc != nil {
		m.SetProbabilityFunc(probability)
	} else {
		m.Probability = probability
	}
}

// SetMinLevel sets the minimum level of the sampling config.
func (m *MockSamplingConfig) SetMinLevel(minLevel slog.Level) {
	if m.SetMinLevelFunc != nil {
		m.SetMinLevelFunc(minLevel)
	} else {
		m.MinLevel = minLevel
	}
}

// ShouldSample checks if a log should be sampled based on the configuration.
func (m *MockSamplingConfig) ShouldSample(level slog.Level, lastLogTime time.Time) bool {
	if m.ShouldSampleFunc != nil {
		return m.ShouldSampleFunc(level, lastLogTime)
	}
	return true
}

// IsValid checks if the sampling configuration is valid.
func (m *MockSamplingConfig) IsValid() bool {
	if m.IsValidFunc != nil {
		return m.IsValidFunc()
	}
	return m.Probability >= 0 && m.Probability <= 1
}

// NewMockSamplingConfig creates a new mock SamplingConfig.
func NewMockSamplingConfig(interval time.Duration, probability float64, minLevel slog.Level) *MockSamplingConfig {
	return &MockSamplingConfig{
		Interval:    interval,
		MinLevel:    minLevel,
		Probability: probability,
	}
}
