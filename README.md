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
  releases the worker. It is idempotent and safe to call concurrently; every
  caller sees the same result.
- Records logged after `Shutdown` are discarded. They never panic and never
  block, and they are counted by `(*SlogLogger).Rejected()`.
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