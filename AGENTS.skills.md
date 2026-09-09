# AGENTS.skills.md — kit-logger

Required skills for contributors. kit-logger is an I/O-performing logging library built on `log/slog`. Think in terms of pipeline composition and observable behavior, not domain purity.

---

## 1. Handler Pipeline Composition

- **Understand the full pipeline `New` assembles** — `Writer` → `FilterRules` → `GlobalFields` → component handler → `Sampling` → Prometheus handler → buffering → `Hook`. Know what each stage does before changing the order or adding a new one.
- **Know exactly what `Config.Handler` bypasses** — all of the above except `Level` and `RateLimit`/`Counter`. When touching `New`, verify this scope hasn't drifted (a stage silently starting or stopping being bypassed is a real regression, not a refactor detail).
- **Implement `Unwrap`/`UnwrapAll` on any new handler** — this is how `ManagedLogger` lifecycle discovery (`Flush`/`Shutdown`) reaches through a hand-assembled chain passed as `Config.Handler`. A handler that doesn't implement it breaks lifecycle control for anyone who wraps it.

---

## 2. Working With Real Time and Randomness

- **Recognize the injection pattern already in use** — `SamplingConfig.Now`/`Rand`, `rateState.now`. When a new code path needs a deterministic test, add a field in this style rather than introducing a new abstraction layer.
- **Know the fail-toward-emitting defaults** — `SamplingConfig.Probability == 0` → treated as `1` (emit everything); non-positive `RateLimit` → tracked, never limits. Any new "what does zero/unset mean" decision should default the same way unless there's a documented reason not to.
- **Don't design around removing `time.Now()`/`math/rand` from production paths** — they're correct there. The skill is choosing the right *test* override, not eliminating the real dependency.

---

## 3. Concurrency and Global State

- **Recognize why `atomic.Pointer[T]` is used for `globalLogger`/`globalContextFieldExtractor`** — it's a hot-path performance choice over `sync.RWMutex`. When touching this code, preserve the pattern; don't "simplify" it into a mutex without benchmarking the hot path it protects.
- **Recognize the `sync.Once` Prometheus registration pattern** in `prometheus_handler.go`'s `init()` — package-level, idempotent, tolerant of `AlreadyRegisteredError`. This is the correct shape for a package-level metrics collector.
- **Reason about `-race` correctness for anything touching package-level state, the rate limiter, or the async buffer** — these are exactly the places races have been found and fixed before (see recent history: issues #24, #25).

---

## 4. Filtering and Sampling Semantics

- **Filtering is all-or-nothing** — `FilterHandler` drops the entire record on a match. There is no field-level redaction. Don't build features (or write docs) that imply partial masking exists.
- **Sampling and rate limiting are independent, composable stages** — know which one a given bug report or feature request is actually about before changing code; they have different semantics (probability vs. token-bucket interval) and different "unset" conventions.

---

## 5. Test Doubles vs. Production Code

- **Know the boundary**: `pkg/logger/kitlogtest` holds test doubles for *consumers* of this module (`MockLogger`, `TestHandler`, etc.). Production packages (`pkg/logger`, `pkg/logger/handler`, `pkg/logger/grpc`, `pkg/logger/httpmw`) must never contain mocks or test-only doubles — that was the exact defect fixed by issue #17.
- **Use the library's own injection points in tests** rather than reaching for a mocking framework to fake out `time` or `math/rand`.

---

## 6. Testing Discipline

- **Run the real test matrix**: `go build ./...`, `go vet ./...`, `go test ./...`, `go test -race ./...`, `gofmt -l ./pkg ./scripts` (or the equivalent `make` targets: `test`, `test-race`, `fmt-check`, `vet`). A change isn't done until all of these are clean.
- **Example functions in `pkg/logger/example_test.go` exist to catch drift between the README's code samples and the real API** — they compile and run (via `go vet`/`go test`) even without an `// Output:` comment, since the JSON logger's timestamp makes output-matching impractical. Keep them in sync with the README when either changes.

---

## 7. Restraint

- **Know when NOT to "fix" something that's working as designed** — I/O in the pipeline, global state guarded by `atomic.Pointer`, real time/rand with test overrides, and fail-toward-emitting defaults are all deliberate. If a change would remove one of these, it needs a concrete bug or measured regression behind it, not a purity argument.
- **Don't widen `Config.Handler`'s bypass scope, or narrow it, without flagging the contract change explicitly** — other modules depend on its exact current behavior.

---

*Tone: pragmatic, operational — this library's job is to move log records reliably, not to stay pure.*
