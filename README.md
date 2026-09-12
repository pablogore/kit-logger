# kit-logger

An extensible structured logging framework for Go, built on top of `log/slog`, designed for observable, secure, and efficient microservices.

## Features

- Full support for `log/slog`
- Rule-based filtering that applies to record attrs, `With`/`WithAttrs`, and grouped attrs alike, in either drop-the-record or redact-the-field mode (see [Filtering](#filtering))
- Opt-in Prometheus metrics
- Sampling and rate limiting
- Context-aware and extensible with hooks
- Opt-in OpenTelemetry trace correlation (`trace_id` / `span_id`)
- Asynchronous buffering with safe shutdown
- One key per line: `With`, context fields and call-site attrs never duplicate a key, global fields are pinned first (see [Readable output](#readable-output))
- Opt-in call-site attribution under `source`, and a human-first console format for development
- Pluggable HTTP and gRPC interceptors, with a request-ID-propagating HTTP middleware

## Basic Usage

```go
import "github.com/pablogore/kit-logger/pkg/logger"

func main() {
    log := logger.New(logger.Config{
        Level:  logger.LevelInfo,
        Format: logger.FormatJSON,
        GlobalFields: map[string]string{
            "service": "my-api",
            "env":     "prod",
        },
    })

    logger.SetGlobal(log)
    log.Info("starting service")
}
```

## Destination

By default the logger writes to stdout. Set `Config.Writer` to send output anywhere else — `os.Stderr`, a file, an `io.Writer` used in tests, or a third-party writer such as [`lumberjack`](https://github.com/natefinch/lumberjack) for rotating files:

```go
log := logger.New(logger.Config{
    Level:  logger.LevelInfo,
    Format: logger.FormatJSON,
    Writer: os.Stderr, // keeps stdout free for a CLI's own machine-readable output
})
```

`Writer` composes with the rest of `Config` (`GlobalFields`, `FilterRules`, `Sampling`, `BufferSize`, `Hook`, `ContextFields`) — it only changes where the pipeline's output lands.

**`Config.Sink` replaces `Writer`, not the rest of the pipeline.** Supplying a `slog.Handler` of your own as `Config.Sink` still gets the dedup stage, `FilterRules`, `GlobalFields`, `AddSource`, `Sampling`, `MetricsHandler`, `BufferSize`, `Hook` and `SetLevel` applied on top of it, exactly like the built-in Text/JSON handler does. Because the dedup stage materializes `With` attributes into the record, a `Sink` never receives `WithAttrs` or `WithGroup` calls: every attribute arrives on the record itself, nested with `slog.GroupValue` where a group was open. `Config.Handler` is a deprecated alias for `Sink` (`Sink` wins if both are set) — it used to bypass the whole pipeline, but no longer does.

If you need the old total-bypass behavior — a hand-built chain that must not be redecorated — use `Config.PipelineOverride` instead. It bypasses the handler-decoration pipeline only: `Sink`/`Handler`, `FilterRules`, `GlobalFields`, `Sampling`, `MetricsHandler`, `BufferSize`, `Hook`, `Writer`, `Format` and `Level` are all ignored, and `Config.Validate()` (also reachable through `NewWithError`) reports an error naming each one that was set. Logger-level behavior outside that chain is unaffected — `RateLimit`, `ContextFields` and the outer `ContextHandler` still apply.

## Readable output

A log line is read far more often than it is written, so the pipeline is
shaped around what a reader scans for, following the conventions `log/slog`,
OpenTelemetry and the usual log shippers agree on:

```json
{"time":"...","level":"INFO","msg":"order created","env":"prod","service":"orders","request_id":"req-123","order_id":"ord-1","took":"1.5s"}
```

- **Every key appears once per line.** `slog` deliberately writes duplicate
  keys verbatim, and the ergonomic paths produce them: a logger derived with
  `With("request_id", id)`, a `ContextFields` extractor that adds `request_id`
  again, and a call site that logs it a third time. The innermost stage of the
  pipeline (`handler.DedupHandler`) resolves them to one key with the value of
  the last occurrence and the position of the first, so a JSON shipper never
  has to guess which value wins. Groups with the same key merge member by
  member.
- **Global fields come first and are pinned.** `GlobalFields` describe the
  process (`service`, `env`, `version`); they are emitted right after `msg`, in
  sorted key order, and nothing — not `With`, not a context extractor, not the
  call site — overrides them.
- **`time`, `level` and `msg` belong to the handler.** A call-site attribute
  with one of those keys is renamed to `attr.<key>` instead of shadowing the
  built-in.
- **Call-site attribution is opt-in and lives under `source`.** Set
  `Config.AddSource` to get a `source` group (`function`, `file`, `line`) in the
  same shape `slog.HandlerOptions.AddSource` uses. It is off by default: three
  fields on every production line is noise. `component` is yours for naming a
  subsystem; the pipeline never writes it.
- **Durations are readable in every format.** A `time.Duration` renders as
  `"1.5s"` in JSON as well as text, instead of a nanosecond integer. Values
  whose key carries the unit (`duration_ms`) stay numeric.
- **Messages are stable event names.** The HTTP middleware logs `http_request`
  and the gRPC interceptors `grpc_call`; everything variable goes in fields.
  Do the same in your own code: `log.Info("order created", "order_id", id)`,
  not `log.Info("order " + id + " created")`.

For development, `Format: logger.FormatConsole` writes one line per record with
a short local time, a colored level, the message, then `key=value` pairs, with
`source` rendered as `file.go:42` at the end:

```
15:04:05.123 INF order created service=orders request_id=req-123 order_id=ord-1
15:04:05.124 ERR payment failed service=orders error="card declined" source=orders.go:87
```

Colors are on when the writer is a terminal and off for a pipe or a file;
`handler.ConsoleOptions` has `NoColor` and `ForceColor` for the cases in
between. Ship `FormatJSON` in production.

## Global fields

`Config.GlobalFields` attaches a fixed set of fields to every record, first
after `msg` and in sorted key order, so two replicas built from the same config
emit the same line shape. They are pinned: when `With`, a context extractor or
the call site uses one of those keys, the global value wins and the line never
contains the key twice.

```go
log := logger.New(logger.Config{
    Format:       logger.FormatJSON,
    GlobalFields: map[string]string{"env": "prod", "service": "checkout"},
})

log.Info("config reloaded", "env", "canary")
// {"time":"...","level":"INFO","msg":"config reloaded","env":"prod","service":"checkout"}
```

Global fields always stay at the top level. Record attributes, and anything
added with `WithAttrs` after a `WithGroup`, nest under the open groups exactly
as they would with a bare `slog` handler. For a hand-built chain,
`handler.GlobalFieldsHandler` offers the same replacement rule as a standalone
decorator:

```go
h := handler.NewGlobalFieldsHandler(slog.NewJSONHandler(os.Stdout, nil),
    map[string]string{"env": "prod"}, true)

slog.New(h).WithGroup("request").Info("handled", "method", "GET")
// {"time":"...","level":"INFO","msg":"handled","env":"prod","request":{"method":"GET"}}
```

The standalone handler has one limit the pipeline does not: attributes
attached with `WithAttrs` before any `WithGroup` are handed to the sink to
pre-format, as `slog` intends, so they are not visible to its replacement
check. Inside `logger.New` the dedup stage sees everything, and global fields
win over `With` attributes too.

## Filtering

`Config.FilterRules` matches a key (case-insensitive) and, optionally, an
exact value. A rule matches an attribute wherever it comes from — a direct
log call, `logger.With(...)`/`WithAttrs`, or nested inside a `slog.Group` —
so attaching a field through `With` is no safer than passing it inline.

`Config.FilterMode` selects what a match does:

- `handler.ModeDrop` (the default) discards the whole record, including every
  other field on it. Use this for suppressing noisy or unwanted log lines.
- `handler.ModeRedact` keeps the record and replaces only the matching
  value — with `FilterRule.Replacement`, or `"[REDACTED]"` if unset — leaving
  every other field and the record's time, level and message untouched.
  Prefer this for anything resembling a security control: dropping an
  ERROR-level line because it happened to mention a token loses the
  incident, not just the token.

A rule's `Key` matches an attribute's bare key at any nesting depth, or a
dotted path (e.g. `"credential.password"`) to match only that nested field.

## Sampling

`Config.Sampling.Probability` treats `0` as **unset**, not as "drop
everything" — an unset probability is treated as `1` and every record is
emitted. To actually drop most records, set an explicit probability below `1`
(e.g. `0.1` samples roughly 10%).

## Lifecycle

Buffered logging is asynchronous, so a process that exits without draining loses
its in-flight tail — exactly the window where the interesting records live. The
lifecycle is therefore host-owned and explicit.

`Logger` is unchanged. Type-assert to `ManagedLogger` to reach it:

```go
log := logger.New(logger.Config{BufferSize: 4096, Format: logger.FormatJSON})
logger.SetGlobal(log)

if managed, ok := log.(logger.ManagedLogger); ok {
    defer func() {
        ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
        defer cancel()
        _ = managed.Shutdown(ctx)
    }()
}
```

- `Flush(ctx)` returns once every record accepted before the call has been
  delivered downstream, or `ctx` expires — in which case it returns `ctx.Err()`.
  It does not stop the logger.
- `Shutdown(ctx)` stops accepting records, delivers what it already accepted and
  releases the worker. It is idempotent and safe to call concurrently: the
  shutdown is started once and shared, and `ctx` bounds how long *that call*
  waits for it — not how long the drain is allowed to take. A caller with a
  tight deadline gets its own `ctx.Err()` and cannot cut short a drain another
  caller was willing to wait for; every caller that waits to the end sees the
  same result.
- Admission closes the moment `Shutdown` starts, not when it finishes. Records
  submitted from that point on are discarded before they consume a rate-limit
  token or fire a counter; they never panic and never block, and they are
  counted by `(*SlogLogger).Rejected()`.
- `Sync()` is `Flush(context.Background())`. It stays on the `Logger` interface
  for source compatibility and is deprecated in favour of the context-aware
  methods.
- `ExitWithFlush(code)` shuts the global logger down, bounded by
  `DefaultShutdownTimeout`, before terminating the process.

The lifecycle is captured when `New` assembles the pipeline, so it reaches the
buffer regardless of how many decorators wrap it and regardless of whether the
logger was derived through `With`. A chain assembled by hand and passed as
`Config.Sink` or `Config.PipelineOverride` is discovered through the `Unwrap` /
`UnwrapAll` methods the built-in handlers implement.

## Globals and context fields

Prefer owning a logger and passing it where it is needed. The package-level
helpers remain for consumers that cannot do that yet, and they are now
race-free: `L()` and `SetGlobal()` publish through an `atomic.Pointer`, and the
lazy default is constructed exactly once no matter how many goroutines call
`L()` first.

```go
log := logger.New(logger.Config{Level: logger.LevelInfo, Format: logger.FormatJSON})
logger.SetGlobal(log)          // safe concurrently with L(), even under load
```

- `SetGlobal(l)` **no longer calls `slog.SetDefault`.** Rewiring the standard
  library for the whole process is not something this library should do as a
  side effect of setting its own global. Use `SetGlobalAndSlogDefault(l)` when
  installing the `slog` default is what you actually want.
- `SetGlobal(nil)` panics. Storing a nil logger would turn every later `L()`
  into a nil dereference far away from the mistake.
- `L()` constructs a default logger on first use, exactly once.

Context fields belong on the logger, not on the package:

```go
log := logger.New(logger.Config{
    ContextFields: func(ctx context.Context) []any {
        return []any{"request_id", requestIDFrom(ctx)}
    },
})

log.WithContext(ctx).Info("handling request")   // carries request_id
```

A logger configured this way reads its own immutable field, so it touches no
package-level state at all and is unaffected by another part of the process
calling `SetContextFieldExtractor`. That function still works as a process-wide
fallback for loggers without their own extractor, and is now deprecated.

The extractor also runs on every `*Context` log method, not only `WithContext`:

```go
log.InfoContext(ctx, "handling request")   // carries request_id too
```

That used to be the surprising half of the API. `WithContext(ctx).Info(...)`
carried the configured fields and `InfoContext(ctx, ...)` — the call everybody
reaches for — silently carried none of them.

Pick one style per call site: `WithContext` for a derived logger reused across
several calls, the `*Context` methods for a single call. Doing both is
harmless — the extractor runs twice, but the line still carries each key
once (see [Readable output](#readable-output)).

## Trace correlation (OpenTelemetry)

Logs and traces are only useful together. `pkg/logger/otel` adds `trace_id` and
`span_id` to every record whose context carries a span, so a slow span in the
tracing backend becomes a one-field log query instead of a timestamp hunt across
40,000 concurrent lines.

```go
import kitotel "github.com/pablogore/kit-logger/pkg/logger/otel"

log := logger.New(logger.Config{
    Level:          logger.LevelInfo,
    Format:         logger.FormatJSON,
    ContextHandler: kitotel.Decorator(kitotel.Options{}),
})

log.ErrorContext(ctx, "stock check failed")
// {"level":"ERROR", ..., "trace_id":"4bf92f...4736", "span_id":"00f067aa0ba902b7"}
```

**The dependency is opt-in.** `pkg/logger` and `pkg/logger/handler` do not
import OpenTelemetry, and never will:

```bash
go list -deps ./pkg/logger ./pkg/logger/handler | rg opentelemetry   # empty
go list -deps ./pkg/logger/otel | rg 'otel/sdk'                      # empty
```

The subpackage depends on `go.opentelemetry.io/otel/trace` — the API module —
and nothing else. Reading a span context needs no SDK, no exporter and no
tracer provider.

What it deliberately does *not* do: create, start, end or sample spans; export
log records over OTLP; parse `traceparent` headers (that is the propagator's
job). It reads state that is already there.

- Records with **no span carry no correlation fields at all** — not empty ones —
  and the no-span path allocates nothing.
- `Config.ContextHandler` installs the handler as the **outermost** decorator,
  above the buffer. A handler that reads the caller's context has to run on the
  caller's goroutine.
- `Options` covers the rest: `TraceIDKey`/`SpanIDKey` to rename the fields,
  `Group` to nest them, `TraceFlags` to also emit the sampling flags, and
  `OnlySampled` to skip unsampled spans. `OnlySampled` is **off** by default: an
  unsampled span still has a valid trace ID, and correlating a request whose
  trace was dropped is often all you have left.

`ContextHandler` is a plain `func(slog.Handler) slog.Handler`, so the same seam
takes any context-reading handler, not only this one.

## Tests

Every Go snippet in this README is backed by a compiled `Example*` function —
`pkg/logger/example_test.go` for the core `Config` sections, and an
`example_test.go` in `grpc/`, `httpmw/`, `otel/` and `prometheus/` for theirs.
`go vet ./...` and `make examples` fail the moment a snippet drifts from the
real API, which is what keeps this file honest.

The project enforces a **minimum 85% test coverage threshold**, checked by
`./scripts/coverage-complete-report.sh`. Run `make coverage-threshold` (or
`make ci-threshold` for the full CI pipeline) to see the current numbers —
they aren't reproduced here since they drift with every change and a stale
number in this file would be actively misleading.

```bash
# Generate complete coverage report with threshold validation
make coverage-threshold

# Run CI pipeline with coverage validation
make ci-threshold

# Generate detailed coverage analysis
./scripts/coverage-complete-report.sh
```

The coverage system generates multiple report formats:
- **Main Report**: `coverage/coverage_report.txt` - Overall summary with threshold validation
- **Package Report**: `coverage/coverage_packages.txt` - Detailed analysis by package
- **File Report**: `coverage/coverage_files.txt` - Detailed analysis by file
- **HTML Report**: `coverage/coverage_report.html` - Interactive HTML coverage report

## Prometheus Metrics

Metrics are opt-in. Importing `pkg/logger` (or `pkg/logger/handler`) never
registers anything, anywhere -- a plain `logger.New(logger.Config{})` counts
nothing and touches no registry. To wire instrumentation, import
`pkg/logger/prometheus` (the only package in this module that depends on
`client_golang`) and set `Config.MetricsHandler`:

```go
import (
    "github.com/pablogore/kit-logger/pkg/logger"
    kitprom "github.com/pablogore/kit-logger/pkg/logger/prometheus"
    "github.com/prometheus/client_golang/prometheus"
)

log := logger.New(logger.Config{
    MetricsHandler: func(next slog.Handler) (slog.Handler, error) {
        return kitprom.New(next, prometheus.DefaultRegisterer, kitprom.Options{})
    },
})
```

This registers and exposes:
```
kitlogger_records_total{level="INFO"} 123
```

`kitprom.New` takes the `prometheus.Registerer` explicitly -- pass your own
registry instead of `prometheus.DefaultRegisterer` for a custom or per-tenant
setup, or `nil` to count without registering anywhere. A registration
collision with an existing, differently-typed collector returns a
descriptive error instead of panicking; `kitprom.MustNew` panics on that
error for callers that want to fail fast at startup.

**Migrating from the old always-on metric:** `handler.NewPrometheusHandler`
and the package-level `handler.LogCounter` it used to maintain are
deprecated -- the former is now a no-op that returns its argument unchanged,
and the latter is removed. Any dashboard built on `slog_logged_total` needs
the consumer to opt in via `Config.MetricsHandler` as shown above; the new
metric is named `kitlogger_records_total`, not `slog_logged_total`.

## HTTP & gRPC Integration

```go
import (
    kitgrpc "github.com/pablogore/kit-logger/pkg/logger/grpc"
    "github.com/pablogore/kit-logger/pkg/logger/httpmw"
    "google.golang.org/grpc"
    "google.golang.org/grpc/credentials/insecure"
)

grpcServer := grpc.NewServer(
    grpc.UnaryInterceptor(kitgrpc.UnaryServerInterceptor(kitgrpc.Options{Logger: log})),
    grpc.StreamInterceptor(kitgrpc.StreamServerInterceptor(kitgrpc.Options{Logger: log})),
)

var handler http.Handler = httpmw.New(httpmw.Options{Logger: log})(next)
```

### HTTP middleware

`httpmw.New` logs one line per request and is careful to observe without
altering. `httpmw.Middleware()` is `New(Options{})` and stays for source
compatibility; it uses the process-wide logger.

```go
mw := httpmw.New(httpmw.Options{
    Logger:    log,                  // nil falls back to logger.L()
    SkipPaths: []string{"/healthz"}, // served and un-logged
})
```

### gRPC interceptors

`kitgrpc.UnaryServerInterceptor`, `StreamServerInterceptor`,
`UnaryClientInterceptor` and `StreamClientInterceptor` each log one
`grpc_call` line per call, mapping the status code to a level instead of
always logging at Info: `OK` is Info, the codes that describe a caller or
environment problem (`NotFound`, `InvalidArgument`, `Unavailable`, ...) are
Warn, everything else — including a non-status error, and a recovered panic
— is Error.

```go
opts := kitgrpc.Options{
    Logger:      log,                                 // nil falls back to logger.L()
    SkipMethods: []string{"/grpc.health.v1.Health/Check"},
}

grpcServer := grpc.NewServer(
    grpc.UnaryInterceptor(kitgrpc.UnaryServerInterceptor(opts)),
    grpc.StreamInterceptor(kitgrpc.StreamServerInterceptor(opts)),
)

conn, err := grpc.NewClient(target,
    grpc.WithTransportCredentials(insecure.NewCredentials()),
    grpc.WithChainUnaryInterceptor(kitgrpc.UnaryClientInterceptor(opts)),
    grpc.WithChainStreamInterceptor(kitgrpc.StreamClientInterceptor(opts)),
)
```

A correlation ID read from incoming `x-request-id`/`x-correlation-id`
metadata (configurable via `Options.MetadataKeys`) is attached to the
context a server interceptor hands the handler — retrieve it with
`kitgrpc.RequestIDFrom(ctx)` — and rides along automatically on any outbound
call the handler makes through a client interceptor from this package, the
same end-to-end correlation `httpmw` gives an HTTP request.

grpc-go does not recover a panicking handler on its own; left unhandled, one
panicking RPC takes down every other call the process is serving. Both
server interceptors recover by default and respond with `codes.Internal`
after logging the panic and its stack — set `Options.DisablePanicRecovery`
to log and re-panic instead.

Request and response payloads are never logged unless `Options.LogPayloads`
is set: they routinely carry PII, credentials or bodies too large for a log
line, so this is opt-in rather than a default a caller has to remember to
turn off. It only applies to the unary interceptors — streaming payloads are
never logged by this package.

**`StreamClientInterceptor` logs exactly once per stream, as soon as any of
these says the stream is over** — a client stream has no explicit "close"
callback to hook, unlike a server stream whose handler simply returns:

- `RecvMsg` returns a terminal error (`io.EOF` on a clean end, anything else
  otherwise);
- `RecvMsg` succeeds on a call whose `StreamDesc.ServerStreams` is `false` —
  a client-streaming RPC where the server sends exactly one response, so
  receiving it successfully *is* the end of the stream and nothing else will
  ever call `RecvMsg` again to observe an EOF;
- `SendMsg` or `CloseSend` returns an error;
- the call's `context.Context` is done (cancelled or past its deadline)
  before any of the above happened, so a caller that abandons a
  server-streaming or bidi stream mid-flight still gets a log line instead
  of none.

A successful `CloseSend` alone never finalizes: the caller may still be
waiting on one or more responses.

`UnaryLoggingInterceptor()` remains as `UnaryServerInterceptor(Options{})`
for source compatibility; existing callers gain panic recovery and
status-derived levels along with it.

**The response writer keeps its optional interfaces.** A wrapper that embeds the
`http.ResponseWriter` *interface* promotes only `Header`, `Write` and
`WriteHeader` — so `w.(http.Flusher)` fails behind it and SSE, WebSocket
upgrades, HTTP/2 push and `httputil.ReverseProxy` all break. This wrapper
implements `http.Flusher`, `http.Hijacker`, `http.Pusher` and `io.ReaderFrom`
explicitly, plus `Unwrap()` for `http.NewResponseController`.

The trade-off, stated plainly: because those methods are declared
unconditionally, a type assertion now always succeeds. When the underlying
writer cannot do the thing, `Flush` is a no-op, `Hijack` and `Push` return
`http.ErrNotSupported`, and `ReadFrom` falls back to `io.Copy` — still counting
the bytes. Use `http.NewResponseController(w)` when you need an honest answer.

**The request ID is actually propagated.** It is read from `X-Request-Id` (or
`Options.RequestIDHeader`), generated when absent, put in the request context,
echoed on the response, and logged:

```go
func handler(w http.ResponseWriter, r *http.Request) {
    id, ok := httpmw.RequestIDFrom(r.Context())
    if ok {
        w.Header().Set("X-Handled-Request-Id", id)
    }
}
```

An inbound header is reused verbatim only when it is usable — non-empty, at most
200 bytes, printable ASCII. It is attacker-controlled and goes straight into the
log stream, so a value carrying a newline (which can forge log entries in any
line-oriented format) is replaced with a generated one.

Wire it to `Config.ContextFields` and every log line in the request gets it for
free:

```go
log := logger.New(logger.Config{
    ContextFields: func(ctx context.Context) []any {
        if id, ok := httpmw.RequestIDFrom(ctx); ok {
            return []any{"request_id", id}
        }
        return nil
    },
})
```

**Panics are logged and re-panicked.** A handler that panics used to produce no
kit-logger line at all — the endpoint that was 100% broken was the one with no
log entries. It now emits one `Error` line with `panic` and `stack`, then
re-panics so the server behaves exactly as before. `http.ErrAbortHandler` is
net/http's quiet abort signal, so it is logged without a stack trace.

The rest of what changed: `status` is the **first** `WriteHeader` (what the
client actually received) rather than the last, `bytes_written` is recorded, the
level follows the outcome (5xx → Error, 4xx → Warn, else Info — override with
`Options.LevelFor`), a cancelled request carries `context_err`, and ID
generation can no longer panic the request.

## Known limitations

Sharp edges that are true at HEAD, each with the issue that explains it. The
list is meant to shrink; when one disappears, remove it here.

- **A `Sink` never receives `WithAttrs` or `WithGroup`**
  ([#42](https://github.com/pablogore/kit-logger/issues/42)). The dedup stage
  holds them and materializes every attribute into the record, nested with
  `slog.GroupValue` where a group was open, so a custom sink that relied on
  pre-formatting `With` attributes sees them on the record instead.
- **`Config.PipelineOverride` ignores most of `Config`**
  ([#11](https://github.com/pablogore/kit-logger/issues/11)). It is the one
  total bypass: `Sink`/`Handler`, `FilterRules`, `GlobalFields`, `Sampling`,
  `MetricsHandler`, `BufferSize`, `Hook`, `Writer`, `Format` and `Level` are
  ignored, and `Config.Validate()` names each one that was set. `RateLimit`,
  `ContextFields` and `ContextHandler` still apply. `Config.Handler` used to
  behave this way; it is now a deprecated alias for `Sink` and bypasses
  nothing.
- **`Sampling.Probability == 0` means unset and emits everything**
  ([#4](https://github.com/pablogore/kit-logger/issues/4)). Zero is not "drop
  everything"; set an explicit value below `1` to sample. A non-positive
  `RateLimit` interval follows the same rule: it is tracked, counted by
  `RateLimitInvalid` and reported once on stderr, but never limits
  ([#5](https://github.com/pablogore/kit-logger/issues/5)).
- **`Sync()` flushes without a deadline**
  ([#3](https://github.com/pablogore/kit-logger/issues/3)). It is
  `Flush(context.Background())`, kept for source compatibility and deprecated.
  A stuck downstream handler can hold the caller; prefer `Flush(ctx)` or
  `Shutdown(ctx)` with a timeout.
- **Metrics are opt-in; importing the package registers nothing**
  ([#10](https://github.com/pablogore/kit-logger/issues/10)). Without
  `Config.MetricsHandler` no metric exists, and the old always-on
  `slog_logged_total` is gone. See [Prometheus Metrics](#prometheus-metrics)
  for the migration.
- **With `AddSource` on, a caller attribute named `source` is replaced**
  ([#42](https://github.com/pablogore/kit-logger/issues/42)). `source` is the
  key slog reserves for call-site attribution, and the pipeline's own group
  wins under the usual last-value rule. Leave `slog.HandlerOptions.AddSource`
  off on a custom `Sink` as well, or the same call site appears twice.
- **`ManagedLogger` is reached by type assertion.** Every logger built by
  `New` satisfies it; a consumer's own `Logger` implementation may not, in
  which case `ExitWithFlush` falls back to `Sync()`.

## Package Structure

```
pkg/logger/
├── config.go                # Config, New, NewWithError, the global accessor
├── config_types.go          # typed Level / Format and the legacy string bridge
├── interface.go             # Logger
├── lifecycle.go             # ManagedLogger (Flush / Shutdown)
├── slog_logger.go
├── level.go
├── rate.go                  # WithRateLimit
├── helpers.go
├── loggerext.go             # ExitWithFlush
├── context_extracto.go
├── example_test.go          # compiled twins of this README's snippets
├── grpc/
│   └── interceptor.go       # Unary/Stream server and client interceptors
├── httpmw/
│   └── middleware.go        # New, Middleware, RequestIDFrom
├── otel/
│   └── handler.go           # OpenTelemetry trace correlation
├── prometheus/
│   └── handler.go           # opt-in Prometheus instrumentation (Config.MetricsHandler)
├── utils/
│   └── ...                  # shared helpers
├── kitlogtest/
│   └── ...                  # MockLogger, TestHandler and other test doubles for consumers of this module
└── handler/
    ├── buffered_handler.go     # async buffer with Flush / Shutdown
    ├── component_handler.go    # call-site attribution from Record.PC
    ├── filter_handler.go       # ModeDrop / ModeRedact
    ├── global_fields_handler.go
    ├── hook_handler.go
    ├── multi_handler.go
    ├── sampling_handler.go
    ├── dedup_handler.go        # innermost stage: one key per line, pinned global fields
    ├── source_handler.go       # opt-in call-site attribution (Config.AddSource)
    ├── console_handler.go      # FormatConsole, the development layout
    ├── component_handler.go    # deprecated; use source_handler.go
    └── prometheus_handler.go   # deprecated no-op shim; see pkg/logger/prometheus
```

## Run Tests

```bash
# Basic tests
go test ./... -cover

# Run tests with coverage threshold validation
make coverage-threshold

# Run complete CI pipeline with coverage validation
make ci-threshold

# Run specific test suites
make test              # All tests
make test-race         # All tests with the race detector
make coverage          # Basic coverage report
make coverage-html     # HTML coverage report
make coverage-func     # Function-level coverage breakdown
```