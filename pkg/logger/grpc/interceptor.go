// Package grpc provides gRPC server and client interceptors that log one
// line per call.
//
// They observe; they never alter a request or its outcome. Payloads are
// never logged unless Options.LogPayloads is explicitly set, and a panic in
// a handler is converted to a codes.Internal status by default rather than
// left to crash the server -- grpc-go does not recover panics on its own,
// and an unrecovered panic in one RPC brings down every other call the
// process is serving, not just the failing one.
package grpc

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"

	kitlog "github.com/pablogore/kit-logger/pkg/logger"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

// DefaultMetadataKeys are the incoming metadata keys checked, in order, for a
// correlation ID. The first key present with a non-empty value wins.
var DefaultMetadataKeys = []string{"x-request-id", "x-correlation-id"}

// requestIDKey is the context key a server interceptor attaches the
// correlation ID under. It is an unexported struct type so no other package
// can collide with it or forge a value, matching httpmw's requestIDKey.
type requestIDKey struct{}

// RequestIDFrom returns the correlation ID a server interceptor in this
// package attached to ctx.
//
// The second result reports whether one was found, distinguishing "no
// interceptor in this chain" from "the metadata carried an empty value".
// A client interceptor in this package reads the same key, so a request ID
// picked up from an inbound call's metadata rides along on any outbound call
// the handler makes with the enriched context -- the end-to-end correlation
// httpmw provides for HTTP extends across the gRPC hop.
func RequestIDFrom(ctx context.Context) (string, bool) {
	if ctx == nil {
		return "", false
	}
	id, ok := ctx.Value(requestIDKey{}).(string)
	return id, ok
}

// Options configures the interceptors in this package. The zero value is
// usable: it logs to the process-wide logger, reads x-request-id and
// x-correlation-id, recovers panics, and maps status codes to levels with
// defaultLevelFor.
type Options struct {
	// Logger receives the call lines. When nil, the process-wide logger is
	// resolved once, at construction -- not per call.
	Logger kitlog.Logger

	// LevelFor maps a status code to a level. Nil means defaultLevelFor:
	// OK to Info, the client-error-shaped codes to Warn, everything else to
	// Error. A recovered panic is always logged at Error and does not
	// consult this function.
	LevelFor func(codes.Code) slog.Level

	// MetadataKeys are the incoming metadata keys checked for a correlation
	// ID, and the outgoing key a client interceptor writes one under. Empty
	// means DefaultMetadataKeys.
	MetadataKeys []string

	// DisablePanicRecovery stops a server interceptor from converting a
	// recovered panic into a codes.Internal status; the panic is logged and
	// then re-raised instead.
	//
	// Phrased as an opt-out because the useful behavior -- one failing RPC
	// does not take the process down -- is the default, and a bool field
	// documented as "defaults to true" is a field whose zero value lies
	// about itself.
	DisablePanicRecovery bool

	// SkipMethods are full method names (e.g.
	// "/grpc.health.v1.Health/Check") that produce no log line. The call is
	// still served.
	SkipMethods []string

	// LogPayloads adds the request and response values as log fields on the
	// unary interceptors. It defaults to false and must be opted into
	// explicitly: request and response messages routinely carry PII,
	// credentials or bodies too large for a log line, so this is a footgun
	// left off by default rather than a feature left on. It has no effect
	// on the streaming interceptors, which never log individual messages.
	LogPayloads bool
}

// defaultLevelFor maps a status code to a level: OK is informational, the
// codes that describe a caller/environment problem rather than a server
// defect are warnings, and everything else -- including Unknown, the code a
// non-status error maps to -- is an error.
func defaultLevelFor(c codes.Code) slog.Level {
	switch c {
	case codes.OK:
		return slog.LevelInfo
	case codes.Canceled, codes.DeadlineExceeded, codes.Unavailable, codes.ResourceExhausted,
		codes.InvalidArgument, codes.NotFound, codes.AlreadyExists, codes.PermissionDenied,
		codes.Unauthenticated, codes.FailedPrecondition, codes.OutOfRange, codes.Aborted:
		return slog.LevelWarn
	default: // Unknown, Internal, Unimplemented, DataLoss
		return slog.LevelError
	}
}

// logger resolves the logger for one line, falling back to the process-wide
// one only when no logger was injected.
func logger(log kitlog.Logger) kitlog.Logger {
	if log != nil {
		return log
	}
	return kitlog.L()
}

// resolved holds the options after defaults are applied, computed once per
// interceptor construction rather than once per call.
type resolved struct {
	levelFor func(codes.Code) slog.Level
	keys     []string
	skip     map[string]struct{}
}

func resolve(opts Options) resolved {
	levelFor := opts.LevelFor
	if levelFor == nil {
		levelFor = defaultLevelFor
	}

	keys := opts.MetadataKeys
	if len(keys) == 0 {
		keys = DefaultMetadataKeys
	}

	skip := make(map[string]struct{}, len(opts.SkipMethods))
	for _, m := range opts.SkipMethods {
		skip[m] = struct{}{}
	}

	return resolved{levelFor: levelFor, keys: keys, skip: skip}
}

func (r resolved) skipped(method string) bool {
	_, ok := r.skip[method]
	return ok
}

// extractRequestID reads the first configured metadata key present with a
// non-empty value on ctx's incoming metadata.
func extractRequestID(ctx context.Context, keys []string) (string, bool) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return "", false
	}
	for _, k := range keys {
		if vals := md.Get(k); len(vals) > 0 && vals[0] != "" {
			return vals[0], true
		}
	}
	return "", false
}

// peerAddr reports the address of the peer on the other end of ctx's call,
// when one is attached.
func peerAddr(ctx context.Context) (string, bool) {
	p, ok := peer.FromContext(ctx)
	if !ok || p.Addr == nil {
		return "", false
	}
	return p.Addr.String(), true
}

// deadlineMillis reports the time remaining until ctx's deadline, in
// milliseconds, when one is set. It can be negative for a context whose
// deadline has already passed by the time this runs.
func deadlineMillis(ctx context.Context) (int64, bool) {
	d, ok := ctx.Deadline()
	if !ok {
		return 0, false
	}
	return time.Until(d).Milliseconds(), true
}

// panicValue renders a recovered value for the log field, preferring an
// error's message over its %v form, matching httpmw.
func panicValue(rec any) string {
	if err, ok := rec.(error); ok {
		return err.Error()
	}
	return fmt.Sprint(rec)
}

// UnaryServerInterceptor returns a grpc.UnaryServerInterceptor that logs one
// "grpc_call" line per call.
func UnaryServerInterceptor(opts Options) grpc.UnaryServerInterceptor {
	r := resolve(opts)
	log := opts.Logger

	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp any, err error) {
		if r.skipped(info.FullMethod) {
			return handler(ctx, req)
		}

		start := time.Now()

		requestID, hasID := extractRequestID(ctx, r.keys)
		if hasID {
			ctx = context.WithValue(ctx, requestIDKey{}, requestID)
		}

		// The whole tail runs from a defer so a panicking handler produces
		// exactly the same line a completed one does, plus the panic
		// fields, and produces it exactly once -- matching httpmw.
		defer func() {
			fields := []any{
				"method", info.FullMethod,
				"duration_ms", time.Since(start).Milliseconds(),
			}
			if hasID {
				fields = append(fields, "request_id", requestID)
			}
			if addr, ok := peerAddr(ctx); ok {
				fields = append(fields, "peer", addr)
			}
			if ms, ok := deadlineMillis(ctx); ok {
				fields = append(fields, "deadline_ms", ms)
			}

			if rec := recover(); rec != nil {
				fields = append(fields,
					"code", codes.Internal.String(),
					"panic", panicValue(rec),
					"stack", string(debug.Stack()),
				)
				logger(log).Log(ctx, slog.LevelError, "grpc_call", fields...)

				if opts.DisablePanicRecovery {
					panic(rec)
				}
				resp, err = nil, status.Error(codes.Internal, "internal error")
				return
			}

			st := status.Convert(err)
			fields = append(fields, "code", st.Code().String())
			switch {
			case err != nil:
				fields = append(fields, "error", err.Error())
			case opts.LogPayloads:
				fields = append(fields, "request", req, "response", resp)
			}
			logger(log).Log(ctx, r.levelFor(st.Code()), "grpc_call", fields...)
		}()

		resp, err = handler(ctx, req)
		return resp, err
	}
}

// wrappedServerStream carries the enriched context to the handler and counts
// messages actually exchanged, analogous to httpmw's responseRecorder.
type wrappedServerStream struct {
	grpc.ServerStream
	ctx      context.Context
	recvMsgs atomic.Int64
	sentMsgs atomic.Int64
}

// Context returns the enriched context (correlation ID attached), not the
// embedded stream's original one -- otherwise a handler that reads
// RequestIDFrom(ss.Context()) would never see it.
func (w *wrappedServerStream) Context() context.Context { return w.ctx }

func (w *wrappedServerStream) RecvMsg(m any) error {
	err := w.ServerStream.RecvMsg(m)
	if err == nil {
		w.recvMsgs.Add(1)
	}
	return err
}

func (w *wrappedServerStream) SendMsg(m any) error {
	err := w.ServerStream.SendMsg(m)
	if err == nil {
		w.sentMsgs.Add(1)
	}
	return err
}

// StreamServerInterceptor returns a grpc.StreamServerInterceptor that logs
// one "grpc_call" line per stream, after it completes.
func StreamServerInterceptor(opts Options) grpc.StreamServerInterceptor {
	r := resolve(opts)
	log := opts.Logger

	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) (err error) {
		if r.skipped(info.FullMethod) {
			return handler(srv, ss)
		}

		start := time.Now()
		ctx := ss.Context()

		requestID, hasID := extractRequestID(ctx, r.keys)
		if hasID {
			ctx = context.WithValue(ctx, requestIDKey{}, requestID)
		}

		wrapped := &wrappedServerStream{ServerStream: ss, ctx: ctx}

		defer func() {
			fields := []any{
				"method", info.FullMethod,
				"duration_ms", time.Since(start).Milliseconds(),
				"msg_received", wrapped.recvMsgs.Load(),
				"msg_sent", wrapped.sentMsgs.Load(),
			}
			if hasID {
				fields = append(fields, "request_id", requestID)
			}
			if addr, ok := peerAddr(ctx); ok {
				fields = append(fields, "peer", addr)
			}
			if ms, ok := deadlineMillis(ctx); ok {
				fields = append(fields, "deadline_ms", ms)
			}

			if rec := recover(); rec != nil {
				fields = append(fields,
					"code", codes.Internal.String(),
					"panic", panicValue(rec),
					"stack", string(debug.Stack()),
				)
				logger(log).Log(ctx, slog.LevelError, "grpc_call", fields...)

				if opts.DisablePanicRecovery {
					panic(rec)
				}
				err = status.Error(codes.Internal, "internal error")
				return
			}

			st := status.Convert(err)
			fields = append(fields, "code", st.Code().String())
			if err != nil {
				fields = append(fields, "error", err.Error())
			}
			logger(log).Log(ctx, r.levelFor(st.Code()), "grpc_call", fields...)
		}()

		err = handler(srv, wrapped)
		return err
	}
}

// UnaryClientInterceptor returns a grpc.UnaryClientInterceptor that logs one
// "grpc_call" line per outbound call.
func UnaryClientInterceptor(opts Options) grpc.UnaryClientInterceptor {
	r := resolve(opts)
	log := opts.Logger

	return func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, callOpts ...grpc.CallOption) error {
		if r.skipped(method) {
			return invoker(ctx, method, req, reply, cc, callOpts...)
		}

		start := time.Now()

		requestID, hasID := RequestIDFrom(ctx)
		if hasID {
			ctx = metadata.AppendToOutgoingContext(ctx, r.keys[0], requestID)
		}

		err := invoker(ctx, method, req, reply, cc, callOpts...)

		st := status.Convert(err)
		fields := []any{
			"method", method,
			"duration_ms", time.Since(start).Milliseconds(),
			"code", st.Code().String(),
		}
		if hasID {
			fields = append(fields, "request_id", requestID)
		}
		if ms, ok := deadlineMillis(ctx); ok {
			fields = append(fields, "deadline_ms", ms)
		}
		switch {
		case err != nil:
			fields = append(fields, "error", err.Error())
		case opts.LogPayloads:
			fields = append(fields, "request", req, "response", reply)
		}
		logger(log).Log(ctx, r.levelFor(st.Code()), "grpc_call", fields...)
		return err
	}
}

// wrappedClientStream counts messages the caller actually sends and receives
// and logs the summary line exactly once, the first time RecvMsg reports the
// stream is over (io.EOF on a clean end, any other error otherwise).
//
// A client stream has no explicit "close" callback to hook, unlike a server
// stream whose handler returns -- RecvMsg reporting a terminal error is the
// only reliable end-of-stream signal available on this side.
type wrappedClientStream struct {
	grpc.ClientStream
	once     sync.Once
	logEnd   func(err error)
	recvMsgs atomic.Int64
	sentMsgs atomic.Int64
}

func (w *wrappedClientStream) SendMsg(m any) error {
	err := w.ClientStream.SendMsg(m)
	if err == nil {
		w.sentMsgs.Add(1)
	}
	return err
}

func (w *wrappedClientStream) RecvMsg(m any) error {
	err := w.ClientStream.RecvMsg(m)
	if err == nil {
		w.recvMsgs.Add(1)
		return nil
	}
	w.once.Do(func() { w.logEnd(err) })
	return err
}

// StreamClientInterceptor returns a grpc.StreamClientInterceptor that logs
// one "grpc_call" line per stream, after it completes.
func StreamClientInterceptor(opts Options) grpc.StreamClientInterceptor {
	r := resolve(opts)
	log := opts.Logger

	return func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, callOpts ...grpc.CallOption) (grpc.ClientStream, error) {
		if r.skipped(method) {
			return streamer(ctx, desc, cc, method, callOpts...)
		}

		start := time.Now()

		requestID, hasID := RequestIDFrom(ctx)
		if hasID {
			ctx = metadata.AppendToOutgoingContext(ctx, r.keys[0], requestID)
		}

		cs, err := streamer(ctx, desc, cc, method, callOpts...)
		if err != nil {
			// The stream was never established: there is nothing to wrap
			// and nothing further will call RecvMsg, so this is logged
			// immediately instead of deferred to a message that will never
			// come.
			st := status.Convert(err)
			fields := []any{
				"method", method,
				"duration_ms", time.Since(start).Milliseconds(),
				"code", st.Code().String(),
				"error", err.Error(),
			}
			if hasID {
				fields = append(fields, "request_id", requestID)
			}
			logger(log).Log(ctx, r.levelFor(st.Code()), "grpc_call", fields...)
			return cs, err
		}

		wrapped := &wrappedClientStream{ClientStream: cs}
		wrapped.logEnd = func(streamErr error) {
			code := codes.OK
			errMsg := ""
			if streamErr != nil && streamErr != io.EOF {
				st := status.Convert(streamErr)
				code = st.Code()
				errMsg = streamErr.Error()
			}

			fields := []any{
				"method", method,
				"duration_ms", time.Since(start).Milliseconds(),
				"code", code.String(),
				"msg_received", wrapped.recvMsgs.Load(),
				"msg_sent", wrapped.sentMsgs.Load(),
			}
			if hasID {
				fields = append(fields, "request_id", requestID)
			}
			if errMsg != "" {
				fields = append(fields, "error", errMsg)
			}
			logger(log).Log(ctx, r.levelFor(code), "grpc_call", fields...)
		}
		return wrapped, nil
	}
}

// UnaryLoggingInterceptor generates an interceptor that logs each gRPC call.
//
// Deprecated: use UnaryServerInterceptor, which takes an explicit Options
// instead of always resolving the process-wide logger. This is
// UnaryServerInterceptor(Options{}) and remains for source compatibility;
// existing callers gain panic recovery and status-derived levels along with
// it, both strict improvements over a call that used to log every outcome
// at Info and never recovered a panic at all.
func UnaryLoggingInterceptor() grpc.UnaryServerInterceptor {
	return UnaryServerInterceptor(Options{})
}
