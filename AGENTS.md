# AGENTS.md — kit-core

## Authority

This document is the source of truth for how kit-core is designed, implemented, and validated. Any change that conflicts with it is out of scope. When in doubt, preserve the invariants below over feature requests.

---

## Core Principles

- **Determinism** — Same inputs and injected dependencies yield the same outputs. No implicit time or randomness; all variable inputs are explicit and injectable.
- **No hidden side effects** — Functions do not perform I/O, change global state, or depend on environment unless that is their declared purpose and surfaced in their signature or interface.
- **Interface-first design** — Abstractions (Clock, ID, Repository, etc.) are defined as interfaces. Implementations live outside this repo; kit-core owns only the contracts.
- **No global state** — No package-level mutable variables, no singletons. Dependencies are passed in (constructor, handler, or request context).
- **Fail closed** — On ambiguity or error, prefer no behavior over incorrect or non-deterministic behavior. Surface errors; do not hide or default silently.

---

## Architectural Invariants

- **Domain must be pure** — Logic that expresses business or domain rules must be pure functions of their arguments and injected interfaces. No direct I/O, no `time`, no `math/rand` in that logic.
- **No I/O in domain** — Reading from the network, filesystem, or process environment is not allowed in domain or core library code. I/O happens only in adapters that implement kit-core interfaces.
- **No `time.Now()` in domain** — Time is obtained only via an injected `Clock` (or equivalent) interface. No use of `time.Now()` or similar in code that is part of kit-core’s domain or core types.
- **No randomness in domain** — Identity and any random-like values are produced only via injected abstractions (e.g. ID generator interface). No direct use of `math/rand`, `crypto/rand`, or UUID libraries in domain logic.
- **Clock and ID must be injected via interfaces** — All time and identity sources are dependencies provided by the caller. No default implementations that read from the real clock or system RNG inside kit-core.

---

## Engineering Discipline

- **Deterministic unit tests only** — Tests must be fully deterministic. No flaky tests; no tests that depend on wall clock, random seed, or environment. Use fake clocks and deterministic ID generators in tests.
- **No environment reads in unit tests** — Unit tests must not read from `os.Environ`, config files, or the host. All inputs are set in the test.
- **No network in unit tests** — Unit tests do not open sockets, make HTTP calls, or connect to any external service. Integration or E2E tests that need network belong in a separate suite and are not required for kit-core’s minimal scope.

---

## AI Agent Behavior Expectations

- **Explain reasoning before code** — Before proposing or editing code, state how it fits the principles and invariants above. If a change weakens determinism or introduces I/O in domain, do not propose it.
- **Refuse non-deterministic proposals** — Reject suggestions that add `time.Now()`, `rand`, or environment-dependent behavior to domain or core types. Propose injection via interfaces instead.
- **Stop and ask if ambiguity exists** — If a request could be implemented in a way that breaks invariants (e.g. adding a default implementation that uses real time or network), do not guess. Ask for clarification and insist on interface injection or moving the implementation out of kit-core.

---

## Validation

Invariants and principles are checked by a validation procedure. See **[AGENTS.validation.md](./AGENTS.validation.md)** for the exact steps and criteria.
