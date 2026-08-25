package httpmw

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	kitlog "github.com/pablogore/kit-logger/pkg/logger"
)

func TestMiddleware_Success(t *testing.T) {
	// Setup mock logger
	mockLogger := kitlog.NewMockLogger()
	kitlog.SetGlobal(mockLogger)

	// Create middleware
	middleware := Middleware()

	// Create test handler
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("success"))
	})

	// Create request
	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()

	// Apply middleware
	middleware(handler).ServeHTTP(w, req)

	// Verify response
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "success", w.Body.String())

	// Verify logging occurred
	assert.True(t, mockLogger.HasMessage("http_request"))
}

func TestMiddleware_WithRequestID(t *testing.T) {
	// Setup mock logger
	mockLogger := kitlog.NewMockLogger()
	kitlog.SetGlobal(mockLogger)

	// Create middleware
	middleware := Middleware()

	// Create test handler
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("success"))
	})

	// Create request with request ID
	req := httptest.NewRequest("POST", "/api/users", nil)
	req.Header.Set("X-Request-Id", "test-request-123")
	w := httptest.NewRecorder()

	// Apply middleware
	middleware(handler).ServeHTTP(w, req)

	// Verify response
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "success", w.Body.String())

	// Verify logging occurred
	assert.True(t, mockLogger.HasMessage("http_request"))
}

func TestMiddleware_WithoutRequestID(t *testing.T) {
	// Setup mock logger
	mockLogger := kitlog.NewMockLogger()
	kitlog.SetGlobal(mockLogger)

	// Create middleware
	middleware := Middleware()

	// Create test handler
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte("created"))
	})

	// Create request without request ID
	req := httptest.NewRequest("POST", "/api/users", nil)
	w := httptest.NewRecorder()

	// Apply middleware
	middleware(handler).ServeHTTP(w, req)

	// Verify response
	assert.Equal(t, http.StatusCreated, w.Code)
	assert.Equal(t, "created", w.Body.String())

	// Verify logging occurred
	assert.True(t, mockLogger.HasMessage("http_request"))
}

func TestMiddleware_DifferentMethods(t *testing.T) {
	// Setup mock logger
	mockLogger := kitlog.NewMockLogger()
	kitlog.SetGlobal(mockLogger)

	// Create middleware
	middleware := Middleware()

	testCases := []struct {
		name           string
		method         string
		path           string
		expectedStatus int
		expectedBody   string
	}{
		{"GET", "GET", "/api/users", http.StatusOK, "users"},
		{"POST", "POST", "/api/users", http.StatusCreated, "created"},
		{"PUT", "PUT", "/api/users/123", http.StatusOK, "updated"},
		{"DELETE", "DELETE", "/api/users/123", http.StatusNoContent, ""},
		{"PATCH", "PATCH", "/api/users/123", http.StatusOK, "patched"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create new mock logger for each test
			mockLogger := kitlog.NewMockLogger()
			kitlog.SetGlobal(mockLogger)

			// Create test handler
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.expectedStatus)
				if tc.expectedBody != "" {
					w.Write([]byte(tc.expectedBody))
				}
			})

			// Create request
			req := httptest.NewRequest(tc.method, tc.path, nil)
			w := httptest.NewRecorder()

			// Apply middleware
			middleware(handler).ServeHTTP(w, req)

			// Verify response
			assert.Equal(t, tc.expectedStatus, w.Code)
			assert.Equal(t, tc.expectedBody, w.Body.String())

			// Verify logging occurred
			assert.True(t, mockLogger.HasMessage("http_request"))
		})
	}
}

func TestMiddleware_ErrorStatusCodes(t *testing.T) {
	// Setup mock logger
	mockLogger := kitlog.NewMockLogger()
	kitlog.SetGlobal(mockLogger)

	// Create middleware
	middleware := Middleware()

	testCases := []struct {
		name           string
		statusCode     int
		expectedStatus int
	}{
		{"BadRequest", http.StatusBadRequest, http.StatusBadRequest},
		{"Unauthorized", http.StatusUnauthorized, http.StatusUnauthorized},
		{"Forbidden", http.StatusForbidden, http.StatusForbidden},
		{"NotFound", http.StatusNotFound, http.StatusNotFound},
		{"InternalServerError", http.StatusInternalServerError, http.StatusInternalServerError},
		{"ServiceUnavailable", http.StatusServiceUnavailable, http.StatusServiceUnavailable},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create new mock logger for each test
			mockLogger := kitlog.NewMockLogger()
			kitlog.SetGlobal(mockLogger)

			// Create test handler
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.statusCode)
				w.Write([]byte("error"))
			})

			// Create request
			req := httptest.NewRequest("GET", "/test", nil)
			w := httptest.NewRecorder()

			// Apply middleware
			middleware(handler).ServeHTTP(w, req)

			// Verify response
			assert.Equal(t, tc.expectedStatus, w.Code)
			assert.Equal(t, "error", w.Body.String())

			// Verify logging occurred
			assert.True(t, mockLogger.HasMessage("http_request"))
		})
	}
}

func TestMiddleware_ContextPreservation(t *testing.T) {
	// Setup mock logger
	mockLogger := kitlog.NewMockLogger()
	kitlog.SetGlobal(mockLogger)

	// Create middleware
	middleware := Middleware()

	// Create test handler that checks context
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify context is preserved
		ctx := r.Context()
		assert.NotNil(t, ctx)

		// Add value to context
		ctx = context.WithValue(ctx, "test-key", "test-value")
		r = r.WithContext(ctx)

		w.WriteHeader(http.StatusOK)
		w.Write([]byte("context preserved"))
	})

	// Create request
	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()

	// Apply middleware
	middleware(handler).ServeHTTP(w, req)

	// Verify response
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "context preserved", w.Body.String())

	// Verify logging occurred
	assert.True(t, mockLogger.HasMessage("http_request"))
}

func TestMiddleware_Performance(t *testing.T) {
	// Setup mock logger
	mockLogger := kitlog.NewMockLogger()
	kitlog.SetGlobal(mockLogger)

	// Create middleware
	middleware := Middleware()

	// Create test handler with delay
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(10 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("delayed response"))
	})

	// Create request
	req := httptest.NewRequest("GET", "/slow", nil)
	w := httptest.NewRecorder()

	// Apply middleware
	start := time.Now()
	middleware(handler).ServeHTTP(w, req)
	duration := time.Since(start)

	// Verify response
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "delayed response", w.Body.String())

	// Verify logging occurred
	assert.True(t, mockLogger.HasMessage("http_request"))

	// Verify duration is reasonable
	assert.GreaterOrEqual(t, duration, 10*time.Millisecond)
	assert.LessOrEqual(t, duration, 50*time.Millisecond)
}

func TestMiddleware_ComplexPaths(t *testing.T) {
	// Setup mock logger
	mockLogger := kitlog.NewMockLogger()
	kitlog.SetGlobal(mockLogger)

	// Create middleware
	middleware := Middleware()

	testCases := []struct {
		name string
		path string
	}{
		{"Root", "/"},
		{"API", "/api"},
		{"Nested", "/api/v1/users/123/posts/456"},
		{"QueryParams", "/api/users?page=1&limit=10"},
		{"SpecialChars", "/api/users/123%20test"},
		{"LongPath", "/api/very/long/path/with/many/segments/and/more/segments"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create new mock logger for each test
			mockLogger := kitlog.NewMockLogger()
			kitlog.SetGlobal(mockLogger)

			// Create test handler
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				w.Write([]byte("success"))
			})

			// Create request
			req := httptest.NewRequest("GET", tc.path, nil)
			w := httptest.NewRecorder()

			// Apply middleware
			middleware(handler).ServeHTTP(w, req)

			// Verify response
			assert.Equal(t, http.StatusOK, w.Code)
			assert.Equal(t, "success", w.Body.String())

			// Verify logging occurred
			assert.True(t, mockLogger.HasMessage("http_request"))
		})
	}
}

func TestResponseRecorder_WriteHeader(t *testing.T) {
	// Create a mock response writer
	mockWriter := &mockResponseWriter{}

	// Create response recorder
	rr := &responseRecorder{
		ResponseWriter: mockWriter,
		status:         200,
	}

	// Test WriteHeader
	rr.WriteHeader(http.StatusNotFound)

	// Verify status was captured
	assert.Equal(t, http.StatusNotFound, rr.status)

	// Verify underlying writer was called
	assert.Equal(t, http.StatusNotFound, mockWriter.statusCode)
}

// Mock response writer for testing
type mockResponseWriter struct {
	statusCode int
}

func (m *mockResponseWriter) Header() http.Header {
	return make(http.Header)
}

func (m *mockResponseWriter) Write(data []byte) (int, error) {
	return len(data), nil
}

func (m *mockResponseWriter) WriteHeader(statusCode int) {
	m.statusCode = statusCode
}
