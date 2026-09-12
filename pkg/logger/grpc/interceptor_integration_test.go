package grpc

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"testing"
	"time"

	"github.com/pablogore/kit-logger/pkg/logger/kitlogtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/encoding"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

// jsonCodec lets this test exercise a real grpc.Server/ClientConn without a
// protoc-generated service: it registers under the "proto" name, which is
// the content-subtype grpc.NewServer and grpc.NewClient use when none is
// requested, so no .proto toolchain is needed to prove the interceptors
// behave correctly over an actual transport.
type jsonCodec struct{}

func (jsonCodec) Marshal(v any) ([]byte, error)      { return json.Marshal(v) }
func (jsonCodec) Unmarshal(data []byte, v any) error { return json.Unmarshal(data, v) }
func (jsonCodec) Name() string                       { return "proto" }

func init() {
	encoding.RegisterCodec(jsonCodec{})
}

type echoMsg struct {
	Value string
	Panic bool
}

// echoServer implements every RPC shape by hand against the ServiceDesc
// below: unary echo, a server stream that emits a fixed count of messages,
// a client stream that counts what it received, and a bidi echo. Unary and
// server-stream additionally honor Panic to exercise panic recovery over the
// wire.
type echoServer struct{}

func (echoServer) Unary(ctx context.Context, in *echoMsg) (*echoMsg, error) {
	if in.Panic {
		panic("integration test panic")
	}
	if in.Value == "fail" {
		return nil, status.Error(codes.InvalidArgument, "bad value")
	}
	return &echoMsg{Value: in.Value}, nil
}

func (echoServer) ServerStream(in *echoMsg, stream grpc.ServerStream) error {
	if in.Panic {
		panic("integration test stream panic")
	}
	for i := 0; i < 3; i++ {
		if err := stream.SendMsg(&echoMsg{Value: in.Value}); err != nil {
			return err
		}
	}
	return nil
}

func (echoServer) ClientStream(stream grpc.ServerStream) error {
	for {
		m := new(echoMsg)
		err := stream.RecvMsg(m)
		if err == io.EOF {
			return stream.SendMsg(&echoMsg{Value: "received"})
		}
		if err != nil {
			return err
		}
	}
}

func (echoServer) Bidi(stream grpc.ServerStream) error {
	for {
		m := new(echoMsg)
		err := stream.RecvMsg(m)
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if err := stream.SendMsg(m); err != nil {
			return err
		}
	}
}

func unaryHandler(srv any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
	in := new(echoMsg)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(echoServer).Unary(ctx, in)
	}
	info := &grpc.UnaryServerInfo{Server: srv, FullMethod: "/test.Echo/Unary"}
	handler := func(ctx context.Context, req any) (any, error) {
		return srv.(echoServer).Unary(ctx, req.(*echoMsg))
	}
	return interceptor(ctx, in, info, handler)
}

func serverStreamHandler(srv any, stream grpc.ServerStream) error {
	in := new(echoMsg)
	if err := stream.RecvMsg(in); err != nil {
		return err
	}
	return srv.(echoServer).ServerStream(in, stream)
}

func clientStreamHandler(srv any, stream grpc.ServerStream) error {
	return srv.(echoServer).ClientStream(stream)
}

func bidiHandler(srv any, stream grpc.ServerStream) error {
	return srv.(echoServer).Bidi(stream)
}

var echoServiceDesc = grpc.ServiceDesc{
	ServiceName: "test.Echo",
	HandlerType: (*any)(nil),
	Methods: []grpc.MethodDesc{
		{MethodName: "Unary", Handler: unaryHandler},
	},
	Streams: []grpc.StreamDesc{
		{StreamName: "ServerStream", Handler: serverStreamHandler, ServerStreams: true},
		{StreamName: "ClientStream", Handler: clientStreamHandler, ClientStreams: true},
		{StreamName: "Bidi", Handler: bidiHandler, ServerStreams: true, ClientStreams: true},
	},
}

// integrationHarness wires echoServer up behind this package's interceptors
// over a bufconn listener, and tears everything down on test cleanup.
type integrationHarness struct {
	conn *grpc.ClientConn
	log  *kitlogtest.MockLogger
}

func newIntegrationHarness(t *testing.T) *integrationHarness {
	t.Helper()

	log := kitlogtest.NewMockLogger()
	opts := Options{Logger: log}

	lis := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer(
		grpc.UnaryInterceptor(UnaryServerInterceptor(opts)),
		grpc.StreamInterceptor(StreamServerInterceptor(opts)),
	)
	server.RegisterService(&echoServiceDesc, echoServer{})

	go func() {
		_ = server.Serve(lis)
	}()
	t.Cleanup(server.Stop)

	dialer := func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }
	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(dialer),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithChainUnaryInterceptor(UnaryClientInterceptor(opts)),
		grpc.WithChainStreamInterceptor(StreamClientInterceptor(opts)),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	return &integrationHarness{conn: conn, log: log}
}

func (h *integrationHarness) invokeUnary(ctx context.Context, in, out *echoMsg) error {
	return h.conn.Invoke(ctx, "/test.Echo/Unary", in, out)
}

func (h *integrationHarness) newStream(ctx context.Context, desc *grpc.StreamDesc, method string) (grpc.ClientStream, error) {
	return h.conn.NewStream(ctx, desc, method)
}

func TestIntegration_Unary_SuccessAndFailure(t *testing.T) {
	h := newIntegrationHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	md := metadata.New(map[string]string{"x-request-id": "int-req-1"})
	ctx = metadata.NewOutgoingContext(ctx, md)

	var out echoMsg
	require.NoError(t, h.invokeUnary(ctx, &echoMsg{Value: "hello"}, &out))
	assert.Equal(t, "hello", out.Value)

	entry := serverEntry(t, h.log, "grpc_call")
	assert.Equal(t, codes.OK.String(), argValue(t, entry.Args, "code"))
	assert.Equal(t, "int-req-1", argValue(t, entry.Args, "request_id"))

	err := h.invokeUnary(ctx, &echoMsg{Value: "fail"}, &out)
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.InvalidArgument, st.Code())

	entry = serverEntry(t, h.log, "grpc_call")
	assert.Equal(t, codes.InvalidArgument.String(), argValue(t, entry.Args, "code"))
}

func TestIntegration_UnaryPanic_RecoveredAsInternal(t *testing.T) {
	h := newIntegrationHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var out echoMsg
	err := h.invokeUnary(ctx, &echoMsg{Panic: true}, &out)
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Internal, st.Code())

	// One line from the server interceptor recording the panic, and the
	// call is not left unlogged from the client's side either.
	found := false
	for _, e := range h.log.Entries {
		if e.Message == "grpc_call" && hasArg(e.Args, "panic") {
			found = true
		}
	}
	assert.True(t, found, "the panicking call must produce a record with a panic field")
}

func TestIntegration_ServerStream_EmitsFixedCount(t *testing.T) {
	h := newIntegrationHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	desc := &echoServiceDesc.Streams[0]
	stream, err := h.newStream(ctx, desc, "/test.Echo/ServerStream")
	require.NoError(t, err)

	require.NoError(t, stream.SendMsg(&echoMsg{Value: "hi"}))
	require.NoError(t, stream.CloseSend())

	count := 0
	for {
		m := new(echoMsg)
		err := stream.RecvMsg(m)
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		count++
	}
	assert.Equal(t, 3, count)

	require.Eventually(t, func() bool {
		return h.log.HasMessage("grpc_call")
	}, time.Second, 10*time.Millisecond)

	entry := serverEntry(t, h.log, "grpc_call")
	assert.Equal(t, int64(3), argValue(t, entry.Args, "msg_sent"))
}

func TestIntegration_ClientStream_CountsReceived(t *testing.T) {
	h := newIntegrationHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	desc := &echoServiceDesc.Streams[1]
	stream, err := h.newStream(ctx, desc, "/test.Echo/ClientStream")
	require.NoError(t, err)

	for i := 0; i < 3; i++ {
		require.NoError(t, stream.SendMsg(&echoMsg{Value: "x"}))
	}
	require.NoError(t, stream.CloseSend())

	out := new(echoMsg)
	require.NoError(t, stream.RecvMsg(out))
	assert.Equal(t, "received", out.Value)

	require.Eventually(t, func() bool {
		return h.log.HasMessage("grpc_call")
	}, time.Second, 10*time.Millisecond)

	entry := serverEntry(t, h.log, "grpc_call")
	assert.Equal(t, int64(3), argValue(t, entry.Args, "msg_received"))

	// The client never calls RecvMsg a second time to observe an EOF on this
	// client-streaming RPC -- the single successful receive above must have
	// been enough, on its own, to finalize and log the client-side line too.
	clientLine := clientEntry(t, h.log, "grpc_call")
	assert.Equal(t, codes.OK.String(), argValue(t, clientLine.Args, "code"))
	assert.Equal(t, int64(3), argValue(t, clientLine.Args, "msg_sent"))
	assert.Equal(t, int64(1), argValue(t, clientLine.Args, "msg_received"))
}

func TestIntegration_Bidi_ConcurrentSendRecv(t *testing.T) {
	h := newIntegrationHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	desc := &echoServiceDesc.Streams[2]
	stream, err := h.newStream(ctx, desc, "/test.Echo/Bidi")
	require.NoError(t, err)

	const n = 5
	done := make(chan error, 1)
	go func() {
		for i := 0; i < n; i++ {
			if err := stream.SendMsg(&echoMsg{Value: "x"}); err != nil {
				done <- err
				return
			}
		}
		done <- stream.CloseSend()
	}()

	received := 0
	for received < n {
		m := new(echoMsg)
		require.NoError(t, stream.RecvMsg(m))
		received++
	}
	require.NoError(t, <-done)

	m := new(echoMsg)
	err = stream.RecvMsg(m)
	assert.ErrorIs(t, err, io.EOF)

	require.Eventually(t, func() bool {
		return h.log.HasMessage("grpc_call")
	}, time.Second, 10*time.Millisecond)
}
