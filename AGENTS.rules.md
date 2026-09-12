# kit-logger — Agent Rules

**kit-logger** is an I/O-performing, stateful structured logging library. The rules below are **hard invariants for this codebase** — they describe what must stay true, and several of them are the *opposite* of the "no I/O / no global state / no time / no rand" rules you may have seen in other libraries in this organization (e.g. kit-core). Do not import those rules here.

---

## 1. I/O and the Handler Pipeline

- **I/O belongs in this library.** Writing to `Config.Writer`, exposing Prometheus metrics, and talking to gRPC/HTTP middleware are the library's job, not a boundary it delegates elsewhere.
- **`Config.Sink` is a terminal handler that the whole pipeline decorates.** `FilterRules`, `GlobalFields`, the component handler, `Sampling`, `MetricsHandler`, `BufferSize`, `Hook` and `SetLevel` apply on top of a caller-supplied `Sink` exactly as on the built-in handler. `Config.Handler` is a deprecated alias with identical behavior.
- **`Config.PipelineOverride` is the only escape hatch, and it bypasses the decoration pipeline entirely.** Setting it ignores `Sink`/`Handler`, `FilterRules`, `GlobalFields`, `Sampling`, `MetricsHandler`, `BufferSize`, `Hook`, `Writer`, `Format` and `Level`, and `Config.Validate()` reports each ignored field by name. `RateLimit`, `ContextFields` and `ContextHandler` still apply. Any change to `New` must preserve this all-or-nothing behavior — don't let it drift into applying some built-in stages but not others.
- **Filtering has two modes; name the one you mean.** `FilterHandler` matches record attrs, `With`/`WithAttrs` attrs and grouped attrs. `ModeDrop` discards the whole record; `ModeRedact` replaces only the matching value. Do not document "redaction" for a drop-mode rule or vice versa.
- **Global fields never emit a key twice.** A global field replaces a colliding record attribute in place or yields to it, in sorted key order, at the top level even under `WithGroup`. `With`-supplied attributes are outside its reach by design (see AGENTS.md, Known deviations).

---

## 2. Time and Randomness (Deliberately Real, With Injection Points)

- **Production code uses the real clock and real randomness by default.** `time.Now()` drives rate limiting and sampling; `math/rand/v2.Float64()` drives sampling probability. This is correct — do not replace it with an injected `Clock`/`Rand` interface across the whole library.
- **Determinism for tests comes from existing override fields, not new abstractions.** Use `SamplingConfig.Now`/`Rand` and `rateState.now`. If a new code path needs deterministic testing, add a field in the same style rather than introducing a new clock/rand interface.
- **`SamplingConfig.Probability == 0` means "unset," and resolves to `1` (emit everything).** Do not "fix" this into meaning "sample nothing" — it is intentional fail-toward-emitting behavior.
- **A non-positive `RateLimit` interval is tracked but never limits.** Same philosophy: ambiguous config fails toward emitting, not toward silence.
- **`SamplingConfig.MaxKeys` fails toward a bounded default, not toward emitting.** It governs memory, not emission, so a non-positive value becomes `DefaultSamplingMaxKeys` and `Validate` still reports a negative one.

---

## 3. Global State

- **`globalLogger` and `globalContextFieldExtractor` are package-level `atomic.Pointer[T]` values, by design.** They sit on the per-log hot path, which is why they use `atomic.Pointer` instead of `sync.RWMutex`. Do not remove these globals or swap the synchronization primitive without a concrete, measured reason.
- **`SetGlobal(l)` does not call `slog.SetDefault`.** Installing this library's global must not silently rewire the standard library's default logger for the whole process. Use `SetGlobalAndSlogDefault(l)` for that, explicitly.
- **`SetGlobal(nil)` panics, deliberately.** A stored nil logger would turn every later `L()` call into a nil dereference far from the mistake — panicking immediately at the point of misuse is the intended behavior.
- **There is no package-level Prometheus collector and no `init()` registration.** Metrics are opt-in through `Config.MetricsHandler`; `pkg/logger/prometheus` holds a per-instance collector and takes its `Registerer` explicitly. `pkg/logger` and `pkg/logger/handler` must not import `client_golang` (`go list -deps` is the check).

---

## 4. Configuration

- **New config fields must decide "unset" behavior consciously**, following the fail-toward-emitting convention (§2) unless there's a stated reason to diverge — and that reason must be documented in code comments and, if user-facing, in the README.
- **The `Sink`-is-decorated and `PipelineOverride`-ignores-everything contracts (§1) are relied on by other modules.** Treat any change to what either one covers as a breaking change requiring explicit sign-off, not a routine refactor.
- **Typed `Level`/`Format` are the API; the `LevelString`/`FormatString` bridge is validated, not defaulted.** An unparseable legacy string is reported by `Config.Validate()`, never silently mapped to a default (issue #13).

---

## 5. Testing

- **Test doubles for consumers (`MockLogger`, `TestHandler`, etc.) live only in `pkg/logger/kitlogtest`.** They must never live in production packages (`pkg/logger`, `pkg/logger/handler`, `pkg/logger/grpc`, `pkg/logger/httpmw`) — that was the exact bug fixed by issue #17.
- **Determinism in tests comes from the library's own injection points** (`SamplingConfig.Now`/`Rand`, `rateState.now`), not from mocking `time`/`math/rand` at the package level.
- **Every README Go snippet has a compiled `Example*` twin.** `pkg/logger/example_test.go` and each subpackage's `example_test.go` are the drift check; `make examples` runs them all. Edit the README and the example together.
- **`gofmt -l ./pkg ./scripts` must return nothing.** Run `make fmt-check` before proposing a change is done; `make fmt` to fix drift.
- **Race-sensitive code (the `atomic.Pointer` globals, rate limiting, buffering) must pass `go test -race ./...`**, which `make test-race` and the `ci`/`ci-threshold`/`ci-complete` targets run.
- **`pkg/logger/handler` runs under `goleak.VerifyTestMain`.** A test or benchmark that builds a `BufferedHandler` must `Shutdown` it, or the package fails.

---

## 6. Change Discipline

- **A change that silently narrows or widens what `Sink` is decorated with or what `PipelineOverride` ignores, or that flips a fail-toward-emitting default to fail-toward-silence, must not be merged without explicit discussion.**
- **Ambiguity requires STOP and ASK.** If a change is unclear with respect to these rules — especially anything touching the global state pattern, the pipeline contracts, or an "unset means what?" config decision — do not assume. Ask before implementing.

---

*These rules apply to all code and changes in kit-logger. They intentionally diverge from domain-purity rules used elsewhere in this organization (e.g. kit-core) because this library's whole purpose is I/O, time, randomness, and shared state.*
