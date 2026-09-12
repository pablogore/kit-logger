package grpc

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	kitlog "github.com/pablogore/kit-logger/pkg/logger"
	"github.com/pablogore/kit-logger/pkg/logger/kitlogtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// lastEntry returns the most recent entry logged under msg, failing the test
// if there is none.
func lastEntry(t *testing.T, log *kitlogtest.MockLogger, msg string) kitlogtest.LogEntry {
	t.Helper()
	var found *kitlogtest.LogEntry
	for i := range log.Entries {
		if log.Entries[i].Message == msg {
			e := log.Entries[i]
			found = &e
		}
	}
	require.NotNil(t, found, "no entry logged for %q", msg)
	return *found
}

// serverEntry returns the last entry logged under msg that carries a "peer"
// field. In the integration tests both a server-side and a client-side
// interceptor log under the same message name for one call; "peer" is only
// ever added by the server side, so it disambiguates which line is which.
func serverEntry(t *testing.T, log *kitlogtest.MockLogger, msg string) kitlogtest.LogEntry {
	t.Helper()
	var found *kitlogtest.LogEntry
	for i := range log.Entries {
		if log.Entries[i].Message == msg && hasArg(log.Entries[i].Args, "peer") {
			e := log.Entries[i]
			found = &e
		}
	}
	require.NotNil(t, found, "no server-side entry logged for %q", msg)
	return *found
}

// clientEntry is serverEntry's counterpart: it isolates the client-side
// interceptor's own log line among entries sharing the same message name,
// since only a server-side entry carries "peer".
func clientEntry(t *testing.T, log *kitlogtest.MockLogger, msg string) kitlogtest.LogEntry {
	t.Helper()
	var found *kitlogtest.LogEntry
	for i := range log.Entries {
		if log.Entries[i].Message == msg && !hasArg(log.Entries[i].Args, "peer") {
			e := log.Entries[i]
			found = &e
		}
	}
	require.NotNil(t, found, "no client-side entry logged for %q", msg)
	return *found
}

// argValue returns the value following key in a flat args slice, failing the
// test if key is absent.
func argValue(t *testing.T, args []any, key string) any {
	t.Helper()
	for i := 0; i+1 < len(args); i += 2 {
		if args[i] == key {
			return args[i+1]
		}
	}
	require.Fail(t, "field not found", "key %q not present in %v", key, args)
	return nil
}

func hasArg(args []any, key string) bool {
	for i := 0; i+1 < len(args); i += 2 {
		if args[i] == key {
			return true
		}
	}
	return false
}

// --- UnaryServerInterceptor ---------------------------------------------

func TestUnaryServerInterceptor_LevelMapping(t *testing.T) {
	cases := []struct {
		name string
		code codes.Code
		want slog.Level
	}{
		{"OK", codes.OK, slog.LevelInfo},
		{"NotFound", codes.NotFound, slog.LevelWarn},
		{"InvalidArgument", codes.InvalidArgument, slog.LevelWarn},
		{"Internal", codes.Internal, slog.LevelError},
		{"Unknown", codes.Unknown, slog.LevelError},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			log := kitlogtest.NewMockLogger()
			interceptor := UnaryServerInterceptor(Options{Logger: log})

			handler := func(ctx context.Context, req any) (any, error) {
				if tc.code == codes.OK {
					return "ok", nil
				}
				return nil, status.Error(tc.code, "boom")
			}
			info := &grpc.UnaryServerInfo{FullMethod: "/test.Service/Method"}

			_, _ = interceptor(context.Background(), "req", info, handler)

			entry := lastEntry(t, log, "grpc_call")
			assert.Equal(t, tc.want, entry.Level)
			assert.Equal(t, tc.code.String(), argValue(t, entry.Args, "code"))
		})
	}
}

func TestUnaryServerInterceptor_ExplicitLoggerNeverFallsBackToGlobal(t *testing.T) {
	globalLog := kitlogtest.NewMockLogger()
	kitlog.SetGlobal(globalLog)

	injected := kitlogtest.NewMockLogger()
	interceptor := UnaryServerInterceptor(Options{Logger: injected})

	handler := func(ctx context.Context, req any) (any, error) { return "ok", nil }
	info := &grpc.UnaryServerInfo{FullMethod: "/test.Service/Method"}
	_, _ = interceptor(context.Background(), "req", info, handler)

	assert.True(t, injected.HasMessage("grpc_call"))
	assert.False(t, globalLog.HasMessage("grpc_call"), "an interceptor built with an explicit Logger must never fall back to the global one")
}

func TestUnaryServerInterceptor_CorrelationIDFromMetadata(t *testing.T) {
	log := kitlogtest.NewMockLogger()
	interceptor := UnaryServerInterceptor(Options{Logger: log})

	var seenID string
	handler := func(ctx context.Context, req any) (any, error) {
		id, ok := RequestIDFrom(ctx)
		require.True(t, ok, "handler must observe the correlation ID through its context")
		seenID = id
		return "ok", nil
	}
	info := &grpc.UnaryServerInfo{FullMethod: "/test.Service/Method"}

	md := metadata.New(map[string]string{"x-request-id": "req-123"})
	ctx := metadata.NewIncomingContext(context.Background(), md)

	_, _ = interceptor(ctx, "req", info, handler)

	assert.Equal(t, "req-123", seenID)
	entry := lastEntry(t, log, "grpc_call")
	assert.Equal(t, "req-123", argValue(t, entry.Args, "request_id"))
}

func TestUnaryServerInterceptor_NoCorrelationIDMeansNoField(t *testing.T) {
	log := kitlogtest.NewMockLogger()
	interceptor := UnaryServerInterceptor(Options{Logger: log})

	handler := func(ctx context.Context, req any) (any, error) { return "ok", nil }
	info := &grpc.UnaryServerInfo{FullMethod: "/test.Service/Method"}

	_, _ = interceptor(context.Background(), "req", info, handler)

	entry := lastEntry(t, log, "grpc_call")
	assert.False(t, hasArg(entry.Args, "request_id"))
}

func TestUnaryServerInterceptor_SkipMethodProducesNoLine(t *testing.T) {
	log := kitlogtest.NewMockLogger()
	interceptor := UnaryServerInterceptor(Options{Logger: log, SkipMethods: []string{"/health.Health/Check"}})

	called := false
	handler := func(ctx context.Context, req any) (any, error) {
		called = true
		return "ok", nil
	}
	info := &grpc.UnaryServerInfo{FullMethod: "/health.Health/Check"}

	resp, err := interceptor(context.Background(), "req", info, handler)

	require.NoError(t, err)
	assert.Equal(t, "ok", resp)
	assert.True(t, called, "a skipped method must still be served")
	assert.False(t, log.HasMessage("grpc_call"), "a skipped method must produce no log line")
}

func TestUnaryServerInterceptor_PayloadsOmittedByDefault(t *testing.T) {
	log := kitlogtest.NewMockLogger()
	interceptor := UnaryServerInterceptor(Options{Logger: log})

	handler := func(ctx context.Context, req any) (any, error) { return "the-response", nil }
	info := &grpc.UnaryServerInfo{FullMethod: "/test.Service/Method"}

	_, _ = interceptor(context.Background(), "the-request", info, handler)

	entry := lastEntry(t, log, "grpc_call")
	assert.False(t, hasArg(entry.Args, "request"), "LogPayloads defaults to false: no request field")
	assert.False(t, hasArg(entry.Args, "response"), "LogPayloads defaults to false: no response field")
}

func TestUnaryServerInterceptor_PayloadsLoggedWhenOptedIn(t *testing.T) {
	log := kitlogtest.NewMockLogger()
	interceptor := UnaryServerInterceptor(Options{Logger: log, LogPayloads: true})

	handler := func(ctx context.Context, req any) (any, error) { return "the-response", nil }
	info := &grpc.UnaryServerInfo{FullMethod: "/test.Service/Method"}

	_, _ = interceptor(context.Background(), "the-request", info, handler)

	entry := lastEntry(t, log, "grpc_call")
	assert.Equal(t, "the-request", argValue(t, entry.Args, "request"))
	assert.Equal(t, "the-response", argValue(t, entry.Args, "response"))
}

func TestUnaryServerInterceptor_PanicConvertsToInternalByDefault(t *testing.T) {
	log := kitlogtest.NewMockLogger()
	interceptor := UnaryServerInterceptor(Options{Logger: log})

	handler := func(ctx context.Context, req any) (any, error) {
		panic("boom")
	}
	info := &grpc.UnaryServerInfo{FullMethod: "/test.Service/Method"}

	var resp any
	var err error
	require.NotPanics(t, func() {
		resp, err = interceptor(context.Background(), "req", info, handler)
	})

	assert.Nil(t, resp)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Internal, st.Code())

	entry := lastEntry(t, log, "grpc_call")
	assert.Equal(t, slog.LevelError, entry.Level)
	assert.Equal(t, "boom", argValue(t, entry.Args, "panic"))
	assert.NotEmpty(t, argValue(t, entry.Args, "stack"))
}

func TestUnaryServerInterceptor_PanicRepanicsWhenRecoveryDisabled(t *testing.T) {
	log := kitlogtest.NewMockLogger()
	interceptor := UnaryServerInterceptor(Options{Logger: log, DisablePanicRecovery: true})

	handler := func(ctx context.Context, req any) (any, error) {
		panic("boom")
	}
	info := &grpc.UnaryServerInfo{FullMethod: "/test.Service/Method"}

	assert.Panics(t, func() {
		_, _ = interceptor(context.Background(), "req", info, handler)
	})
	assert.True(t, log.HasMessage("grpc_call"), "the panic must still be logged before it is re-raised")
}

// --- StreamServerInterceptor ---------------------------------------------

type fakeServerStream struct {
	ctx  context.Context
	recv []any
	sent []any
}

func (f *fakeServerStream) SetHeader(metadata.MD) error  { return nil }
func (f *fakeServerStream) SendHeader(metadata.MD) error { return nil }
func (f *fakeServerStream) SetTrailer(metadata.MD)       {}
func (f *fakeServerStream) Context() context.Context     { return f.ctx }
func (f *fakeServerStream) SendMsg(m any) error {
	f.sent = append(f.sent, m)
	return nil
}
func (f *fakeServerStream) RecvMsg(m any) error {
	if len(f.recv) == 0 {
		return io.EOF
	}
	f.recv = f.recv[1:]
	return nil
}

func TestStreamServerInterceptor_CountsMessagesAndLogsOnce(t *testing.T) {
	log := kitlogtest.NewMockLogger()
	interceptor := StreamServerInterceptor(Options{Logger: log})

	base := &fakeServerStream{ctx: context.Background(), recv: []any{1, 2, 3}}
	info := &grpc.StreamServerInfo{FullMethod: "/test.Service/Stream"}

	handler := func(srv any, stream grpc.ServerStream) error {
		for i := 0; i < 3; i++ {
			require.NoError(t, stream.RecvMsg(new(any)))
		}
		require.ErrorIs(t, stream.RecvMsg(new(any)), io.EOF)
		for i := 0; i < 5; i++ {
			require.NoError(t, stream.SendMsg("x"))
		}
		return nil
	}

	err := interceptor(nil, base, info, handler)
	require.NoError(t, err)

	entries := 0
	for _, e := range log.Entries {
		if e.Message == "grpc_call" {
			entries++
		}
	}
	assert.Equal(t, 1, entries, "exactly one grpc_call record per stream")

	entry := lastEntry(t, log, "grpc_call")
	assert.Equal(t, int64(3), argValue(t, entry.Args, "msg_received"))
	assert.Equal(t, int64(5), argValue(t, entry.Args, "msg_sent"))
	assert.Equal(t, codes.OK.String(), argValue(t, entry.Args, "code"))
}

func TestStreamServerInterceptor_ContextCarriesCorrelationID(t *testing.T) {
	log := kitlogtest.NewMockLogger()
	interceptor := StreamServerInterceptor(Options{Logger: log})

	md := metadata.New(map[string]string{"x-request-id": "stream-req-1"})
	ctx := metadata.NewIncomingContext(context.Background(), md)
	base := &fakeServerStream{ctx: ctx}
	info := &grpc.StreamServerInfo{FullMethod: "/test.Service/Stream"}

	var seenID string
	handler := func(srv any, stream grpc.ServerStream) error {
		id, ok := RequestIDFrom(stream.Context())
		require.True(t, ok)
		seenID = id
		return nil
	}

	err := interceptor(nil, base, info, handler)
	require.NoError(t, err)
	assert.Equal(t, "stream-req-1", seenID)
}

func TestStreamServerInterceptor_PanicConvertsToInternalByDefault(t *testing.T) {
	log := kitlogtest.NewMockLogger()
	interceptor := StreamServerInterceptor(Options{Logger: log})

	base := &fakeServerStream{ctx: context.Background()}
	info := &grpc.StreamServerInfo{FullMethod: "/test.Service/Stream"}

	handler := func(srv any, stream grpc.ServerStream) error {
		panic("stream boom")
	}

	var err error
	require.NotPanics(t, func() {
		err = interceptor(nil, base, info, handler)
	})
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Internal, st.Code())

	entry := lastEntry(t, log, "grpc_call")
	assert.Equal(t, "stream boom", argValue(t, entry.Args, "panic"))
}

func TestStreamServerInterceptor_SkipMethodProducesNoLine(t *testing.T) {
	log := kitlogtest.NewMockLogger()
	interceptor := StreamServerInterceptor(Options{Logger: log, SkipMethods: []string{"/health.Health/Watch"}})

	base := &fakeServerStream{ctx: context.Background()}
	info := &grpc.StreamServerInfo{FullMethod: "/health.Health/Watch"}

	called := false
	handler := func(srv any, stream grpc.ServerStream) error {
		called = true
		return nil
	}

	err := interceptor(nil, base, info, handler)
	require.NoError(t, err)
	assert.True(t, called)
	assert.False(t, log.HasMessage("grpc_call"))
}

// --- UnaryClientInterceptor ----------------------------------------------

func TestUnaryClientInterceptor_LogsRequestIDAndCode(t *testing.T) {
	log := kitlogtest.NewMockLogger()
	interceptor := UnaryClientInterceptor(Options{Logger: log})

	ctx := context.WithValue(context.Background(), requestIDKey{}, "client-req-1")

	var capturedCtx context.Context
	invoker := func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, opts ...grpc.CallOption) error {
		capturedCtx = ctx
		return nil
	}

	err := interceptor(ctx, "/test.Service/Method", "req", "reply", nil, invoker)
	require.NoError(t, err)

	md, ok := metadata.FromOutgoingContext(capturedCtx)
	require.True(t, ok, "the correlation ID must be propagated on the outgoing call")
	assert.Equal(t, []string{"client-req-1"}, md.Get("x-request-id"))

	entry := lastEntry(t, log, "grpc_call")
	assert.Equal(t, "client-req-1", argValue(t, entry.Args, "request_id"))
	assert.Equal(t, codes.OK.String(), argValue(t, entry.Args, "code"))
}

func TestUnaryClientInterceptor_ErrorLogsCodeAndMessage(t *testing.T) {
	log := kitlogtest.NewMockLogger()
	interceptor := UnaryClientInterceptor(Options{Logger: log})

	wantErr := status.Error(codes.Unavailable, "down")
	invoker := func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, opts ...grpc.CallOption) error {
		return wantErr
	}

	err := interceptor(context.Background(), "/test.Service/Method", "req", "reply", nil, invoker)
	assert.Equal(t, wantErr, err)

	entry := lastEntry(t, log, "grpc_call")
	assert.Equal(t, slog.LevelWarn, entry.Level)
	assert.Equal(t, codes.Unavailable.String(), argValue(t, entry.Args, "code"))
	assert.Equal(t, wantErr.Error(), argValue(t, entry.Args, "error"))
}

// --- StreamClientInterceptor ----------------------------------------------

type fakeClientStream struct {
	recv         []any
	sent         []any
	sendErr      error
	closeSendErr error
}

func (f *fakeClientStream) Header() (metadata.MD, error) { return nil, nil }
func (f *fakeClientStream) Trailer() metadata.MD         { return nil }
func (f *fakeClientStream) CloseSend() error             { return f.closeSendErr }
func (f *fakeClientStream) Context() context.Context     { return context.Background() }
func (f *fakeClientStream) SendMsg(m any) error {
	if f.sendErr != nil {
		return f.sendErr
	}
	f.sent = append(f.sent, m)
	return nil
}
func (f *fakeClientStream) RecvMsg(m any) error {
	if len(f.recv) == 0 {
		return io.EOF
	}
	f.recv = f.recv[1:]
	return nil
}

// blockingClientStream never resolves RecvMsg/SendMsg on its own; it exists
// to prove the ctx.Done() watcher, not RecvMsg, is what finalizes a stream
// whose caller stops driving it.
type blockingClientStream struct {
	fakeClientStream
	block chan struct{}
}

func (f *blockingClientStream) RecvMsg(m any) error {
	<-f.block
	return io.EOF
}

func TestStreamClientInterceptor_CountsMessagesAndLogsOnceOnEOF(t *testing.T) {
	log := kitlogtest.NewMockLogger()
	interceptor := StreamClientInterceptor(Options{Logger: log})

	fake := &fakeClientStream{recv: []any{1, 2}}
	streamer := func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		return fake, nil
	}

	cs, err := interceptor(context.Background(), &grpc.StreamDesc{ServerStreams: true}, nil, "/test.Service/Stream", streamer)
	require.NoError(t, err)

	require.NoError(t, cs.SendMsg("a"))
	require.NoError(t, cs.SendMsg("b"))
	require.NoError(t, cs.SendMsg("c"))
	require.NoError(t, cs.RecvMsg(new(any)))
	require.NoError(t, cs.RecvMsg(new(any)))
	require.ErrorIs(t, cs.RecvMsg(new(any)), io.EOF)
	// A second terminal RecvMsg must not produce a second log line.
	require.ErrorIs(t, cs.RecvMsg(new(any)), io.EOF)

	entries := 0
	for _, e := range log.Entries {
		if e.Message == "grpc_call" {
			entries++
		}
	}
	assert.Equal(t, 1, entries)

	entry := lastEntry(t, log, "grpc_call")
	assert.Equal(t, int64(3), argValue(t, entry.Args, "msg_sent"))
	assert.Equal(t, int64(2), argValue(t, entry.Args, "msg_received"))
	assert.Equal(t, codes.OK.String(), argValue(t, entry.Args, "code"))
}

func TestStreamClientInterceptor_StreamEstablishFailureLogsImmediately(t *testing.T) {
	log := kitlogtest.NewMockLogger()
	interceptor := StreamClientInterceptor(Options{Logger: log})

	wantErr := status.Error(codes.Unavailable, "no connection")
	streamer := func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		return nil, wantErr
	}

	cs, err := interceptor(context.Background(), &grpc.StreamDesc{}, nil, "/test.Service/Stream", streamer)
	assert.Nil(t, cs)
	assert.Equal(t, wantErr, err)

	entry := lastEntry(t, log, "grpc_call")
	assert.Equal(t, codes.Unavailable.String(), argValue(t, entry.Args, "code"))
}

func TestStreamClientInterceptor_NonEOFRecvErrorLogsThatCode(t *testing.T) {
	log := kitlogtest.NewMockLogger()
	interceptor := StreamClientInterceptor(Options{Logger: log})

	fake := &recvErrClientStream{err: status.Error(codes.Canceled, "cancelled")}
	streamer := func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		return fake, nil
	}

	cs, err := interceptor(context.Background(), &grpc.StreamDesc{}, nil, "/test.Service/Stream", streamer)
	require.NoError(t, err)

	recvErr := cs.RecvMsg(new(any))
	assert.True(t, errors.Is(recvErr, fake.err) || status.Code(recvErr) == codes.Canceled)

	entry := lastEntry(t, log, "grpc_call")
	assert.Equal(t, codes.Canceled.String(), argValue(t, entry.Args, "code"))
	assert.Equal(t, slog.LevelWarn, entry.Level)
}

// TestStreamClientInterceptor_ClientStreamingSingleResponseLogsOnSuccess
// covers the exact gap the review flagged: a client-streaming RPC
// (ServerStreams: false) where the caller sends its messages, closes the
// send side, and receives exactly one successful response -- never calling
// RecvMsg again to observe an EOF. That single successful receive must be
// treated as terminal, or the call goes unlogged forever.
func TestStreamClientInterceptor_ClientStreamingSingleResponseLogsOnSuccess(t *testing.T) {
	log := kitlogtest.NewMockLogger()
	interceptor := StreamClientInterceptor(Options{Logger: log})

	fake := &fakeClientStream{recv: []any{"the one response"}}
	streamer := func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		return fake, nil
	}

	cs, err := interceptor(context.Background(), &grpc.StreamDesc{ServerStreams: false}, nil, "/test.Service/ClientStream", streamer)
	require.NoError(t, err)

	require.NoError(t, cs.SendMsg("a"))
	require.NoError(t, cs.SendMsg("b"))
	require.NoError(t, cs.CloseSend())
	require.NoError(t, cs.RecvMsg(new(any)))

	entry := lastEntry(t, log, "grpc_call")
	assert.Equal(t, codes.OK.String(), argValue(t, entry.Args, "code"))
	assert.Equal(t, int64(2), argValue(t, entry.Args, "msg_sent"))
	assert.Equal(t, int64(1), argValue(t, entry.Args, "msg_received"))
}

func TestStreamClientInterceptor_SendMsgErrorFinalizes(t *testing.T) {
	log := kitlogtest.NewMockLogger()
	interceptor := StreamClientInterceptor(Options{Logger: log})

	sendErr := status.Error(codes.Unavailable, "broken pipe")
	fake := &fakeClientStream{sendErr: sendErr}
	streamer := func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		return fake, nil
	}

	cs, err := interceptor(context.Background(), &grpc.StreamDesc{ServerStreams: true}, nil, "/test.Service/Stream", streamer)
	require.NoError(t, err)

	require.ErrorIs(t, cs.SendMsg("a"), sendErr)
	// A further RecvMsg after the stream already finalized must not add a
	// second log line.
	_ = cs.RecvMsg(new(any))

	entries := 0
	for _, e := range log.Entries {
		if e.Message == "grpc_call" {
			entries++
		}
	}
	assert.Equal(t, 1, entries)

	entry := lastEntry(t, log, "grpc_call")
	assert.Equal(t, codes.Unavailable.String(), argValue(t, entry.Args, "code"))
}

func TestStreamClientInterceptor_CloseSendErrorFinalizes(t *testing.T) {
	log := kitlogtest.NewMockLogger()
	interceptor := StreamClientInterceptor(Options{Logger: log})

	closeErr := status.Error(codes.Internal, "close failed")
	fake := &fakeClientStream{closeSendErr: closeErr}
	streamer := func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		return fake, nil
	}

	cs, err := interceptor(context.Background(), &grpc.StreamDesc{ServerStreams: false}, nil, "/test.Service/ClientStream", streamer)
	require.NoError(t, err)

	require.NoError(t, cs.SendMsg("a"))
	require.ErrorIs(t, cs.CloseSend(), closeErr)

	entry := lastEntry(t, log, "grpc_call")
	assert.Equal(t, codes.Internal.String(), argValue(t, entry.Args, "code"))
}

// TestStreamClientInterceptor_SuccessfulCloseSendAloneDoesNotFinalize proves
// a clean CloseSend on a stream still awaiting a response is not, by itself,
// grounds to log: the caller may still be waiting on data.
func TestStreamClientInterceptor_SuccessfulCloseSendAloneDoesNotFinalize(t *testing.T) {
	log := kitlogtest.NewMockLogger()
	interceptor := StreamClientInterceptor(Options{Logger: log})

	fake := &fakeClientStream{recv: []any{"resp"}}
	streamer := func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		return fake, nil
	}

	cs, err := interceptor(context.Background(), &grpc.StreamDesc{ServerStreams: true}, nil, "/test.Service/Bidi", streamer)
	require.NoError(t, err)

	require.NoError(t, cs.SendMsg("a"))
	require.NoError(t, cs.CloseSend())

	assert.False(t, log.HasMessage("grpc_call"), "a successful CloseSend alone must not finalize a still-open, server-streaming call")

	require.NoError(t, cs.RecvMsg(new(any)))
	require.ErrorIs(t, cs.RecvMsg(new(any)), io.EOF)
	assert.True(t, log.HasMessage("grpc_call"))
}

// TestStreamClientInterceptor_ContextCancellationFinalizes proves a caller
// that abandons a stream (stops calling RecvMsg entirely, e.g. after its
// context is cancelled) still gets exactly one "grpc_call" line, instead of
// none.
func TestStreamClientInterceptor_ContextCancellationFinalizes(t *testing.T) {
	log := kitlogtest.NewMockLogger()
	interceptor := StreamClientInterceptor(Options{Logger: log})

	fake := &blockingClientStream{block: make(chan struct{})}
	t.Cleanup(func() { close(fake.block) })
	streamer := func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		return fake, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	cs, err := interceptor(ctx, &grpc.StreamDesc{ServerStreams: true}, nil, "/test.Service/Bidi", streamer)
	require.NoError(t, err)
	_ = cs

	cancel()

	require.Eventually(t, func() bool {
		return log.HasMessage("grpc_call")
	}, time.Second, 10*time.Millisecond, "context cancellation must eventually finalize the stream's log line")

	entry := lastEntry(t, log, "grpc_call")
	assert.Equal(t, codes.Canceled.String(), argValue(t, entry.Args, "code"))
}

// TestStreamClientInterceptor_ExactlyOnceUnderConcurrentFinalizers races a
// RecvMsg error against context cancellation to prove the sync.Once guard
// holds even when two independent finalize triggers fire close together.
func TestStreamClientInterceptor_ExactlyOnceUnderConcurrentFinalizers(t *testing.T) {
	for i := 0; i < 20; i++ {
		log := kitlogtest.NewMockLogger()
		interceptor := StreamClientInterceptor(Options{Logger: log})

		fake := &recvErrClientStream{err: io.EOF}
		streamer := func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, opts ...grpc.CallOption) (grpc.ClientStream, error) {
			return fake, nil
		}

		ctx, cancel := context.WithCancel(context.Background())
		cs, err := interceptor(ctx, &grpc.StreamDesc{ServerStreams: true}, nil, "/test.Service/Bidi", streamer)
		require.NoError(t, err)

		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); cancel() }()
		go func() { defer wg.Done(); _ = cs.RecvMsg(new(any)) }()
		wg.Wait()

		require.Eventually(t, func() bool {
			return log.HasMessage("grpc_call")
		}, time.Second, 10*time.Millisecond)

		entries := 0
		for _, e := range log.Entries {
			if e.Message == "grpc_call" {
				entries++
			}
		}
		assert.Equal(t, 1, entries)
	}
}

type recvErrClientStream struct {
	fakeClientStream
	err error
}

func (f *recvErrClientStream) RecvMsg(m any) error { return f.err }

// --- Benchmarks -------------------------------------------------------

func BenchmarkInterceptorUnaryServer(b *testing.B) {
	log := kitlogtest.NewMockLogger()
	interceptor := UnaryServerInterceptor(Options{Logger: log})
	handler := func(ctx context.Context, req any) (any, error) { return "ok", nil }
	info := &grpc.UnaryServerInfo{FullMethod: "/bench.Service/Method"}
	ctx := context.Background()

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = interceptor(ctx, "req", info, handler)
	}
}
