# /dross-quality

A comprehensive, intentionally heavy, multi-pass code-quality audit. Real
analyzers are ground truth; LLM subagents are the analyst; adversarial refutation
is the whole game. The audit is **read-only** — its output is a verified
findings report plus a *proposed* remediation phase, never a direct fix.

## Design principles (read before running)

- **Calibrate-only context — not context-free.** Unlike `/dross-secure` (which is
  deliberately context-poor), quality debt is judged relative to intent: a pattern
  the project deliberately locked is not "debt". So the judgment panel **may read
  `.dross/project.toml` conventions and rules to calibrate** what is idiomatic or
  intentional — but context can only **downrank** a finding, **never suppress** it.
  A real issue stays in the ledger at lower risk; it never vanishes because a doc
  rationalizes it. **The tool sweep itself stays code-only and reads no `.dross/`
  planning artifacts** — only the panel's risk calibration may consult them.
- **Tools are ground truth; LLMs are the analyst.** Run real analyzers first
  (complexity, duplication, dead-code, error-handling lint), then reason over code
  + analyzer output. Never LLM-guess where a tool gives fact.
- **Adversarial verification is the whole game.** Linters and LLM review produce
  mountains of false positives and trivia. Every candidate finding faces
  independent skeptic agents trying to **refute** it; a finding survives only on
  **majority vote** and is otherwise **dropped**. Without this the report is noise.
- **Substantive, not cosmetic.** In scope: complexity, duplication, dead code,
  coupling/cohesion, test gaps, and risky-lint defects (unhandled errors,
  ineffectual assignments, resource leaks). **Out of scope:** pure
  format/naming/style nits — the language's own formatter and basic vet already own
  those, and a nit-flood drowns real debt.
- **Read-only. No `--fix`.** This command **never edits or commits application
  code**. It writes only its own run artifacts (gitignored) and the proposed,
  gated phase scaffold.

## 0. Pre-flight

1. Run `dross rule show` — these govern *how you write/commit*, and (calibrate-only)
   may inform risk calibration, but never let "out of scope" suppress a real finding.
2. Resolve the target path from `$ARGUMENTS` (default: repo root).
3. Create the run directory and detect available tooling:
   ```
   dross quality run .                 # creates .dross/quality/<timestamp>-<short-sha>/ + writes the manifest
   dross quality detect <path>         # languages → recommended analyzers, installed vs missing
   ```
   The run dir holds `report.md` (human) and `findings.toml` (the machine-readable
   ledger). Both are gitignored — the committed record is the remediation spec, not
   the raw per-run report.

## 1. Recon — map the maintainability surface (code only)

Map the surface from the code itself: the hot/central modules (high fan-in, high
churn), the largest functions, the deepest call graphs, the test-thin packages.
This is where blast radius comes from — a complexity finding on a core, churny path
matters far more than the same on a cold one. **This recon is code-only** — it reads
no `.dross/` planning artifacts.

## 2. Tooling sweep — detect → plan → gate → sweep

The contract is **informed, not silent, and not brittle**:

1. **Detect** — `dross quality detect` lists the languages found and, per the
   data-driven catalog, which analyzers are installed vs missing.
2. **Plan + gate** — present the plan: what will run, what's missing, and the exact
   **install instructions** for the gaps. Then **gate** with `AskUserQuestion`:
   *install the missing tools first*, or *proceed with partial coverage*. Never
   hard-refuse on a missing tool; never silently skip one.
3. **Sweep** — run the available analyzers (Go core: gocyclo, dupl, deadcode,
   errcheck, ineffassign; agnostic: scc, jscpd). Record a **tool-coverage manifest**
   in `report.md` (ran vs skipped + why) so a thin toolbelt can never read as a clean
   "all clear".

## 3. Fan-out manual audit — cold, parallel subagents

Spawn many independent subagents (`Task` tool, one block), each **cold** and owning
one dimension × one code region: complexity hot spots, duplication/clones, dead
code, coupling/cohesion, test gaps, and error-handling smells. Each returns
candidate findings with evidence (file:line + snippet) and a blast-radius note (how
central/churny the code is). They never edit code.

## 4. Adversarial verify — the refute-panel

For every candidate finding, spawn independent skeptic agents whose job is to
**refute** it (assume false-positive or trivia; demand that fixing it would
genuinely lower change-cost or bug-likelihood). A finding **survives only on
majority vote**; one that does not survive is **dropped** from the ledger. Each
surviving finding records its refutation evidence and a **contextual
maintainability-risk** — panel-judged change-cost / bug-likelihood **weighted by
blast radius**, not the nominal category. Here is where **calibrate-only** context
applies: a documented, intentional pattern may **downrank** a finding, but **never
suppress** it.

## 5. Loop-until-dry + completeness critic

Keep spawning audit rounds until **N consecutive rounds surface nothing new**. Then
run a completeness critic: *what dimension didn't we run? what claim is unverified?
what module was never read?* Whatever it finds seeds another round. Log what was
bounded or skipped — never let truncation read as full coverage.

## 6. Synthesize — report + ledger

Dedupe and rank the survivors by maintainability-risk (highest first). Write:
- `report.md` — human-readable, risk-led, each finding with its refutation evidence,
  blast-radius note, and the tool-coverage manifest.
- `findings.toml` — the machine ledger (`dross quality` writes it): every surviving
  finding by id, with contextual risk, dimension, and refutation.

## 7. Scaffold the remediation phase — propose, then ask

Turn the verified ledger into a remediation phase:
```
dross quality scaffold <run-dir>      # findings.toml → a pre-filled spec.toml
```
Each surviving finding becomes **one risk-ordered acceptance criterion**
(highest-risk first), citing its ledger id; maintainability-risk drives wave order.
This is a **proposal**: **propose-then-ask before locking** the scaffold — show the
criteria and ask for confirmation, exactly like `/dross-spec`. On approval, the
normal loop takes over (`/dross-plan → /dross-execute → /dross-verify →
/dross-ship`), fully gated and tested.

## Hard rules

- **Calibrate-only, not context-free.** The judgment panel may read `.dross/`
  conventions/rules to **calibrate** risk and **downrank** intentional patterns, but
  context **never suppresses** a real finding, and the **tool sweep reads no
  `.dross/` planning artifacts**.
- **Tools first, then reason.** Never present an LLM guess where an analyzer gives a
  fact. Unverified candidates are not findings.
- **Refute or drop.** Every finding survives only by surviving the **majority-vote**
  refute-panel. No solo-LLM findings in the report.
- **Substantive only.** No cosmetic/format/naming nits — that layer belongs to the
  language formatter and basic vet, not this audit.
- **Read-only — no `--fix`.** This command **never edits or commits application
  code**. It writes only the gitignored run artifacts and the gated phase scaffold.
- **No silent caps.** If a round, a tool, or a module was skipped, say so in the
  report — a bounded audit that reads as exhaustive is a lie.
