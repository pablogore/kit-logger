package httpmw

import (
	"net/http"
)

// MockMiddleware implements HTTP middleware functionality for testing.
type MockMiddleware struct {
	MiddlewareFunc func() func(http.Handler) http.Handler
}

// Middleware returns the middleware function.
func (m *MockMiddleware) Middleware() func(http.Handler) http.Handler {
	if m.MiddlewareFunc != nil {
		return m.MiddlewareFunc()
	}
	return func(next http.Handler) http.Handler {
		return next
	}
}

// NewMockMiddleware creates a new mock Middleware.
func NewMockMiddleware() *MockMiddleware {
	return &MockMiddleware{}
}

// MockResponseRecorder implements http.ResponseWriter for testing.
type MockResponseRecorder struct {
	GetStatusFunc   func() int
	HeaderFunc      func() http.Header
	WriteFunc       func(data []byte) (int, error)
	WriteHeaderFunc func(code int)

	ResponseWriter http.ResponseWriter
	status         int
}

// WriteHeader writes the HTTP response header.
func (m *MockResponseRecorder) WriteHeader(code int) {
	m.status = code
	if m.WriteHeaderFunc != nil {
		m.WriteHeaderFunc(code)
	} else if m.ResponseWriter != nil {
		m.ResponseWriter.WriteHeader(code)
	}
}

// Write writes the HTTP response body.
func (m *MockResponseRecorder) Write(data []byte) (int, error) {
	if m.WriteFunc != nil {
		return m.WriteFunc(data)
	}
	if m.ResponseWriter != nil {
		return m.ResponseWriter.Write(data)
	}
	return len(data), nil
}

// Header returns the HTTP response headers.
func (m *MockResponseRecorder) Header() http.Header {
	if m.HeaderFunc != nil {
		return m.HeaderFunc()
	}
	if m.ResponseWriter != nil {
		return m.ResponseWriter.Header()
	}
	return make(http.Header)
}

// GetStatus returns the current status code.
func (m *MockResponseRecorder) GetStatus() int {
	if m.GetStatusFunc != nil {
		return m.GetStatusFunc()
	}
	return m.status
}

// NewMockResponseRecorder creates a new mock ResponseRecorder.
func NewMockResponseRecorder(w http.ResponseWriter) *MockResponseRecorder {
	return &MockResponseRecorder{
		ResponseWriter: w,
		status:         200,
	}
}
