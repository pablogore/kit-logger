# AGENTS.validation.md — kit-logger

Mandatory pre-code validation. No code proposal may be made until this procedure is completed and the validation block is filled.

---

## Step 1 — Read Governing Docs

Before any code change or proposal:

1. **AGENTS.md** — Authority, core principles, architectural invariants, engineering discipline, AI behavior expectations.
2. **AGENTS.rules.md** — Hard invariants: pipeline scope, time/randomness handling, global state, configuration, testing, change discipline.
3. **AGENTS.skills.md** — Required skills and usage constraints for the change.

Confirm each has been read. Proceed only when the change is consistent with all three.

---

## Step 2 — Validation Questions

Answer each. A "yes" where it changes an existing contract must block the proposal or force explicit discussion — it does not mean the change is forbidden, only that it needs to be called out.

| # | Question | Answer (Y/N) | Notes |
|---|----------|--------------|-------|
| 1 | Does this change what `Config.Handler` bypasses in `New`? | | If Y: this is a breaking contract change — must be called out explicitly, not silent. |
| 2 | Does this add or change a config field's "unset" behavior? | | Should default toward emitting (not silence) unless there's a stated reason to diverge. |
| 3 | Does this touch `globalLogger`, `globalContextFieldExtractor`, or other package-level state? | | Must stay `atomic.Pointer`-based unless a measured hot-path regression justifies otherwise. |
| 4 | Does this add a new handler to the built-in pipeline? | | Must implement `Unwrap`/`UnwrapAll` so `ManagedLogger` lifecycle discovery still works. |
| 5 | Does this add a code path needing deterministic tests? | | Use the existing injection pattern (`SamplingConfig.Now`/`Rand`, `rateState.now`-style field), not a new abstraction. |
| 6 | Does this add a test double for consumers? | | Must live in `pkg/logger/kitlogtest`, never in a production package. |
| 7 | Does this imply or add partial redaction? | | Filtering is all-or-nothing (whole record dropped). Don't document or imply field-level redaction unless it's actually built. |

---

## Step 3 — Rules Check

Verify compliance. Every item must pass.

- [ ] **Pipeline scope** — If `Config.Handler` bypass behavior changed, it's explicitly flagged and documented (README + code comments).
- [ ] **Time/randomness** — No new ad-hoc clock/rand abstraction; existing injection fields used or extended in the same style.
- [ ] **Global state** — `atomic.Pointer` pattern preserved for hot-path globals; no unexplained switch to mutexes or removal of globals.
- [ ] **Configuration defaults** — New "unset" semantics documented and consistent with fail-toward-emitting unless justified otherwise.
- [ ] **Lifecycle** — New handlers implement `Unwrap`/`UnwrapAll`.
- [ ] **Test doubles** — Live only in `pkg/logger/kitlogtest`.
- [ ] **Formatting and race safety** — `gofmt -l ./pkg ./scripts` empty; `go test -race ./...` passes.

---

## Step 4 — Mandatory Validation Block

**Before any code proposal**, emit this block with all fields completed. Omission or "TBD" is not acceptable.

```markdown
## Validation

- **Governing docs read:** AGENTS.md [ ], AGENTS.rules.md [ ], AGENTS.skills.md [ ]
- **Changes Config.Handler bypass scope:** Y/N — if Y, explicitly documented in README + code
- **Adds/changes an "unset" config default:** Y/N — if Y, direction (emit vs. silence) and reason
- **Touches global state:** Y/N — if Y, pattern preserved (atomic.Pointer) and reason if not
- **New handler added:** Y/N — if Y, Unwrap/UnwrapAll implemented
- **New deterministic test need:** Y/N — if Y, injection style used
- **New test double added:** Y/N — if Y, located in pkg/logger/kitlogtest
- **Rules check:** pipeline scope [ ], time/randomness [ ], global state [ ], config defaults [ ], lifecycle [ ], test doubles [ ], fmt/race [ ]
- **Blocking issues:** none | (list)
```

If any blocking issue is listed, do not propose code. Request clarification or explicit sign-off on the contract change.

---

*This validation is mandatory for every change. Exceptions require an explicit, documented update to AGENTS.rules.md or AGENTS.md.*
