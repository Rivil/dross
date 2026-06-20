# /dross-secure

A comprehensive, intentionally heavy, multi-pass security audit. Real scanners are
ground truth; LLM subagents are the analyst; adversarial refutation is the whole
game. The audit is **read-only** — its output is a verified findings report plus a
*proposed* remediation phase, never a direct fix.

## Design principles (read before running)

- **Context-free by construction.** This is the one dross command that is
  deliberately context-poor. It **reads no `.dross/` planning artifacts** — no
  goals, non-goals, locked decisions, rules, specs. An attacker doesn't read your
  spec, and "we decided that's out of scope" is exactly the bias that hides real
  holes. Derive the attack surface only from code, dependency manifests, and real
  entry points. The single allowed exception is detecting language/framework so
  you test correctly.
- **Tools are ground truth; LLMs are the analyst.** Run real scanners first, then
  reason over code + scanner output. Never LLM-guess where a tool gives fact.
- **Adversarial verification is the whole game.** SAST + LLM review produce
  mountains of false positives. Every candidate finding faces independent skeptic
  agents trying to **refute** it; a finding survives only on **majority vote** and
  is otherwise **dropped**. Without this the report is noise.
- **Read-only. No `--fix`.** This command **never edits or commits application
  code**. It writes only its own run artifacts (gitignored) and the proposed,
  gated phase scaffold.

## 0. Pre-flight

1. Run `dross rule show` — but treat the rules as governing *how you write/commit*,
   not as audit scope. The audit scope itself is **context-free**: do not narrow
   it by anything in `.dross/`.
2. Resolve the target path from `$ARGUMENTS` (default: repo root).
3. Create the run directory and detect available tooling:
   ```
   dross security run --new            # creates .dross/security/<timestamp>-<short-sha>/
   dross security detect <path>        # languages → recommended scanners, installed vs missing
   ```
   The run dir holds `report.md` (human) and `findings.toml` (the machine-readable
   ledger). Both are gitignored — raw findings must never pre-disclose on a public
   repo.

**Context boundary (MUST):** for the remainder of this audit you **read no
`.dross/` planning artifacts**. Code, manifests, git history, and entry points are
your only inputs.

## 1. Recon — enumerate the attack surface (code only)

Map the surface from the code itself: entry points, every `exec` / network /
file-I/O call, deserialization, trust boundaries, and dependency manifests. Note
the dross-specific surfaces too — command injection via shelling out to `git` /
`gh`, untrusted TOML/JSON config parsing, telemetry redaction leaks, and file
permissions on `~/.claude/dross/`.

## 2. Tooling sweep — detect → plan → gate → sweep

The contract is **informed, not silent, and not brittle**:

1. **Detect** — `dross security detect` lists the languages found and, per the
   data-driven catalog, which scanners are installed vs missing.
2. **Plan + gate** — present the plan: what will run, what's missing, and the exact
   **install instructions** for the gaps. Then **gate** with `AskUserQuestion`:
   *install the missing tools first*, or *proceed with partial coverage*. Never
   hard-refuse on a missing tool; never silently skip one.
3. **Sweep** — run the available scanners (Go core: govulncheck, gosec,
   staticcheck, osv-scanner; agnostic: gitleaks, semgrep, trivy). Record a
   **tool-coverage manifest** in `report.md` (ran vs skipped + why) so a thin
   toolbelt can never read as a clean "all clear".

## 3. Fan-out manual audit — cold, parallel subagents

Spawn many independent subagents (`Task` tool, one block), each **cold** and owning
one dimension × one code region: injection (cmd / SQL / path / template / SSRF),
authn/authz, input validation & deserialization, secrets/credential handling,
crypto, info-disclosure, race/TOCTOU, file permissions, and CI/CD supply-chain.
Each returns candidate findings with evidence (file:line + snippet). They never
edit code.

## 4. Adversarial verify — the refute-panel

For every candidate finding, spawn independent skeptic agents whose job is to
**refute** it (assume false-positive; demand a real, reachable exploit path).
A finding **survives only on majority vote**; one that does not survive is
**dropped** from the ledger. Each surviving finding records its refutation
evidence and a **contextual severity** — exploitability-adjusted (reachability,
required prior access), not the nominal scariness of the attack class.

## 5. Loop-until-dry + completeness critic

Keep spawning audit rounds until **N consecutive rounds surface nothing new**.
Then run a completeness critic: *what modality didn't we run? what claim is
unverified? what code region was never read?* Whatever it finds seeds another
round. Log what was bounded or skipped — never let truncation read as full
coverage.

## 6. Synthesize — report + ledger

Dedupe and severity-rank the survivors (criticals first). Write:
- `report.md` — human-readable, severity-led, each finding with its refutation
  evidence and the tool-coverage manifest.
- `findings.toml` — the machine ledger (`dross security` writes it): every
  surviving finding by id, with contextual severity and refutation.

## 7. Scaffold the remediation phase — propose, then ask

Turn the verified ledger into a remediation phase:
```
dross security scaffold <run-dir>     # findings.toml → a pre-filled spec.toml
```
Each surviving finding becomes **one severity-ordered acceptance criterion**
(criticals first), citing its ledger id; severity drives wave order. This is a
**proposal**: **propose-then-ask before locking** the scaffold — show the
criteria and ask for confirmation, exactly like `/dross-spec`. On approval, the
normal loop takes over (`/dross-plan → /dross-execute → /dross-verify →
/dross-ship`), fully gated and tested.

## Hard rules

- **Context-free.** You **read no `.dross/` planning artifacts** during the audit.
  Language/framework detection is the only allowed peek, and only for correctness.
- **Tools first, then reason.** Never present an LLM guess where a scanner gives a
  fact. Unverified candidates are not findings.
- **Refute or drop.** Every finding survives only by surviving the **majority-vote**
  refute-panel. No solo-LLM findings in the report.
- **Read-only — no `--fix`.** This command **never edits or commits application
  code**. It writes only the gitignored run artifacts and the gated phase scaffold.
- **No silent caps.** If a round, a tool, or a region was skipped, say so in the
  report — a bounded audit that reads as exhaustive is a lie.
