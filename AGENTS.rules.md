# kit-logger — Agent Rules

**kit-logger** is an I/O-performing, stateful structured logging library. The rules below are **hard invariants for this codebase** — they describe what must stay true, and several of them are the *opposite* of the "no I/O / no global state / no time / no rand" rules you may have seen in other libraries in this organization (e.g. kit-core). Do not import those rules here.

---

## 1. I/O and the Handler Pipeline

- **I/O belongs in this library.** Writing to `Config.Writer`, exposing Prometheus metrics, and talking to gRPC/HTTP middleware are the library's job, not a boundary it delegates elsewhere.
- **`Config.Handler` is an escape hatch that bypasses the built-in pipeline entirely.** Setting it skips `Writer`, `FilterRules`, `GlobalFields`, the component handler, `Sampling`, the Prometheus handler, `BufferSize`, and `Hook`. Only `Level` and `RateLimit`/`Counter` still apply. Any change to `New` must preserve this all-or-nothing behavior — don't let it drift into applying some built-in stages but not others.
- **Filtering discards whole records.** `FilterHandler` has no partial redaction or field masking. Do not add code or documentation implying otherwise without actually implementing field-level redaction.

---

## 2. Time and Randomness (Deliberately Real, With Injection Points)

- **Production code uses the real clock and real randomness by default.** `time.Now()` drives rate limiting and sampling; `math/rand/v2.Float64()` drives sampling probability. This is correct — do not replace it with an injected `Clock`/`Rand` interface across the whole library.
- **Determinism for tests comes from existing override fields, not new abstractions.** Use `SamplingConfig.Now`/`Rand` and `rateState.now`. If a new code path needs deterministic testing, add a field in the same style rather than introducing a new clock/rand interface.
- **`SamplingConfig.Probability == 0` means "unset," and resolves to `1` (emit everything).** Do not "fix" this into meaning "sample nothing" — it is intentional fail-toward-emitting behavior.
- **A non-positive `RateLimit` interval is tracked but never limits.** Same philosophy: ambiguous config fails toward emitting, not toward silence.

---

## 3. Global State

- **`globalLogger` and `globalContextFieldExtractor` are package-level `atomic.Pointer[T]` values, by design.** They sit on the per-log hot path, which is why they use `atomic.Pointer` instead of `sync.RWMutex`. Do not remove these globals or swap the synchronization primitive without a concrete, measured reason.
- **`SetGlobal(l)` does not call `slog.SetDefault`.** Installing this library's global must not silently rewire the standard library's default logger for the whole process. Use `SetGlobalAndSlogDefault(l)` for that, explicitly.
- **`SetGlobal(nil)` panics, deliberately.** A stored nil logger would turn every later `L()` call into a nil dereference far from the mistake — panicking immediately at the point of misuse is the intended behavior.
- **The Prometheus collector registers exactly once via `sync.Once` in `init()`**, with `AlreadyRegisteredError` handled gracefully. This is the correct pattern here; do not replace package-level registration with constructor injection without a concrete bug driving the change.

---

## 4. Configuration

- **New config fields must decide "unset" behavior consciously**, following the fail-toward-emitting convention (§2) unless there's a stated reason to diverge — and that reason must be documented in code comments and, if user-facing, in the README.
- **`Config.Handler`'s pipeline-bypass scope (§1) is a contract other modules rely on.** Treat any change to what it bypasses as a breaking change requiring explicit sign-off, not a routine refactor.

---

## 5. Testing

- **Test doubles for consumers (`MockLogger`, `TestHandler`, etc.) live only in `pkg/logger/kitlogtest`.** They must never live in production packages (`pkg/logger`, `pkg/logger/handler`, `pkg/logger/grpc`, `pkg/logger/httpmw`) — that was the exact bug fixed by issue #17.
- **Determinism in tests comes from the library's own injection points** (`SamplingConfig.Now`/`Rand`, `rateState.now`), not from mocking `time`/`math/rand` at the package level.
- **`gofmt -l ./pkg ./scripts` must return nothing.** Run `make fmt-check` before proposing a change is done; `make fmt` to fix drift.
- **Race-sensitive code (the `atomic.Pointer` globals, rate limiting, buffering) must pass `go test -race ./...`**, which `make test-race` and the `ci`/`ci-threshold`/`ci-complete` targets run.

---

## 6. Change Discipline

- **A change that silently narrows or widens what `Config.Handler` bypasses, or that flips a fail-toward-emitting default to fail-toward-silence, must not be merged without explicit discussion.**
- **Ambiguity requires STOP and ASK.** If a change is unclear with respect to these rules — especially anything touching the global state pattern, the pipeline-bypass contract, or an "unset means what?" config decision — do not assume. Ask before implementing.

---

*These rules apply to all code and changes in kit-logger. They intentionally diverge from domain-purity rules used elsewhere in this organization (e.g. kit-core) because this library's whole purpose is I/O, time, randomness, and shared state.*
