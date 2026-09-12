# AGENTS.md — kit-logger

## Authority

This document is the source of truth for how kit-logger is designed, implemented, and validated. Any change that conflicts with it is out of scope. When in doubt, preserve the invariants below over feature requests.

Every invariant stated here holds at HEAD, or is listed under **Known deviations** at the bottom with the issue that explains it. If you find one that is neither, that is a bug in this document or in the code — say so, do not silently pick a side.

---

## What kit-logger is

kit-logger is a structured logging library built on `log/slog`. Its entire purpose is I/O: writing records to a destination, optionally counting them into Prometheus, sampling with real randomness, and rate-limiting against a real clock. It is not a domain-pure library, and it must not be redesigned as one — I/O, package-level state, and use of `time`/`math/rand` are the point, not violations to eliminate.

---

## Core Principles

- **I/O is the product** — `New` assembles a handler pipeline over a terminal sink (the built-in Text/JSON handler on `Writer`, or a caller-supplied `Sink`): `FilterRules` → `GlobalFields` → the component handler → `Sampling` → `MetricsHandler` (opt-in) → `BufferSize` → `Hook`, with `ContextHandler` as the outermost decorator so it runs on the caller's goroutine. Its job is to write log records somewhere. Do not push I/O out of this library; it belongs here.
- **Real time and real randomness by default, with injection points for tests** — Production code paths use `time.Now()` (rate limiting, sampling defaults) and `math/rand/v2.Float64()` (sampling). Every one of these has an explicit override field for deterministic tests (`SamplingConfig.Now`/`Rand`, `rateState.now`) — use those overrides in tests instead of asking for the real dependency to be removed from production code.
- **Package-level state is deliberate, and protected with `atomic.Pointer`** — `globalLogger` and `globalContextFieldExtractor` are package-level `atomic.Pointer[T]` values, not `sync.RWMutex`-guarded state, because they sit on the per-log hot path. This is a considered performance tradeoff, not an oversight to "fix" by removing the globals or switching the synchronization primitive.
- **Metrics are opt-in and injected, never an import side effect** — Importing `pkg/logger` or `pkg/logger/handler` registers nothing anywhere; neither package imports `client_golang`. Instrumentation is wired through `Config.MetricsHandler`, and `pkg/logger/prometheus` (the only package that depends on `client_golang`) takes its `prometheus.Registerer` explicitly. Do not reintroduce an `init()`, a package-level collector, or a `sync.Once` registration (issue #10).
- **Fail toward emitting, not toward silence** — When a config value is ambiguous, this library prefers to log more rather than drop records silently: `SamplingConfig.Probability == 0` means "unset" and is treated as `1` (emit everything, not "drop everything"); a non-positive `RateLimit` interval is tracked but never limits. Any new config surface should default the same way unless there's a specific reason not to. Values that govern memory rather than emission (`SamplingConfig.MaxKeys`) fail toward a bounded default instead.

---

## Architectural Invariants

- **`Config.Sink` is a terminal handler, not a pipeline bypass** — A caller-supplied `Sink` receives fully decorated records: `FilterRules`, `GlobalFields`, component attribution, `Sampling`, `MetricsHandler`, `BufferSize`, `Hook` and `SetLevel` all apply on top of it, exactly as they do on the built-in Text/JSON handler. `Config.Handler` is a deprecated alias for `Sink` (`Sink` wins if both are set) and behaves identically (issue #11).
- **`Config.PipelineOverride` is the only total bypass, and it is loud about it** — It replaces the decoration pipeline verbatim: `Sink`/`Handler`, `FilterRules`, `GlobalFields`, `Sampling`, `MetricsHandler`, `BufferSize`, `Hook`, `Writer`, `Format` and `Level` are ignored, and `Config.Validate()` reports an error naming each one that was set. Logger-level behavior outside the chain (`RateLimit`, `ContextFields`, `ContextHandler`) still applies. Never let this drift into applying some stages but not others, and never let a stage silently start or stop being ignored.
- **Filtering has two explicit modes and covers every attribute source** — `FilterHandler` matches record attrs, `With`/`WithAttrs` attrs and grouped attrs alike, by bare key or dotted path. `ModeDrop` (the `NewFilterHandler` default) discards the whole record; `ModeRedact` replaces only the matching value. Docs and comments must say which mode they describe; "filtering" alone is ambiguous (issue #16).
- **Global fields never produce a duplicate key** — `GlobalFieldsHandler` replaces a colliding record attribute in place (override) or keeps the record's (no override), emits fields in sorted key order, and keeps them at the top level under `WithGroup`. It inspects record attributes only; see Known deviations (issue #12).
- **The lifecycle (`Flush`/`Shutdown`/`Sync`) must reach the buffer regardless of chain depth** — `ManagedLogger` is discovered through the `Unwrap`/`UnwrapAll` methods the built-in handlers implement, so a hand-assembled chain passed as `PipelineOverride` still exposes lifecycle control as long as each handler in the chain implements `Unwrap`. New handlers must implement `Unwrap` (or `UnwrapAll` for handlers with multiple children) to preserve this. `Sync()` is `Flush` with a background context and is deprecated, not a no-op (issue #3).
- **Source attribution comes from `slog.Record.PC`, never from a live stack walk** — The component handler resolves the call site from the PC slog captured at the log call, so attribution survives asynchronous delivery through the buffer (issue #7).

---

## Engineering Discipline

- **Tests that need determinism use the injection points, not mocks of the standard library** — Prefer `SamplingConfig.Now`/`Rand` and `rateState.now`-style fields over wrapping `time`/`math/rand` behind a new interface just for this library. The pattern already exists; follow it.
- **Test doubles for consumers live in `pkg/logger/kitlogtest`, never in production packages** — `MockLogger`, `TestHandler`, and similar doubles are for *other modules* that import kit-logger and want to fake it out in their own tests. They must not live alongside the production handler/logger code.
- **Every README code sample is a compiled `Example*` function** — `pkg/logger/example_test.go` and the `example_test.go` in each subpackage mirror the README's snippets; `go vet` and `make examples` fail when either side drifts. Change both together.
- **`gofmt -l ./pkg ./scripts` must be empty** — Formatting drift is a real acceptance criterion (checked by `make fmt-check` and CI), not a style nit.
- **Goroutine leaks are test failures** — `pkg/logger/handler` runs under `goleak.VerifyTestMain`; any test or example that builds a `BufferedHandler` must `Shutdown` it.

---

## AI Agent Behavior Expectations

- **Do not propose removing I/O, global state, or time/rand usage as "cleanup"** — Those are correct here. If something about the current design does look wrong, say so explicitly and explain the concrete bug or risk — don't default to a domain-purity rewrite.
- **When adding a new config field, decide its "unset" behavior consciously** — follow the fail-toward-emitting convention above unless there's a stated reason to diverge, and document the choice.
- **Stop and ask if a change would alter what `Sink` is decorated with or what `PipelineOverride` ignores** — both are documented, relied-upon contracts; changing either silently is a breaking change for every caller who wires their own handler.

---

## Known deviations

Places where a principle above is bent on purpose. Each is documented in the README's "Known limitations" section and in code comments; none is an accident to be fixed by a purity pass.

| Principle | Deviation | Why | Issue |
|---|---|---|---|
| Global fields never produce a duplicate key | A `With("k", ...)` attribute colliding with a global `k` reaches the output alongside it | `With` attrs are delegated to the sink for pre-formatting, as slog intends, so the handler cannot see them; inspecting them would cost that optimization on every record | #12 |
| Fail toward emitting | `SetGlobal(nil)` panics instead of keeping the previous logger | A stored nil turns every later `L()` into a nil dereference far from the mistake; failing at the call site is the honest option | #2 |
| One attribution per record | `ComponentHandler` and `slog.HandlerOptions.AddSource` on a custom `Sink` both emit the call site | `AddSource` belongs to the sink, which the pipeline does not control; keep it off when using the component handler | #7 |

---

## Validation

See **[AGENTS.validation.md](./AGENTS.validation.md)** for the pre-code checklist.
