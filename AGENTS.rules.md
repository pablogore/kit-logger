# kit-core — Agent Rules

**kit-core** is a minimal deterministic infrastructure library. The rules below are **hard invariants**. They are non-negotiable.

---

## 1. Hexagonal Boundaries

- **Domain must not import adapters.** Domain packages may not depend on any adapter, driver, or infrastructure implementation. Dependencies point inward only.
- **No I/O in domain.** Domain code must not perform filesystem, network, or any other I/O. All I/O is performed by adapters that implement interfaces defined in the domain or application.
- **No runtime wiring in domain.** Dependency injection, wiring, and composition belong in composition roots or application bootstrap—not in domain packages.
- **No HTTP, gRPC, broker, or DB implementations in domain.** Domain and application layers define interfaces only. Concrete implementations (HTTP handlers, gRPC servers, message brokers, database clients) live in adapters.

---

## 2. Determinism (Strict)

- **No `time.Now()` in domain or application.** Use of the system clock in domain or application code is forbidden. Time must be obtained via an injected abstraction (e.g. clock interface).
- **No randomness unless injected via interface.** Do not use `math/rand`, `crypto/rand`, or any source of randomness in domain or application code unless it is provided through an explicitly injected interface.
- **No global ID generators.** IDs must not be produced by package-level or global generators. ID generation is a dependency and must be injected.
- **Clock must be injected.** Any code that needs the current time must receive a clock (or equivalent) via constructor or method parameter.
- **ID generation must be injected.** Any code that needs unique identifiers must receive an ID generator via constructor or method parameter.

---

## 3. Global State

- **No mutable global state.** Package-level variables that can be modified at runtime are forbidden.
- **No `sync.Once` singletons.** Lazy singletons hide dependencies and violate testability and determinism. Dependencies must be constructed and passed explicitly.
- **No hidden registries.** No global maps, registries, or lookup tables that adapters or callers register with at runtime. Explicit composition only.

---

## 4. Configuration

- **No direct `os.Getenv` in domain.** Domain and application code must not read environment variables. Configuration is an external concern.
- **Config must be explicit and injected.** Configuration (ports, feature flags, timeouts, etc.) must be passed in as a struct or interface. The source of configuration (env, file, flags) belongs in the composition root or adapter layer.

---

## 5. Testing (UT-CORE-1)

Unit tests must be deterministic and isolated:

- **No `time.Now()`** in tests. Use a fake or fixed clock.
- **No `rand`** in tests. Use deterministic or injected sources.
- **No network** in unit tests. No real HTTP, gRPC, or TCP. Use mocks, fakes, or in-memory implementations.
- **No real DB** in unit tests. Use in-memory stores, fakes, or mocks.
- **No environment reads** in unit tests. Do not call `os.Getenv` or read config from the process environment. Pass config explicitly.

Tests that violate any of the above are not considered unit tests under UT-CORE-1 and must not block on these rules only if explicitly categorized and isolated (e.g. integration tests).

---

## 6. Documentation

- **Every package must include `doc.go`.** Each package must have a `doc.go` (or equivalent package comment) that describes the package purpose and usage.
- **Every exported symbol must have GoDoc.** All exported types, functions, methods, and constants must have a comment that satisfies `go doc` and explains purpose and contract.
- **Invariants must be documented.** Where code enforces an invariant (e.g. “caller must not pass nil”), the comment must state it. Ambiguity in contracts is not acceptable.

---

## 7. Change Discipline

- **Any violation blocks merge.** A change that breaks any invariant in sections 1–6 must not be merged. Fix or revert.
- **Ambiguity requires STOP and ASK.** If a change or requirement is unclear with respect to these rules, do not assume. Stop and ask for clarification before implementing or approving.

---

*These rules apply to all code and changes in kit-core. Exceptions are not granted without an explicit, documented decision and update to this file.*
