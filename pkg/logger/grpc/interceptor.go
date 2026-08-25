package grpc

import (
	"context"

	kitlog "github.com/pablogore/kit-logger/pkg/logger"

	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/status"
)

// UnaryLoggingInterceptor generates an interceptor that logs each gRPC call.
func UnaryLoggingInterceptor() grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req any,
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (resp any, err error) {
		start := time.Now()           // Record the start time of the request.
		resp, err = handler(ctx, req) // Handle the request.
		duration := time.Since(start) // Calculate the duration of the request.

		st, _ := status.FromError(err) // Extract the status from the error.

		// Structured logging of the gRPC call.
		kitlog.L().InfoContext(ctx, "grpc_call",
			"method", info.FullMethod, // Full method name of the gRPC call.
			"duration_ms", duration.Milliseconds(), // Duration of the call in milliseconds.
			"error", err, // Error returned by the handler.
			"code", st.Code().String(), // Status code of the gRPC call.
		)

		return resp, err // Return the response and error.
	}
}
