package utils

import (
	"log/slog"
)

// MockLogUtils implements log utilities functionality for testing.
type MockLogUtils struct {
	ExtractAttrsFunc func(r slog.Record) map[string]interface{}
}

// ExtractAttrs extracts the attributes from a slog.Record as a map.
func (m *MockLogUtils) ExtractAttrs(r slog.Record) map[string]interface{} {
	if m.ExtractAttrsFunc != nil {
		return m.ExtractAttrsFunc(r)
	}
	return make(map[string]interface{})
}

// NewMockLogUtils creates a new mock LogUtils.
func NewMockLogUtils() *MockLogUtils {
	return &MockLogUtils{}
}

// MockSlogRecord implements slog.Record for testing.
type MockSlogRecord struct {
	AddAttrFunc    func(key string, value interface{})
	AttrsFunc      func(f func(slog.Attr) bool)
	ClearAttrsFunc func()
	GetAttrsFunc   func() map[string]interface{}

	attrs map[string]interface{}
}

// AddAttr adds an attribute to the mock record.
func (m *MockSlogRecord) AddAttr(key string, value interface{}) {
	if m.AddAttrFunc != nil {
		m.AddAttrFunc(key, value)
	} else {
		if m.attrs == nil {
			m.attrs = make(map[string]interface{})
		}
		m.attrs[key] = value
	}
}

// Attrs iterates over attributes.
func (m *MockSlogRecord) Attrs(f func(slog.Attr) bool) {
	if m.AttrsFunc != nil {
		m.AttrsFunc(f)
	} else {
		for key, value := range m.attrs {
			attr := slog.Any(key, value)
			if !f(attr) {
				break
			}
		}
	}
}

// GetAttrs returns the attributes as a map.
func (m *MockSlogRecord) GetAttrs() map[string]interface{} {
	if m.GetAttrsFunc != nil {
		return m.GetAttrsFunc()
	}
	return m.attrs
}

// ClearAttrs clears all attributes.
func (m *MockSlogRecord) ClearAttrs() {
	if m.ClearAttrsFunc != nil {
		m.ClearAttrsFunc()
	} else {
		m.attrs = make(map[string]interface{})
	}
}

// NewMockSlogRecord creates a new mock SlogRecord.
func NewMockSlogRecord() *MockSlogRecord {
	return &MockSlogRecord{
		attrs: make(map[string]interface{}),
	}
}
