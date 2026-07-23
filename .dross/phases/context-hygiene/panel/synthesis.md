# context-hygiene — panel synthesis

Merged from three independent drafts: **risk** (7 tasks / stated 3 waves), **mvp**
(6 / 2), **verification** (8 / 2). I authored none; this picks a skeleton, grafts
concrete improvements, and records where the lenses genuinely diverge.

## Scores

| Dimension | risk | mvp | verification |
|---|---|---|---|
| Criteria coverage | **A** — 5/5, explicit map | **A** — 5/5, tidy table | **A** — 5/5, per-test map |
| Test-contract specificity | **A−** — adversarial (idempotent / foreign-survives / rejects-garbage) | **B** — present but light; no no-prompt / last-line asserts | **A** — sharpest: no-prompt, idempotent-merge, LAST-line, exempt-removal |
| Granularity | **A** — 7 tasks, one failure mode each; helper isolated | **B** — 6; scaffold t-6 is a mega-task (helper + wiring + init + onboard) | **B+** — 8; execute-footer split adds a task/edge to dodge a race ordering already prevents |
| Wave correctness | **B** — body is 2 waves but header says "3 waves" (defect) | **A** — clean 2 waves | **A** — clean 2 waves; best dep reasoning (t-8 stays wave 2 because verb strings are constants) |

One-liner per draft:
- **risk** — sharpest failure-mode isolation and granularity; only blemish is the header/body wave-count mismatch and contracts a notch below verification's.
- **mvp** — leanest and structurally clean, but collapses the highest-risk clobber surface (hook merge) into a broad scaffold task, weakening isolation.
- **verification** — best contracts and dependency reasoning; slightly over-cut (splits execute's footer into its own task purely to avoid a same-file race that wave-ordering already removes).

**Skeleton: `risk`.** It has the best granularity-vs-isolation balance and a complete
criteria map; its two real weaknesses (wave-count defect, contracts a notch soft) are
cheap grafts from mvp (clean 2-wave frame) and verification (sharper named tests).

## Merged plan

7 tasks across 2 waves. Format: `t-N  title  [origin lens(es)]` / files / covers / contract.

### Wave 1

**t-1  Hook-merge helper for settings.json**  `[risk + verification]`
- files: `internal/hooks/settings.go`, `internal/hooks/settings_test.go`
- covers: c-3, c-5
- contract: installing the PreCompact hook into settings.json twice yields byte-identical output (idempotent); a pre-existing **foreign** PreCompact **or SessionStart** entry survives untouched, unreordered, with sibling keys intact `[graft: verification TestMergeHookPreservesOthers]`; malformed JSON returns an error and writes nothing (no partial clobber). `TestMergeHookIdempotent / TestMergeHookPreservesForeign / TestMergeHookRejectsGarbage`.

**t-2  `dross pause --auto` snapshot (section-scoped)**  `[risk + mvp + verification]`
- files: `internal/cmd/pause.go`, `internal/cmd/pause_test.go`, `internal/cmd/root.go`
- covers: c-3
- desc: new non-interactive `dross pause --auto` writing/merging ONLY a dedicated `## Auto-snapshot` block (branch, dirty files, `dross status`, injected timestamp) into `.dross/handoff.md`; never prompts; no-ops (exit 0, no write) outside a dross repo.
- contract: given a handoff.md with hand-edited `## Thread` / `## Open loops`, those sections stay byte-identical and only `## Auto-snapshot` is rewritten; a second run updates that block in place (no duplicate section, next `##` heading not consumed) `[graft: verification TestPauseAutoIdempotentMerge]`; it never reads stdin / prompts `[graft: verification TestPauseAutoNoPrompt]`; outside a dross repo exits 0 and creates no file. Clock injected for deterministic timestamp.

**t-3  Clear-point footer on 7 boundary prompts**  `[risk + mvp]`
- files: `assets/prompts/{spec,plan,execute,verify,ship,quick,pause}.md`
- covers: c-1
- desc: each prompt's wrap-up ends with the literal sentinel `state is on disk — safe to /clear` plus the exact next command for a fresh session; uniform wording so the gate matches one sentinel.
- contract: each of the seven contains the sentinel followed by a concrete `/dross-…` next command; the c-1 gate (t-6) fails naming any prompt whose sentinel is absent. `TestBoundaryPromptsCarryFooter`.

**t-4  `dross reentry` emitter + status re-entry footer**  `[risk + verification]`
- files: `internal/cmd/reentry.go`, `internal/cmd/reentry_test.go`, `internal/cmd/status.go`, `internal/cmd/status_test.go`, `internal/cmd/root.go`
- covers: c-4, c-5
- desc: a shared `reentryLine()` generator feeds both surfaces. `dross reentry` prints one "you are here + exact next command" line (reusing `suggestNext`), silent exit 0 outside a dross repo (SessionStart-safe); `dross status` ends by emitting the identical line.
- contract: `dross reentry` outside a dross repo exits 0 with empty stdout; inside, its single line names the same command `suggestNext` returns (no plan → `/dross-spec --new`; next-runnable → `/dross-execute`; verify flipped to fail → the fail hint); `dross status`'s **LAST** printed line is byte-equal to `dross reentry`'s output `[graft: verification TestStatusEndsWithReentryLine — assert on last line to catch a mid-output regression]`.

### Wave 2

**t-5  Checkpoint option in execute pair-mode gate**  `[risk + mvp + verification]`  — depends **t-4**
- files: `assets/prompts/execute.md`, `internal/cmd/execute_prompt_test.go`
- covers: c-2
- desc: extend the per-task gate to continue / stop / **checkpoint**; checkpoint confirms plan.toml + state consistency, then ends the session with `/clear → /dross-execute --from <next-task>`. Checkpoint is the recommended lead option at wave boundaries (checkpoint_posture).
- contract: the test asserts the gate enumerates a checkpoint option, that checkpoint instructs the plan.toml/state consistency check BEFORE emitting `--from <next-task>`, and that the wave-boundary branch marks checkpoint as lead/recommended; removing the checkpoint block fails the test.

**t-6  c-1 footer coverage doc + fail-closed gate**  `[risk + mvp + verification]`  — depends **t-3**
- files: `internal/cmd/footer_coverage.go`, `internal/cmd/footer_coverage_test.go`, `docs/footer-audit.md`
- covers: c-1
- desc: mirror the interaction-audit convention (footer_coverage decision) — classify every command-backed prompt as footer-bearing (boundary) or exempt-with-reason in a **dedicated** `docs/footer-audit.md`; fail-closed Go gate.
- contract: deleting the sentinel from `ship.md` fails `TestFooterCoverageFailClosed` naming `ship`; a new boundary prompt with neither footer nor `## Exempt` row fails; an exempt row that omits its reason cell fails; dropping an existing command from the Exempt list fails `[graft: verification TestClearPointExemptRemovalFails]`; a correctly-footed or correctly-exempt prompt passes.

**t-7  Scaffold PreCompact + SessionStart hooks in init/onboard**  `[risk + mvp + verification]`  — depends **t-1, t-2, t-4**
- files: `internal/cmd/hooks.go`, `internal/cmd/hooks_test.go`, `internal/cmd/init.go`, `internal/cmd/onboard.go`, `internal/cmd/init_test.go`, `internal/cmd/onboard_test.go`
- covers: c-3, c-5
- desc: an idempotent ensure wiring user-level `~/.claude/settings.json` — PreCompact → `dross pause --auto`, SessionStart → `dross reentry` — via t-1's merge helper; both init and onboard call it (hook_scope: user-level, idempotent). Hook command strings are **plan-fixed constants**, so this depends on t-4's *existence* (verb decided here), not its runtime output `[graft: verification's wave-placement reasoning — keeps t-7 in wave 2, not wave 3]`.
- contract: running init's ensure twice leaves exactly one dross PreCompact and one dross SessionStart entry (idempotent, vs a temp `CLAUDE_CONFIG_DIR`); a pre-existing foreign PreCompact hook survives; onboard's ensure produces settings.json byte-identical to init's. `TestInitEnsuresHooksIdempotent / TestOnboardEnsuresSameHooks`.

## Disagreements

**D1 — Re-entry surface: dedicated command vs status flag.**
risk & verification both add a dedicated `dross reentry` command (silent no-op outside a
repo) as the SessionStart target; mvp instead adds a `--reentry` flag on `dross status`.
*Default taken:* dedicated `dross reentry` command (2 lenses; cleaner hook target — the
hook string is `<bin> reentry` rather than a flagged subcommand). *Why it matters:* the
choice fixes the literal command baked into the SessionStart hook in t-7 and the surface
the c-5 contract asserts; changing it later touches both the emitter and the scaffold.

**D2 — Hook-merge helper: own task vs folded into scaffold; and its package name.**
risk (t-1, `internal/hooks`) and verification (t-5, `internal/claudehook`) both isolate the
settings.json merge into its own wave-1 task with its own idempotency/clobber tests; mvp
folds it into the scaffold task (t-6) inside `internal/hooks`. *Default taken:* own wave-1
task, package `internal/hooks` (2 of 3 on isolation; 2 of 3 on the name). *Why it matters:*
clobbering `~/.claude/settings.json` is the single highest-consequence failure in this
phase — isolating it gives the merge logic a directly-testable surface and keeps the
scaffold task from ballooning; the package name is where every future hook merge lands.

**D3 — Footer grouping: all 7 together vs 6 + execute split.**
risk & mvp put all seven footers in one wave-1 task; verification splits execute's footer
into the checkpoint task (its t-2) so no two wave-1 tasks edit `execute.md`. *Default
taken:* all 7 together (t-3), with checkpoint (t-5) sequenced to wave 2. *Why it matters:*
verification's split exists only to dodge a same-file race — but because checkpoint already
lands in wave 2 (after t-3), execute.md is edited sequentially across waves and no race
exists. Keeping the footers uniform in one task removes a task and a dependency edge for no
lost safety. (Trade-off surfaced: t-3 and t-5 both touch execute.md; the wave ordering, not
a task split, is what serializes them — do not promote t-5 to wave 1.)

**D4 — Coverage doc: dedicated new doc vs extend interaction-audit.md.**
risk (`docs/footer-audit.md`) and verification (`docs/clear-point-audit.md`) each create a
dedicated parallel doc; mvp extends the existing `docs/interaction-audit.md`. *Default
taken:* a dedicated new doc, `docs/footer-audit.md` (risk's name). *Why it matters:* the
footer_coverage decision says the gate *mirrors* the interaction-audit convention (same
shape, separate gate). Folding a second classification into one doc couples two independent
fail-closed gates and forces every new command through one merged table; a separate doc
keeps each gate single-purpose. Cost accepted: a genuinely new command may need enrolling in
two audits.

**D5 — Task/wave count, and risk's internal wave-count defect.**
Drafts land at 6 (mvp) / 7 (risk) / 8 (verification) tasks; risk's header claims "3 waves"
while its body lists only 2. *Default taken:* 7 tasks across 2 waves — risk's task set,
normalized to the 2-wave structure its own body uses (and that mvp/verification confirm).
*Why it matters:* the third wave is phantom — no task depends on a wave-2 output — so a
3-wave frame would serialize independent work. The merged dependency graph has depth 2:
{t-1,t-2,t-3,t-4} → {t-5⇐t-4, t-6⇐t-3, t-7⇐t-1,t-2,t-4}.
