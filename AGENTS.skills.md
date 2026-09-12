# AGENTS.skills.md — kit-logger

Required skills for contributors. kit-logger is an I/O-performing logging library built on `log/slog`. Think in terms of pipeline composition and observable behavior, not domain purity.

---

## 1. Handler Pipeline Composition

- **Understand the full pipeline `New` assembles** — terminal sink (`Writer` + `Format`, or `Sink`) → `FilterRules` → `GlobalFields` → component handler → `Sampling` → `MetricsHandler` (only when set) → `BufferSize` → `Hook`, with `ContextHandler` wrapped outermost. Know what each stage does before changing the order or adding a new one.
- **Know the two contracts around a caller's handler** — `Sink` (and its deprecated alias `Handler`) is decorated by every stage above plus `SetLevel`; `PipelineOverride` is decorated by none of them and makes `Config.Validate()` name every field it ignored. When touching `New`, verify neither scope has drifted (a stage silently starting or stopping being applied is a real regression, not a refactor detail).
- **Implement `Unwrap`/`UnwrapAll` on any new handler** — this is how `ManagedLogger` lifecycle discovery (`Flush`/`Shutdown`) reaches through a hand-assembled chain passed as `PipelineOverride`. A handler that doesn't implement it breaks lifecycle control for anyone who wraps it.
- **Decorators that must see `With` attributes hold them, they do not forward them** — `FilterHandler` (rules) and `GlobalFieldsHandler` (after a `WithGroup`) record `WithAttrs`/`WithGroup` calls and replay or nest them at `Handle` time, because a forwarded attribute is pre-formatted by the sink and invisible afterwards. Reuse that pattern rather than inventing another.

---

## 2. Working With Real Time and Randomness

- **Recognize the injection pattern already in use** — `SamplingConfig.Now`/`Rand`, `rateState.now`. When a new code path needs a deterministic test, add a field in this style rather than introducing a new abstraction layer.
- **Know the fail-toward-emitting defaults** — `SamplingConfig.Probability == 0` → treated as `1` (emit everything); non-positive `RateLimit` → tracked, never limits. `SamplingConfig.MaxKeys` is the deliberate exception: it bounds memory, so it fails toward `DefaultSamplingMaxKeys`. Any new "what does zero/unset mean" decision should default the same way unless there's a documented reason not to.
- **Don't design around removing `time.Now()`/`math/rand` from production paths** — they're correct there. The skill is choosing the right *test* override, not eliminating the real dependency.

---

## 3. Concurrency and Global State

- **Recognize why `atomic.Pointer[T]` is used for `globalLogger`/`globalContextFieldExtractor`** — it's a hot-path performance choice over `sync.RWMutex`. When touching this code, preserve the pattern; don't "simplify" it into a mutex without benchmarking the hot path it protects.
- **Recognize the opt-in metrics seam** — `Config.MetricsHandler func(next slog.Handler) (slog.Handler, error)` is how instrumentation enters the pipeline, and `pkg/logger/prometheus` is the only package allowed to import `client_golang`. A registration collision returns an error, never panics in `init()`.
- **Reason about `-race` correctness for anything touching package-level state, the rate limiter, or the async buffer** — these are exactly the places races have been found and fixed before (issues #1, #2, #5, #15).

---

## 4. Filtering, Global Fields and Sampling Semantics

- **Filtering is two-mode** — `ModeDrop` discards the entire record; `ModeRedact` replaces the matching value and keeps the rest. Rules match record attrs, `With`/`WithAttrs` attrs and grouped attrs, by bare key or dotted path. Say which mode a doc or feature means.
- **Global fields replace, they do not append** — under override a colliding record attr is replaced in place; without it the record wins. Order is sorted and deterministic, and globals stay top-level under `WithGroup`. They do not see `With` attributes; that limitation is documented, not a bug to paper over.
- **Sampling and rate limiting are independent, composable stages** — know which one a given bug report or feature request is actually about before changing code; they have different semantics (probability vs. per-key interval) and different "unset" conventions.

---

## 5. Test Doubles vs. Production Code

- **Know the boundary**: `pkg/logger/kitlogtest` holds test doubles for *consumers* of this module (`MockLogger`, `TestHandler`, etc.). Production packages (`pkg/logger`, `pkg/logger/handler`, `pkg/logger/grpc`, `pkg/logger/httpmw`) must never contain mocks or test-only doubles — that was the exact defect fixed by issue #17.
- **Use the library's own injection points in tests** rather than reaching for a mocking framework to fake out `time` or `math/rand`.

---

## 6. Testing Discipline

- **Run the real test matrix**: `go build ./...`, `go vet ./...`, `go test ./...`, `go test -race ./...`, `gofmt -l ./pkg ./scripts` (or the equivalent `make` targets: `test`, `test-race`, `fmt-check`, `vet`). A change isn't done until all of these are clean.
- **Example functions exist to catch drift between the README's code samples and the real API** — `pkg/logger/example_test.go` covers the core `Config` snippets; `pkg/logger/{grpc,httpmw,otel,prometheus}/example_test.go` cover their sections. They compile under `go vet`/`go test` even without an `// Output:` comment, since the JSON logger's timestamp makes output-matching impractical. `make examples` runs them all. Keep them in sync with the README when either changes.
- **Respect `goleak`** — `pkg/logger/handler` verifies no goroutine outlives the test binary. Shut down every `BufferedHandler` you build.

---

## 7. Restraint

- **Know when NOT to "fix" something that's working as designed** — I/O in the pipeline, global state guarded by `atomic.Pointer`, real time/rand with test overrides, and fail-toward-emitting defaults are all deliberate. If a change would remove one of these, it needs a concrete bug or measured regression behind it, not a purity argument.
- **Don't widen or narrow what `Sink` is decorated with, or what `PipelineOverride` ignores, without flagging the contract change explicitly** — other modules depend on the exact current behavior.

---

*Tone: pragmatic, operational — this library's job is to move log records reliably, not to stay pure.*
