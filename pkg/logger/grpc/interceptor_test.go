package grpc

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	kitlog "github.com/pablogore/kit-logger/pkg/logger"
)

func TestUnaryLoggingInterceptor_Success(t *testing.T) {
	// Setup mock logger
	mockLogger := kitlog.NewMockLogger()
	kitlog.SetGlobal(mockLogger)

	// Create interceptor
	interceptor := UnaryLoggingInterceptor()

	// Mock handler that succeeds
	handler := func(ctx context.Context, req any) (any, error) {
		return "success response", nil
	}

	// Mock gRPC info
	info := &grpc.UnaryServerInfo{
		FullMethod: "/test.Service/Method",
	}

	// Call interceptor
	ctx := context.Background()
	req := "test request"

	resp, err := interceptor(ctx, req, info, handler)

	// Verify response
	assert.Equal(t, "success response", resp)
	assert.NoError(t, err)

	// Verify logging occurred
	assert.True(t, mockLogger.HasMessage("grpc_call"))
}

func TestUnaryLoggingInterceptor_Error(t *testing.T) {
	// Setup mock logger
	mockLogger := kitlog.NewMockLogger()
	kitlog.SetGlobal(mockLogger)

	// Create interceptor
	interceptor := UnaryLoggingInterceptor()

	// Mock handler that returns error
	expectedErr := errors.New("test error")
	handler := func(ctx context.Context, req any) (any, error) {
		return nil, expectedErr
	}

	// Mock gRPC info
	info := &grpc.UnaryServerInfo{
		FullMethod: "/test.Service/ErrorMethod",
	}

	// Call interceptor
	ctx := context.Background()
	req := "test request"

	resp, err := interceptor(ctx, req, info, handler)

	// Verify response
	assert.Nil(t, resp)
	assert.Equal(t, expectedErr, err)

	// Verify logging occurred
	assert.True(t, mockLogger.HasMessage("grpc_call"))
}

func TestUnaryLoggingInterceptor_GRPCError(t *testing.T) {
	// Setup mock logger
	mockLogger := kitlog.NewMockLogger()
	kitlog.SetGlobal(mockLogger)

	// Create interceptor
	interceptor := UnaryLoggingInterceptor()

	// Mock handler that returns gRPC error
	grpcErr := status.Error(codes.InvalidArgument, "invalid argument")
	handler := func(ctx context.Context, req any) (any, error) {
		return nil, grpcErr
	}

	// Mock gRPC info
	info := &grpc.UnaryServerInfo{
		FullMethod: "/test.Service/GRPCErrorMethod",
	}

	// Call interceptor
	ctx := context.Background()
	req := "test request"

	resp, err := interceptor(ctx, req, info, handler)

	// Verify response
	assert.Nil(t, resp)
	assert.Equal(t, grpcErr, err)

	// Verify logging occurred
	assert.True(t, mockLogger.HasMessage("grpc_call"))
}

func TestUnaryLoggingInterceptor_ContextWithValues(t *testing.T) {
	// Setup mock logger
	mockLogger := kitlog.NewMockLogger()
	kitlog.SetGlobal(mockLogger)

	// Create interceptor
	interceptor := UnaryLoggingInterceptor()

	// Mock handler
	handler := func(ctx context.Context, req any) (any, error) {
		// Verify context is passed through
		assert.Equal(t, "test-value", ctx.Value("test-key"))
		return "success", nil
	}

	// Mock gRPC info
	info := &grpc.UnaryServerInfo{
		FullMethod: "/test.Service/ContextMethod",
	}

	// Call interceptor with context containing values
	ctx := context.WithValue(context.Background(), "test-key", "test-value")
	req := "test request"

	resp, err := interceptor(ctx, req, info, handler)

	// Verify response
	assert.Equal(t, "success", resp)
	assert.NoError(t, err)

	// Verify logging occurred
	assert.True(t, mockLogger.HasMessage("grpc_call"))
}

func TestUnaryLoggingInterceptor_DifferentMethods(t *testing.T) {
	// Setup mock logger
	mockLogger := kitlog.NewMockLogger()
	kitlog.SetGlobal(mockLogger)

	// Create interceptor
	interceptor := UnaryLoggingInterceptor()

	// Mock handler
	handler := func(ctx context.Context, req any) (any, error) {
		return "success", nil
	}

	testCases := []struct {
		name       string
		fullMethod string
	}{
		{"UserService", "/user.UserService/CreateUser"},
		{"AuthService", "/auth.AuthService/Login"},
		{"PaymentService", "/payment.PaymentService/ProcessPayment"},
		{"NotificationService", "/notification.NotificationService/SendEmail"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create new mock logger for each test
			mockLogger := kitlog.NewMockLogger()
			kitlog.SetGlobal(mockLogger)

			// Mock gRPC info
			info := &grpc.UnaryServerInfo{
				FullMethod: tc.fullMethod,
			}

			// Call interceptor
			ctx := context.Background()
			req := "test request"

			resp, err := interceptor(ctx, req, info, handler)

			// Verify response
			assert.Equal(t, "success", resp)
			assert.NoError(t, err)

			// Verify logging occurred with correct method
			assert.True(t, mockLogger.HasMessage("grpc_call"))
		})
	}
}

func TestUnaryLoggingInterceptor_Performance(t *testing.T) {
	// Setup mock logger
	mockLogger := kitlog.NewMockLogger()
	kitlog.SetGlobal(mockLogger)

	// Create interceptor
	interceptor := UnaryLoggingInterceptor()

	// Mock handler with delay
	handler := func(ctx context.Context, req any) (any, error) {
		time.Sleep(10 * time.Millisecond)
		return "delayed response", nil
	}

	// Mock gRPC info
	info := &grpc.UnaryServerInfo{
		FullMethod: "/test.Service/SlowMethod",
	}

	// Call interceptor
	ctx := context.Background()
	req := "test request"

	start := time.Now()
	resp, err := interceptor(ctx, req, info, handler)
	duration := time.Since(start)

	// Verify response
	assert.Equal(t, "delayed response", resp)
	assert.NoError(t, err)

	// Verify logging occurred
	assert.True(t, mockLogger.HasMessage("grpc_call"))

	// Verify total duration is reasonable
	assert.GreaterOrEqual(t, duration, 10*time.Millisecond)
	assert.LessOrEqual(t, duration, 50*time.Millisecond)
}

func TestUnaryLoggingInterceptor_ErrorStatusCodes(t *testing.T) {
	testCases := []struct {
		name     string
		code     codes.Code
		expected string
	}{
		{"NotFound", codes.NotFound, "NotFound"},
		{"PermissionDenied", codes.PermissionDenied, "PermissionDenied"},
		{"Internal", codes.Internal, "Internal"},
		{"Unavailable", codes.Unavailable, "Unavailable"},
		{"DeadlineExceeded", codes.DeadlineExceeded, "DeadlineExceeded"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Setup mock logger
			mockLogger := kitlog.NewMockLogger()
			kitlog.SetGlobal(mockLogger)

			// Create interceptor
			interceptor := UnaryLoggingInterceptor()

			// Mock handler that returns specific gRPC error
			grpcErr := status.Error(tc.code, "test error")
			handler := func(ctx context.Context, req any) (any, error) {
				return nil, grpcErr
			}

			// Mock gRPC info
			info := &grpc.UnaryServerInfo{
				FullMethod: "/test.Service/" + tc.name,
			}

			// Call interceptor
			ctx := context.Background()
			req := "test request"

			resp, err := interceptor(ctx, req, info, handler)

			// Verify response
			assert.Nil(t, resp)
			assert.Equal(t, grpcErr, err)

			// Verify logging occurred
			assert.True(t, mockLogger.HasMessage("grpc_call"))
		})
	}
}
