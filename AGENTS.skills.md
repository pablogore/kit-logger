# AGENTS.skills.md — kit-core

Required skills for contributors. kit-core is an infrastructure library. Think in terms of architectural purity.

---

## 1. Architectural Purity

- **Recognize side effects** — Identify any operation that mutates state, performs I/O, or depends on the environment. If it is not the declared purpose of the function or type, it does not belong there.
- **Keep domain pure** — Domain and core library logic must be pure: same inputs and injected dependencies yield the same outputs. No direct use of filesystem, network, process env, or global mutable state in domain code.
- **Prevent responsibility leakage** — Do not let infrastructure concerns (time, identity, persistence, transport) seep into domain types. Domain depends on interfaces only; implementations are supplied by the caller or an outer layer.

---

## 2. Deterministic Thinking

- **Think in terms of replay** — Design so that a run can be reproduced given the same inputs and injected dependencies. If a behavior cannot be replayed, it violates the library’s contract.
- **Avoid hidden time or randomness** — No `time.Now()`, no `math/rand`, no UUID-from-system in domain or core types. Time and identity are explicit dependencies (e.g. `Clock`, ID generator) passed in.
- **Design for reproducibility** — Every variable input must be an explicit parameter or an injected interface. Tests and production must be able to supply fakes or fixed values to get identical behavior.

---

## 3. Interface-First Design

- **Prefer abstractions** — Define what the domain needs (e.g. “something that provides the current time”) as an interface. Implementations live outside kit-core or in adapters that satisfy the interface.
- **Avoid concrete coupling** — Domain and core code must not depend on concrete types for time, IDs, or I/O. Depend on interfaces only; the caller wires implementations.
- **Recognize inversion of control** — The library defines contracts; the application or adapter layer provides the real clock, ID generator, repository, etc. kit-core does not own default implementations that read from the system.

---

## 4. Functional Reasoning

- **Prefer pure functions** — Where possible, use functions that depend only on their arguments and injected interfaces, with no hidden state or side effects. Mutations and I/O belong at boundaries.
- **Explicit inputs and outputs** — All variable inputs appear in the signature or in an injected dependency. Outputs are returned or written through explicit parameters; no global or hidden channels.
- **No hidden state** — No package-level mutable variables, no singletons. State that must exist is passed in (e.g. request context, constructor-injected dependencies) and is visible at the call site.

---

## 5. Boundary Awareness

- **Domain vs infrastructure** — Domain = rules, invariants, and types that describe “what” the system does. Infrastructure = time, persistence, network, env. Domain must not import or call infrastructure directly; it uses interfaces only.
- **Interfaces vs implementations** — kit-core owns and publishes interfaces (contracts). Implementations of those interfaces may live in this repo only as test fakes or examples; production implementations live in the application or in other packages.

---

## 6. Testing Discipline

- **Deterministic unit tests only** — Unit tests must not depend on wall clock, random seed, or environment. Same test code must produce the same result on every run.
- **Inject clock and ID** — Use fake or fixed `Clock` and ID sources in tests. Never call `time.Now()` or real RNG in unit tests. This keeps tests fast, stable, and reproducible.
- **No environmental coupling** — Unit tests must not read `os.Environ`, config files, or the host. All inputs are set in the test. Network, disk, or external services belong in a separate integration/E2E suite, not in core unit tests.

---

## 7. Restraint

- **Know when NOT to add a feature** — If a feature requires I/O in domain, hidden time, or global state, do not add it. Propose an interface and push the implementation to the caller or another package.
- **Reject scope creep** — kit-core stays minimal: contracts and pure logic. “Nice to have” helpers that introduce non-determinism or concrete coupling are out of scope. When in doubt, leave it out.

---

*Tone: architectural, serious, non-marketing.*
