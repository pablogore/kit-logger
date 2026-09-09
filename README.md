# go-kit/logger

An extensible structured logging framework for Go, built on top of `log/slog`, designed for observable, secure, and efficient microservices.

## Features

- Full support for `log/slog`
- Automatic redaction of sensitive fields
- Built-in Prometheus metrics
- Conditional filters by key, value, and level
- Sampling and rate limiting
- Context-aware and extensible with hooks
- Asynchronous buffering with safe shutdown
- Support for global fields and component-based logging
- Pluggable HTTP and gRPC interceptors

## Basic Usage

```go
import "github.com/getsyntegrity/go-kit-logger/pkg/logger"

func main() {
    log := logger.New(logger.Config{
        Level:  "info",
        Format: "json",
        GlobalFields: map[string]string{
            "service": "my-api",
            "env":     "prod",
        },
    })

    logger.SetGlobal(log)
    log.Info(context.Background(), "starting service")
}
```

## Destination

By default the logger writes to stdout. Set `Config.Writer` to send output anywhere else — `os.Stderr`, a file, an `io.Writer` used in tests, or a third-party writer such as [`lumberjack`](https://github.com/natefinch/lumberjack) for rotating files:

```go
log := logger.New(logger.Config{
    Level:  "info",
    Format: "json",
    Writer: os.Stderr, // keeps stdout free for a CLI's own machine-readable output
})
```

`Writer` composes with the rest of `Config` (`GlobalFields`, `FilterRules`, `Sampling`, `BufferSize`, `Hook`, `ContextFields`) — it only changes where the pipeline's output lands. It is ignored when `Config.Handler` is set.

## Lifecycle

Buffered logging is asynchronous, so a process that exits without draining loses
its in-flight tail — exactly the window where the interesting records live. The
lifecycle is therefore host-owned and explicit.

`Logger` is unchanged. Type-assert to `ManagedLogger` to reach it:

```go
log := logger.New(logger.Config{BufferSize: 4096, Format: "json"})
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
`Config.Handler` is discovered through the `Unwrap` / `UnwrapAll` methods the
built-in handlers implement.

## Globals and context fields

Prefer owning a logger and passing it where it is needed. The package-level
helpers remain for consumers that cannot do that yet, and they are now
race-free: `L()` and `SetGlobal()` publish through an `atomic.Pointer`, and the
lazy default is constructed exactly once no matter how many goroutines call
`L()` first.

```go
log := logger.New(logger.Config{Level: "info", Format: "json"})
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

A logger configured this way reads its own immutable field, so `WithContext`
touches no package-level state at all and is unaffected by another part of the
process calling `SetContextFieldExtractor`. That function still works as a
process-wide fallback for loggers without their own extractor, and is now
deprecated.

## Tests

The framework maintains over 85% test coverage on handlers, middleware, and testing utilities.

### Coverage Requirements

The project enforces a **minimum 85% test coverage threshold**. Coverage reports are generated automatically and the build will fail if the threshold is not met.

#### Running Coverage Reports

```bash
# Generate complete coverage report with threshold validation
make coverage-threshold

# Run CI pipeline with coverage validation
make ci-threshold

# Generate detailed coverage analysis
./scripts/coverage-complete-report.sh
```

#### Coverage Reports

The coverage system generates multiple report formats:
- **Main Report**: `coverage/coverage_report.txt` - Overall summary with threshold validation
- **Package Report**: `coverage/coverage_packages.txt` - Detailed analysis by package
- **File Report**: `coverage/coverage_files.txt` - Detailed analysis by file
- **HTML Report**: `coverage/coverage_report.html` - Interactive HTML coverage report

#### Current Coverage Status

- **Total Coverage**: 98.2% ✅
- **Threshold**: 85% ✅
- **Status**: PASS with 13.2% margin

## Prometheus Metrics

Automatically exposes:
```
slog_logged_total{level="INFO"} 123
```

## HTTP & gRPC Integration

- `grpc.UnaryInterceptor(logger.GRPCInterceptor())`
- `http.Handler = logger.HTTPMiddleware(next)`

## Package Structure

```
pkg/logger/
├── config.go
├── interface.go
├── slog_logger.go
├── grpc/
│   └── interceptor.go
├── httpmw/
│   └── middleware.go
└── handler/
    ├── sampling_handler.go
    ├── global_fields_handler.go
    ├── prometheus_handler.go
    └── ...
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
make test-all          # All tests including mocks
make coverage          # Basic coverage report
make coverage-html     # HTML coverage report
make coverage-func     # Function-level coverage breakdown
```