# Subagent offload audit

Tracks, per command prompt, where heavy inline reading happens (mutation logs,
big diffs, many files) and whether that reading should fan out to **read-only
subagents** so the main conversation context stays lean.

**Scope.** Every prompt in `assets/prompts/*.md` except underscore partials.
The Go test in `internal/cmd/subagent_offload_audit_test.go` fails (fail-closed)
if a prompt has no `### <name>` section here — the same convention as
`docs/interaction-audit.md`.

**The boundary (non-negotiable).** Offload never moves authority: per the
`dross-agent-gate` builtin, fan-out agents are read-only — they return findings
to the main loop and never edit, commit, decide a verdict, or finalize anything.
And per the locked `offload_posture` decision, offload is a **size-gated
conditional preference, never mandatory** — a small phase stays inline; the
prompt names the gate heuristically ("when the surface is large"), not as a hard
number.

**Legend.**

- **offloads-already** — the prompt is built around subagents; nothing to do
- **offload-worthy** — heavy inline reads exist; guidance added (or flagged future)
- **inline-only** — reads are small or offloading would defeat the command's purpose

### architecture

Heavy step: whole-repo code + git-history scan for the backfill generation.
Disposition: **offload-worthy (future)** — the scan half could fan readers per
subsystem on a large repo; the doc generation itself must stay in the main loop
(it is the deliverable). Not changed this phase.

### execute

Heavy step: §1b code insight — reading every `task.files` entry plus sibling
patterns before proposing an approach. Disposition: **offload-worthy (done,
this phase)** — when the task touches many or large files, read-only subagents
return the 3-5 key observations; implementation, edits and commits stay in the
main loop. See §1b's offload paragraph.

### inbox

Reads two small JSON feeds (board issues, deferred list). Disposition:
**inline-only** — the payloads are triage-sized by construction.

### init

Interactive scaffold Q&A; reads nothing heavy. Disposition: **inline-only**.

### milestone

Reads/writes one milestone toml. Disposition: **inline-only**.

### onboard

Heavy step: repo signal scan (layout, stack, runtime detection). Disposition:
**offload-worthy (future)** — a large existing repo could fan the signal scan;
the capture conversation stays main-loop. Not changed this phase.

### options

Walks settings interactively; small reads. Disposition: **inline-only**.

### pause

Writes a handoff snapshot from state already in context. Disposition:
**inline-only** — offloading would discard exactly the context being captured.

### plan

Decomposition reads spec + project toml (small); `--panel` already fans three
lens planners plus a cold judge. Disposition: **offloads-already** (panel);
the non-panel path's reads are small.

### plan-review

Spawns a fresh subagent for the whole review — context isolation is the point.
Disposition: **offloads-already**.

### quality

Multi-pass analyzer + adversarial refute-panel over cold subagents.
Disposition: **offloads-already**.

### quick

One-shot task; code insight is bounded by the single task's files.
Disposition: **inline-only** — the size gate that justifies offload in execute
rarely triggers for a quick task; if one grows that large it belongs in a phase.

### resume

Replays handoff + re-orients on branch/diff. Disposition: **inline-only** —
the command exists to load context *into* the main loop; offloading the reads
would defeat it. Keep the diff re-orient summary-level rather than full-dump.

### review

Four-lens subagent panel over an open PR. Disposition: **offloads-already**.

### rule

Small toml edits. Disposition: **inline-only**.

### secure

Scanner passes + adversarial refute-panel over cold subagents. Disposition:
**offloads-already**.

### ship

Reads verify.toml, changes.json landmarks, ARCHITECTURE.md entries; CI-watch is
polling, not reading. Disposition: **inline-only** — artefacts are small and
the landmark merge needs main-loop judgement; the absent-doc backfill already
delegates to the architecture flow (see `### architecture`).

### spec

Reads project/milestone toml + deferred list. Disposition: **inline-only**.

### status

One CLI read plus two small JSON probes. Disposition: **inline-only**.

### techdebt

Runs the deterministic scanner and reads its newest report digest.
Disposition: **inline-only** — the CLI already condensed the heavy part.

### verify

Heavy step: §2 criterion-to-test mapping — reading tests.json (every surviving
mutant) and grepping test files per criterion. Disposition: **offload-worthy
(done, this phase)** — when the surface is large, read-only subagents do the
tests.json + test-file reading and return per-criterion candidate mappings;
classification judgement, the mutation cross-check and the verdict stay in the
main loop. See §2's offload paragraph.

### watch

Read-only heartbeat digest of small feeds. Disposition: **inline-only**.
