# AGENTS.md — kit-logger

## Authority

This document is the source of truth for how kit-logger is designed, implemented, and validated. Any change that conflicts with it is out of scope. When in doubt, preserve the invariants below over feature requests.

---

## What kit-logger is

kit-logger is a structured logging library built on `log/slog`. Its entire purpose is I/O: writing records to a destination, talking to Prometheus, sampling with real randomness, and rate-limiting against a real clock. It is not a domain-pure library, and it must not be redesigned as one — I/O, package-level state, and use of `time`/`math/rand` are the point, not violations to eliminate.

---

## Core Principles

- **I/O is the product** — `New` assembles a handler pipeline (`Writer`, `FilterRules`, `GlobalFields`, the component handler, `Sampling`, the Prometheus handler, buffering, `Hook`) whose job is to write log records somewhere. Do not push I/O out of this library; it belongs here.
- **Real time and real randomness by default, with injection points for tests** — Production code paths use `time.Now()` (rate limiting, sampling defaults) and `math/rand/v2.Float64()` (sampling). Every one of these has an explicit override field for deterministic tests (`SamplingConfig.Now`/`Rand`, `rateState.now`) — use those overrides in tests instead of asking for the real dependency to be removed from production code.
- **Package-level state is deliberate, and protected with `atomic.Pointer`** — `globalLogger` and `globalContextFieldExtractor` are package-level `atomic.Pointer[T]` values, not `sync.RWMutex`-guarded state, because they sit on the per-log hot path. This is a considered performance tradeoff, not an oversight to "fix" by removing the globals or switching the synchronization primitive.
- **`sync.Once` singleton registration is used deliberately** — `pkg/logger/handler/prometheus_handler.go`'s `init()` registers the Prometheus collector exactly once via `registerOnce`, falling back gracefully on `AlreadyRegisteredError`. This is the correct pattern for a package-level Prometheus collector; do not replace it with constructor-injected registration without a concrete reason tied to a real bug.
- **Fail toward emitting, not toward silence** — When a config value is ambiguous, this library prefers to log more rather than drop records silently: `SamplingConfig.Probability == 0` means "unset" and is treated as `1` (emit everything, not "drop everything"); a non-positive `RateLimit` interval is tracked but never limits. Any new config surface should default the same way unless there's a specific reason not to.

---

## Architectural Invariants

- **`Config.Handler` bypasses the built-in pipeline entirely** — When a caller supplies their own `slog.Handler`, `New` skips `Writer`, `FilterRules`, `GlobalFields`, the component handler, `Sampling`, the Prometheus handler, `BufferSize`, and `Hook`. Only `Level` and `RateLimit`/`Counter` (via the `SlogLogger` wrapper itself) still apply. Keep this behavior — it's what lets a caller take full control of the handler chain — but never let it silently regress into applying only some of the built-in stages.
- **Filtering drops whole records, it does not redact fields** — `FilterHandler` matches a rule and discards the entire record. There is no partial masking or field-level redaction anywhere in this codebase. Do not describe or imply redaction in docs, comments, or new features unless field-level redaction is actually implemented.
- **The lifecycle (`Flush`/`Shutdown`/`Sync`) must reach the buffer regardless of chain depth** — `ManagedLogger` is discovered through the `Unwrap`/`UnwrapAll` methods the built-in handlers implement, so a hand-assembled chain passed as `Config.Handler` still exposes lifecycle control as long as each handler in the chain implements `Unwrap`. New handlers must implement `Unwrap` (or `UnwrapAll` for handlers with multiple children) to preserve this.

---

## Engineering Discipline

- **Tests that need determinism use the injection points, not mocks of the standard library** — Prefer `SamplingConfig.Now`/`Rand` and `rateState.now`-style fields over wrapping `time`/`math/rand` behind a new interface just for this library. The pattern already exists; follow it.
- **Test doubles for consumers live in `pkg/logger/kitlogtest`, never in production packages** — `MockLogger`, `TestHandler`, and similar doubles are for *other modules* that import kit-logger and want to fake it out in their own tests. They must not live alongside the production handler/logger code.
- **`gofmt -l ./pkg ./scripts` must be empty** — Formatting drift is a real acceptance criterion (checked by `make fmt-check` and CI), not a style nit.

---

## AI Agent Behavior Expectations

- **Do not propose removing I/O, global state, or time/rand usage as "cleanup"** — Those are correct here. If something about the current design does look wrong, say so explicitly and explain the concrete bug or risk — don't default to a domain-purity rewrite.
- **When adding a new config field, decide its "unset" behavior consciously** — follow the fail-toward-emitting convention above unless there's a stated reason to diverge, and document the choice.
- **Stop and ask if a change would alter what `Config.Handler` bypasses** — this is a documented, relied-upon contract; changing it silently would be a breaking change to every caller who wires their own handler.

---

## Validation

See **[AGENTS.validation.md](./AGENTS.validation.md)** for the pre-code checklist.
