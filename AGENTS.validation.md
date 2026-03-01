# AGENTS.validation.md — kit-core

Mandatory pre-code validation. No code proposal may be made until this procedure is completed and the validation block is filled.

---

## Step 1 — Read Governing Docs

Before any code change or proposal:

1. **AGENTS.md** — Authority, core principles, architectural invariants, engineering discipline, AI behavior expectations.
2. **AGENTS.rules.md** — Hard invariants: hexagonal boundaries, determinism, global state, config, UT-CORE-1, documentation, change discipline.
3. **AGENTS.skills.md** — If present: required skills and usage constraints for the change.

Confirm each has been read. Proceed only when the change is consistent with all three.

---

## Step 2 — Validation Questions

Answer each. A "yes" where forbidden or a "no" where required must block the proposal or force a redesign.

| # | Question | Answer (Y/N) | Notes |
|---|----------|--------------|-------|
| 1 | What layer is affected? (domain / application / adapter) | | Must be explicit. Domain and application have stricter rules. |
| 2 | Does this introduce I/O? | | If Y in domain/application: forbidden. Only in adapters. |
| 3 | Does this introduce time or randomness? | | If Y: must be via injected interfaces (Clock, ID, etc.). No direct use. |
| 4 | Does this introduce global state? | | If Y: forbidden. No package-level mutable vars, singletons, registries. |
| 5 | Is config injected? | | Config must be passed in; no `os.Getenv` in domain/application. |
| 6 | Is determinism preserved? | | Same inputs + injected deps ⇒ same outputs. No implicit time/rand. |

---

## Step 3 — Rules Check

Verify compliance. Every item must pass.

- [ ] **Hexagonal boundaries** — Domain does not import adapters. No I/O in domain. No HTTP/gRPC/DB implementations in domain.
- [ ] **Determinism** — No `time.Now()`, no `math/rand`/`crypto/rand` in domain/application unless via injected interface. Clock and ID injected.
- [ ] **No global state** — No mutable package-level variables, no `sync.Once` singletons, no global registries.
- [ ] **No `os.Getenv`** — No environment reads in domain or application. Config is explicit and injected.
- [ ] **UT-CORE-1** — Unit tests are deterministic: no real time, no rand, no network, no real DB, no env reads. Use fakes/mocks.
- [ ] **Documentation** — Package has `doc.go` (or package comment). Every exported symbol has GoDoc. Invariants stated where enforced.

---

## Step 4 — Mandatory Validation Block

**Before any code proposal**, emit this block with all fields completed. Omission or "TBD" is not acceptable.

```markdown
## Validation

- **Governing docs read:** AGENTS.md [ ], AGENTS.rules.md [ ], AGENTS.skills.md [ ] (if present)
- **Layer:** domain | application | adapter
- **Introduces I/O:** Y/N — if Y, layer = adapter only
- **Introduces time/randomness:** Y/N — if Y, via injected interface only
- **Introduces global state:** N required
- **Config:** injected / N/A
- **Determinism:** preserved Y/N
- **Rules check:** hexagonal [ ], determinism [ ], no global state [ ], no os.Getenv [ ], UT-CORE-1 [ ], documentation [ ]
- **Blocking issues:** none | (list)
```

If any blocking issue is listed, do not propose code. Request clarification or redesign.

---

*This validation is mandatory for every change. Exceptions require an explicit, documented update to AGENTS.rules.md or AGENTS.md.*
