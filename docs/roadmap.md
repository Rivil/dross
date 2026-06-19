# Dross — consolidated roadmap

> Planning artifact. Captures the workstreams agreed on 2026-06-19 from a
> telemetry pressure-point review plus a design session on interaction quality,
> a codebase comprehension layer, parallel agents, and a new security command.
> Sequenced and prioritized; not yet broken into phases.

## Unifying theme

Almost everything here points one direction: **dross should treat agents and the
human as collaborators with a durable shared understanding, not a one-shot
proposer.** The CLI-ergonomics fixes remove friction, the comprehension layer
builds shared memory, and the parallel-agent and security work are the same
fan-out muscle pointed at different jobs.

---

## Workstream A — Agent-ergonomics quick wins (from the logs)

Small Go CLI changes, high daily value, independent of everything else. Source:
telemetry review of `~/.claude/dross/telemetry.jsonl` (8.6k events, May 4 – Jun 19).
The `err_detail` fix (`d37d344`) is working — fresh `other`-bucket events now name
the exact failure, and they cluster tightly.

| ID | Change | Evidence | Touch points |
|----|--------|----------|--------------|
| A1 | `milestone get/set/add`: default `<version>` to current milestone | Dominant fresh signal — agents omit the version positional or invent `--milestone`. Inconsistent with `show [version]`, which is optional. | `internal/cmd/milestone.go` |
| A2 | bare `dross task` → default action (`status` or `next`) | 17 `unknown_subcommand` on the hottest path in the whole log (`task status/next/show` ≈ 2,040 ok calls) | `internal/cmd/task.go` |
| A3 | `phase complete` friction: clearer at-failure guidance; reconsider strictness | All `dirty_tree` (39) + `merge_pending` (28) errors funnel through this one command | `internal/cmd/phase.go` (needs a look before scoping) |
| A4 | Telemetry: record the rejected token on `unknown_subcommand` / `unknown_field` | Same mechanism as the `err_detail` fix; graduates these out of opaque buckets | `internal/telemetry`, command error paths |

Run as `dross-quick` one-offs — they don't need phase ceremony.

### Prompt-hygiene fixes (surfaced by dogfooding, 2026-06-19)

Found while running the first `dross-quick` on dross itself:

- **A5 — `Co-Authored-By` contradiction.** `quick.md`, `execute.md`, and
  `plan.md` all say *"Do not add Co-Authored-By trailers unless the user asked"*,
  but the dross repo uses the trailer on every commit (and the harness instructs
  it). dross's own prompts disagree with how dross is developed. Reconcile —
  either flip the prompt guidance to "follow `repo.commit_convention` / repo
  history" or capture a project rule. *(prompt files + possibly a project rule)*
- **A6 — `quick.md` leaves `.dross/` dirty.** §6 bumps the version and touches
  state but never says to commit `state.json`, while the builtin hygiene rule
  forbids leaving `.dross/` dirty across a command boundary. A clean quick run
  therefore always ends dirty and forces an extra `chore(dross):` commit. Fold
  the state bump into the work commit (or instruct an explicit follow-up).
  *(`quick.md`; check `execute.md` / others for the same gap)*

---

## Workstream B — Comprehension layer: `ARCHITECTURE.md`

**The flagship of the design session.** A single living document at repo root,
organized by **functionality** (never by phase), that lets the human — and the
next agent — know *what the system does and where it lives* without
reverse-engineering diffs later.

### Shape of the document

- **One file, repo root.** `ARCHITECTURE.md`.
- **Feature-organized.** A capability like "tag management" is one entry even if
  phase 3 created it, phase 7 extended it, phase 11 refactored it. Phases never
  appear as structure — only as provenance.
- Each feature entry carries:
  - a **one-line description** (what it does — this is why it's "architecture",
    not a bare link "map"),
  - **symbol-level links** to where it lives (`utils/tags.ts:normalizeName()`),
  - a **provenance** breadcrumb (introduced 03, limit added 07, + commit links).
- **Dense, not waffly.** This is the hard editorial constraint and it cuts against
  what every AI model defaults to. The doc is a *reference*: **complete coverage**
  (every piece of functionality has an entry) at **minimum verbosity** (one-line
  descriptions, no narrative paragraphs, no restating what the code obviously does,
  no padding). Coverage is the goal; word-count is the enemy. The merge agent must
  be instructed explicitly against prose — terse entries, hard line budget per
  feature.
- It doubles as the **reuse / dedup index**: execute reads it *before* proposing,
  which is what stops it duplicating code.

### Triggers (one agent, four entry points)

| ID | Trigger | Behaviour |
|----|---------|-----------|
| B0a | `dross-init` (greenfield) | Seed a skeleton `ARCHITECTURE.md` the loop fills in over time |
| B0b | `dross-onboard` (existing repo) | **Backfill** — scan the whole tree + `git log`, produce the initial feature map. Same merge-agent machinery as B2, pointed at the repo instead of one phase's changes. |
| B1 | `dross-execute` (per task) | Live comprehension beat in chat (locations / entry points), and feed a one-line landmark into the **existing** `dross changes record --notes` (append-only, non-blocking, conflict-free) |
| B2 | `dross-ship` (per phase) | A focused background agent reads `changes.json` + diff + current `ARCHITECTURE.md`, merges **by feature** with provenance, commits onto `phase/<id>` so it lands in the PR |

### The hard problem

Keeping a feature-indexed doc coherent as it accretes. When a task touches
existing functionality the merge agent must **find the right entry and update in
place**, not append a new section — otherwise it drifts back into a phase-shaped
pile and the dedup value evaporates. The prompt has to enforce: *read → locate
affected feature → update in place → only add a new section for genuinely new
capability.*

### Why ship-time merge

Ship is the point where "the phase's reality is now final," so it's the single
serialized writer by construction — no concurrent-write hazard on the shared
file. `ARCHITECTURE.md` therefore tracks **shipped** state, not work-in-progress,
which is the correct semantic (you navigate shipped code). The merge must commit
**before** the push so it's in the PR; it naturally overlaps the dead time while
the human reviews the ship preview.

### Status of the plumbing

Already half-built: `changes.json` stores a per-task `Notes` field
(`internal/changes/changes.go:34`) and `dross changes record` already exposes a
`--notes` flag (`changes.go:59`) — but `execute.md` doesn't populate it yet.

- **B3 (deferred):** typed repeatable `--landmark` field on `TaskRecord`, only if
  free-form `--notes` proves too lossy. The ship-time agent reads actual code for
  symbol links, so notes only need to capture intent.

---

## Workstream C — Interaction quality (prompt-only)

- **C1 — spec turn-boundaries.** Kill the wall-of-text. The `spec.md` prompt
  *already* mandates a step-by-step flow (§2 criteria one at a time; §3c "one
  focused exchange at a time — don't batch"), but nothing *forces* a turn
  boundary, so the model collapses it into one mega-message hidden behind ctrl+o.
  Fix: hard `AskUserQuestion` gates per criterion and per gray-area, and **never
  display the full TOML** — agree content in plain language, write silently,
  confirm with a one-liner. Same root cause as the "shrink the observe→act loop"
  rule.
- **C2 — execute reuse scan.** Folded into B: execute reads `ARCHITECTURE.md` plus
  a real "does this already exist?" grep before proposing, so `steer` becomes a
  live choice instead of `proceed` being the only viable option.

---

## Workstream D — Parallel-agent policy (foundational)

Mostly principle + rule edits; unblocks B2, E, and F.

The gate line:

> **Agents fan out freely to gather / analyze / verify. Writing and deciding stay
> gated in pair mode (or `--solo`).**

Generalize the `--panel` pattern (already trusted in `plan.md` §2P) as the
sanctioned shape for fan-out, and relax the current blanket "no subagents in
default mode" hard rule to this nuanced one. Stack agents (E) *may* write under
pair mode with per-edit gating — the gate's product is comprehension, not just
consent.

---

## Workstream E — Stack profiles

Agents-as-**loadout**, not flavour personas. Telling a model "you are a Svelte
expert" adds no knowledge and risks overconfidence; what helps is the *bundle*:
the right MCP tools (autofixers, context7), project conventions, locked stack
decisions, and guardrails ("verify API signatures before guessing"). Generate an
agent definition from `stack.locked`. Lowest priority — lands after D + B.

---

## Workstream F — `dross-secure` (new command)

A comprehensive, intentionally heavy, multi-pass security audit. Name locked:
**`dross-secure`** (family-consistent with `verify` / `ship`).

### Design principles

- **Context-free by construction.** Every other dross command is context-rich;
  this one is the deliberate inverse. It reads **no `.dross/` planning artifacts**
  (no goals, non-goals, locked decisions, rules). An attacker doesn't read your
  spec, and "we decided that's out of scope" is exactly the bias that hides real
  holes. It derives the attack surface only from code, dependency manifests, and
  actual entry points. (One unavoidable exception: detect language/framework to
  test correctly.)
- **Tools are ground truth; LLMs are the analyst.** Run real scanners first, then
  reason over code + scanner output — never LLM-guess where a tool gives fact.
- **Adversarial verification is the whole game.** SAST + LLM security review
  produce mountains of false positives. Every candidate finding gets independent
  skeptic agents trying to **refute** it (majority-vote to survive). Without this
  the report is noise.
- **Read-only.** No `--fix`. Output feeds the normal loop (see below).

### Harness shape (heavy, multi-pass, parallel)

1. **Recon** — enumerate attack surface from code only: entry points, every
   `exec` / network / file-I/O call, trust boundaries, dependency manifests.
2. **Tooling sweep (deterministic)** — language-appropriate scanners. For this Go
   repo: `govulncheck`, `gosec`, `staticcheck`, `gitleaks` / `trufflehog` (incl.
   git history), `osv-scanner` / `grype` / `trivy`, `semgrep`, optionally CodeQL.
3. **Fan-out manual audit** — many parallel subagents, each cold/independent,
   owning one dimension × one code region: injection (cmd / SQL / path / template
   / SSRF), authn/authz, input validation & deserialization, secrets/cred
   handling, crypto, info-disclosure, race/TOCTOU, file permissions, and **CI/CD
   supply-chain** (reuse `~/.claude/memory/reference_ci_supply_chain_hardening.md`).
4. **Adversarial verify** — refute-panel per finding; drop those that don't
   survive.
5. **Loop-until-dry / multi-pass** — keep spawning rounds until N consecutive
   rounds surface nothing new, plus a completeness critic ("what modality didn't
   we run?").
6. **Synthesize** — dedupe, severity-rank (CVSS-style + exploitability),
   prioritized report → `.dross/security/<run>/report.md` (durable audit trail).

### Output: a remediation phase, not fixes

`dross-secure` then **scaffolds a new dross phase** from the *verified* findings:

- Each surviving finding → an acceptance criterion in a pre-filled `spec.toml`
  ("this attack class is now blocked and a test proves it" — naturally testable,
  which is exactly dross's criterion philosophy).
- **Severity drives wave/priority order** (criticals first).
- Each criterion links back to its finding + the refutation evidence.
- It *proposes* the phase scaffold and asks before locking, same as `/dross-spec`.
- From there it's the normal loop: `/dross-plan → /dross-execute → /dross-verify
  → /dross-ship`. Fully gated, tested, reviewed.

This cleanly resolves the gate question: the audit is unlimited cold fan-out (no
writes, no gate needed), and remediation inherits all of pair-mode's gating.

### dross-specific surfaces it should catch

Command injection via shelling out to `git` / `gh`; untrusted TOML/JSON config
parsing; telemetry redaction leaks (the `err_detail` path now carries messages —
does it leak filesystem paths?); file permissions on `~/.claude/dross/`.

### Implementation

Prompt-first — `assets/prompts/secure.md` + `assets/commands/dross-secure.md`,
orchestrating subagents via the Task tool the way `--panel` does. Optional later
CLI (`dross security ...`) for findings tracking/state. This is the natural home
for heavy multi-agent orchestration — running it intentionally spawns many agents
and burns real tokens.

---

## Dogfooding dross on dross

**Decision: yes, selectively.** It's the ultimate dogfood — the log pressure
points get exercised on real non-trivial work, B0b backfill has a perfect first
subject (dross's own codebase), and the heavy changes get phase-branches, atomic
commits, and verify gates for free. It also makes the repo a living "dross uses
dross" example.

- **Onboard dross** (`dross-onboard`) → creates `.dross/`, captures Go runtime
  commands, imports rules.
- **Substantial workstreams (B, F, E)** → run through the full phase loop.
- **Small CLI ergonomics (A)** → `dross-quick` one-offs.

**Caveat — the bootstrapping subtlety:** we're editing the very prompts we run.
Prompt changes take effect on the *next* skill invocation, so it's manageable, but
we have to be conscious — we don't get to use B2's ship-merge until after B2
ships. Onboarding also commits `.dross/` planning artifacts into the dross repo
itself (with the `.gitattributes linguist-generated` collapsing that `dross
doctor` checks for) — a deliberate, visible choice.

---

## Recommended sequence

1. **Onboard dross** — sets up `.dross/`, runtime, rules (prerequisite for
   dogfooding).
2. **A** — CLI ergonomics via `dross-quick`. Cheap, immediate relief.
3. **C1** — spec turn-boundaries. Prompt-only daily-pain fix.
4. **D** — agent gate policy. Unblocks B2 / E / F.
5. **B** — `ARCHITECTURE.md` (B0 seed/backfill → B1 → B2); then run B0b on dross
   itself.
6. **F** — `dross-secure`. Heavy; rides on D.
7. **E** — stack profiles. Last.

## Decisions locked 2026-06-19

- `ARCHITECTURE.md`: single repo-root, feature-organized, symbol-level links,
  provenance kept. **Dense reference — complete functionality coverage, minimum
  verbosity, no AI waffle.** Created at `dross-init` (seed) and `dross-onboard`
  (backfill); kept current by execute landmark capture + ship-time merge.
- `dross-secure` is read-only and emits a remediation **phase**, not fixes. Audit
  is context-free; remediation runs the normal loop.
- Parallel agents: gather/analyze/verify freely; writing stays gated (or solo).
- Dogfood dross on dross, selectively (full loop for B/F/E, `dross-quick` for A).
