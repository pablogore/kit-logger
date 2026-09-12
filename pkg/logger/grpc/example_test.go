package grpc_test

import (
	"github.com/pablogore/kit-logger/pkg/logger"
	kitgrpc "github.com/pablogore/kit-logger/pkg/logger/grpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// Example_server mirrors the README's HTTP & gRPC Integration snippet: the
// unary and stream server interceptors are installed on a grpc.Server. The
// server is never started here; the example verifies the snippet compiles
// against the real interceptor signatures.
func Example_server() {
	log := logger.New(logger.Config{Format: logger.FormatJSON})

	grpcServer := grpc.NewServer(
		grpc.UnaryInterceptor(kitgrpc.UnaryServerInterceptor(kitgrpc.Options{Logger: log})),
		grpc.StreamInterceptor(kitgrpc.StreamServerInterceptor(kitgrpc.Options{Logger: log})),
	)
	defer grpcServer.Stop()
}

// Example_options mirrors the README's gRPC interceptors section: one
// Options value shared by the server and client interceptors, with health
// checks skipped. The client is created lazily and never dials.
func Example_options() {
	log := logger.New(logger.Config{Format: logger.FormatJSON})

	opts := kitgrpc.Options{
		Logger:      log, // nil falls back to logger.L()
		SkipMethods: []string{"/grpc.health.v1.Health/Check"},
	}

	grpcServer := grpc.NewServer(
		grpc.UnaryInterceptor(kitgrpc.UnaryServerInterceptor(opts)),
		grpc.StreamInterceptor(kitgrpc.StreamServerInterceptor(opts)),
	)
	defer grpcServer.Stop()

	conn, err := grpc.NewClient("passthrough:///orders.internal",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithChainUnaryInterceptor(kitgrpc.UnaryClientInterceptor(opts)),
		grpc.WithChainStreamInterceptor(kitgrpc.StreamClientInterceptor(opts)),
	)
	if err != nil {
		panic(err)
	}
	defer func() { _ = conn.Close() }()
}
