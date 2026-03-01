package grpc

import (
	"context"

	"google.golang.org/grpc"
)

// MockUnaryLoggingInterceptor implements gRPC interceptor functionality for testing.
type MockUnaryLoggingInterceptor struct {
	UnaryLoggingInterceptorFunc func() grpc.UnaryServerInterceptor
}

// UnaryLoggingInterceptor returns the unary logging interceptor function.
func (m *MockUnaryLoggingInterceptor) UnaryLoggingInterceptor() grpc.UnaryServerInterceptor {
	if m.UnaryLoggingInterceptorFunc != nil {
		return m.UnaryLoggingInterceptorFunc()
	}
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		return handler(ctx, req)
	}
}

// NewMockUnaryLoggingInterceptor creates a new mock UnaryLoggingInterceptor.
func NewMockUnaryLoggingInterceptor() *MockUnaryLoggingInterceptor {
	return &MockUnaryLoggingInterceptor{}
}

// MockUnaryHandler implements grpc.UnaryHandler for testing.
type MockUnaryHandler struct {
	HandleFunc func(ctx context.Context, req any) (any, error)
}

// Handle handles a unary gRPC request.
func (m *MockUnaryHandler) Handle(ctx context.Context, req any) (any, error) {
	if m.HandleFunc != nil {
		return m.HandleFunc(ctx, req)
	}
	return nil, nil
}

// AsFunc returns the mock as a grpc.UnaryHandler.
func (m *MockUnaryHandler) AsFunc() grpc.UnaryHandler {
	return func(ctx context.Context, req any) (any, error) {
		return m.Handle(ctx, req)
	}
}

// NewMockUnaryHandler creates a new mock UnaryHandler.
func NewMockUnaryHandler() *MockUnaryHandler {
	return &MockUnaryHandler{}
}

// MockUnaryServerInfo implements grpc.UnaryServerInfo for testing.
type MockUnaryServerInfo struct {
	FullMethod        string
	GetFullMethodFunc func() string
	GetServerFunc     func() any
	Server            any
}

// GetFullMethod returns the full method name.
func (m *MockUnaryServerInfo) GetFullMethod() string {
	if m.GetFullMethodFunc != nil {
		return m.GetFullMethodFunc()
	}
	return m.FullMethod
}

// GetServer returns the server instance.
func (m *MockUnaryServerInfo) GetServer() any {
	if m.GetServerFunc != nil {
		return m.GetServerFunc()
	}
	return m.Server
}

// NewMockUnaryServerInfo creates a new mock UnaryServerInfo.
func NewMockUnaryServerInfo(fullMethod string, server any) *MockUnaryServerInfo {
	return &MockUnaryServerInfo{
		FullMethod: fullMethod,
		Server:     server,
	}
}
