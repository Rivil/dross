# Phase 12 — synthesis (cold judge over risk / mvp / verification)

## Scores

| Dimension | risk (9t/3w) | mvp (3t/2w) | verification (8t/2w) |
|---|---|---|---|
| Criteria coverage | All 5 owned; every criterion has an edit task AND a test task, explicitly closing the c-3/c-4 "doc-proxy only" gap from phase 11. | All 5 owned but c-3/c-4 ride on one mega-task (t-1) covering 4 criteria + 7 files — coverage is real but reviewability is thin. | All 5 owned, mapped to named test funcs in a coverage table; tightest criterion→test traceability of the three. |
| Test-contract specificity | High — names each test (`TestSetupPromptsWireEmitter`, anchor tests), ties each to a numbered risk (R1–R10), insists anchors key on stable strings + section-scoped counts. | Medium — names the three table tests and gestures at "per-prompt anchor checks", but the anchors are described, not committed as named funcs. | Highest — designed backward from contracts; every assertion is a named function with explicit `strings.Contains` targets, incl. a generic `TestSetupConfigNoBundledTurns` net the others lack. |
| Granularity | Strong — splits mechanical emitter wiring (one risk class) from per-prompt decision-shape rewrites (distinct risk each); isolates options + milestone as their own locked-decision owners. | Coarse — one task edits all 7 prompts across 4 criteria; cheap to run, weak as a failure-locator (a red test can't point at one task). | Strong — splits options/milestone/quick solo + a grouped task for the 4 generic prompts; maps each split to a distinct named contract; respects <5-file cap. |
| Wave correctness | Correct and the most parallel — emitter guard test (t-2) runs in wave 1 alongside the edit since it reads source, not content; rewrites in wave 2; vacuous-pass guards + audit in wave 3. | Correct but flat — t-1 then everything else; misses the chance to parallelize the emitter guard, and t-3 reading t-2's audit isn't truly chained. | Correct; cleanly gates the single test file behind all edits, and uniquely makes `make install` + observed-green its own wave-2 gate task (honors r-01 + the false-green safety rule). |

**Skeleton: `risk`.** It has the sharpest failure-mode-to-owner-to-test mapping, the best wave split (emitter guard parallel in wave 1; vacuous-pass guards isolated in wave 3 where the locked decisions' anchors are stable), and is the only draft that pre-empts the R9 "audit lies" and R10 "vacuous pass" traps that bit phase 11's verify. Grafts below pull verification's `make install` gate task and its generic no-bundled-turns net, and mvp's confirmation that the emitter-guard test need not chain behind the audit.

## Merged plan

Phase 12-retrofit-setup-commands — 10 tasks across 3 waves
(`setupPrompts = {init, onboard, options, rule, inbox, quick, milestone}`; test file `internal/cmd/interaction_setupcmds_test.go`, package `cmd`, reusing `repoRootFromTest`, `deadIncludeLine`, `interactionRefPhrase`, `promptSection`, `coreLoopAuditSection`)

### Wave 1 — emitter + contract delivery (the spine) `[risk]`

```
t-1  Wire emitter + contract phrase into all 7 prompts                        [risk+mvp+verification]
     files:    assets/prompts/{init,onboard,options,rule,inbox,quick,milestone}.md
     covers:   c-1, c-2
     contract: Add `dross interaction show` beside `dross rule show` in each
               prompt's pre-flight; add an intro line carrying the literal phrase
               "interaction playbook". rule.md and inbox.md have no "## 0.
               Pre-flight" yet — the emitter goes under a new pre-flight step, NOT
               as a dead @-include. t-2 TestSetupPromptsWireEmitter fails for the
               named prompt if the emitter is dropped or deadIncludeLine reappears;
               TestSetupPromptsReferenceContract fails if "interaction playbook"
               is missing.
     depends:  —

t-2  Add grep guards for emitter + contract (7 prompts)                       [risk]
     files:    internal/cmd/interaction_setupcmds_test.go (new)
     covers:   c-1, c-2
     contract: New setupPrompts slice. TestSetupPromptsWireEmitter (per-prompt:
               contains "dross interaction show" AND NOT deadIncludeLine) +
               TestSetupPromptsReferenceContract (per-prompt: interactionRefPhrase)
               — direct twins of the coreLoop tests. Reads source, so independent
               of t-1's content; both must land before the suite is green.
     depends:  —
```

### Wave 2 — per-prompt decision-shape rewrites (each owns its bundling/paste risk) `[risk]`

```
t-3  Rewrite init + onboard identity walks one-decision-per-turn             [risk]
     files:    assets/prompts/init.md, assets/prompts/onboard.md
     covers:   c-3, c-4
     contract: init §1 vision and onboard §1 identity become per-field
               AskUserQuestion turns (name → description → core value → audience →
               non-goals), not one "Ask: A,B,C,D" block; stack/runtime/remote each
               stay one decision per turn; project.toml confirmed by one-line
               summary, never pasted. t-8 init/onboard anchors fail on recombined
               turns (R4) or a "paste project.toml back" directive (R8).
     depends:  —

t-4  Rewrite options section-pick gate + per-setting walk                     [risk]
     files:    assets/prompts/options.md
     covers:   c-3, c-4
     contract: Implements locked options_walk — a FIRST AskUserQuestion gate picks
               section(s) ("project / defaults / rules / profile / env"), THEN the
               Keep·Change·Skip turn runs one setting at a time within each chosen
               section. §12 wrap prints a changed/skipped summary, never the full
               project.toml. t-8 options anchor asserts the gate turn is distinct
               from the per-setting turn — fails on a mega-form over all settings
               (R5) or a config paste (R8).
     depends:  —

t-5  Rewrite milestone scoping walk (criteria/non-goals/phases each a segment)[risk]
     files:    assets/prompts/milestone.md
     covers:   c-3, c-4
     contract: Implements locked milestone_walk — §3 success criteria one per turn
               (accept/reword/drop), §4 non-goals its own segment, §5 phase order
               its own confirm/revise turn; §7 confirms via a count line, never a
               toml dump. t-8 milestone anchor asserts per-criterion walk + count-
               line summary — fails if §3 reverts to one accept-the-set turn (R6)
               or pastes the toml (R8).
     depends:  —

t-6  Mirror execute's gates into quick; walk inbox per-issue                  [risk+verification]
     files:    assets/prompts/quick.md, assets/prompts/inbox.md
     covers:   c-3, c-4
     contract: quick §2 approval + §4 red-path reuse execute.md's verbatim
               propose-and-react shape (proceed/steer/show/abort; fix/abort) per
               locked quick_inbox_mirror; inbox §2 triage stays one AskUserQuestion
               per issue, never bundling issues. t-8 quick anchor asserts §2 tokens
               match execute's (proceed/steer/abort, no merged gate); inbox anchor
               asserts the per-issue turn survives — fails on a divergent quick
               gate (R7) or batched inbox issues (R4).
     depends:  —

t-7  Rewrite rule.md interactive turns one-decision-per-turn                  [verification]
     files:    assets/prompts/rule.md
     covers:   c-3, c-4
     contract: rule's action-select (add/list/remove/promote) is one turn; rule
               text + severity are separate proposal turns, not one bundled form;
               a written rule is confirmed by a one-line summary, never a pasted
               rules block. t-8 generic TestSetupNoBundledTurns / no-dump arm
               covers rule.md.
     depends:  —
```

### Wave 3 — vacuous-pass guards + audit truth (R9, R10) `[risk]`

```
t-8  Add per-prompt decision-shape anchor tests + generic no-bundle net      [risk+verification]
     files:    internal/cmd/interaction_setupcmds_test.go
     covers:   c-3, c-4
     contract: Extend the wave-1 file (reuse promptSection). Per-prompt anchors,
               each keyed on a STABLE string and counting AskUserQuestion WITHIN a
               promptSection slice (never whole-file, R10): options gate distinct
               from per-setting turn; milestone §3 per-criterion; init/onboard
               fields separate; quick tokens == execute's. PLUS verification's
               generic TestSetupNoBundledTurns (every flagged decision section
               holds exactly one AskUserQuestion) and TestSetupNoArtifactDump (no
               "paste <artifact> back" directive, c-4). Each assertion names
               prompt+section so a regression points at one owner.
     depends:  t-3, t-4, t-5, t-6, t-7

t-9  Flip the 7 audit rows to ✅ and reconcile decision points               [risk+mvp+verification]
     files:    docs/interaction-audit.md
     covers:   c-5
     contract: Under "## Setup & config (phase 12 — retrofit-setup-commands)",
               every 🟡/⬜ marker across the 7 commands' rows becomes ✅, and each
               row's decision-point list is reconciled to the rewritten prompt
               (milestone gains the per-criterion row; options shows the section-
               pick gate row). Honestly ✅ only after the rewrites land (R9).
     depends:  t-3, t-4, t-5, t-6, t-7

t-10 Add audit-conformance guard + run make install / suite green            [risk+verification]
     files:    internal/cmd/interaction_setupcmds_test.go
     covers:   c-5  (+ r-01 gate over all criteria)
     contract: TestSetupAuditSectionsConform mirrors TestCoreLoopAuditSectionsConform
               (coreLoopAuditSection slicing) over setupPrompts: each `### dross-<name>`
               under the phase-12 heading contains ✅ and none of ⬜/🟡/❌. Then,
               per RULE r-01, run `make install` followed by `go test -count=1 ./...`
               and OBSERVE it green before marking done — the test edit is inert
               against the live command until re-linked. Leave one row 🟡 → the
               named subtest fails.
     depends:  t-9
```

**Coverage:** c-1 → t-1/t-2 · c-2 → t-1/t-2 · c-3 → t-3,t-4,t-5,t-6,t-7/t-8 · c-4 → t-3,t-4,t-5,t-6,t-7/t-8 · c-5 → t-9/t-10. All 5 owned; every criterion has an edit task and a test task.

## Disagreements

1. **Per-prompt vs grouped prompt-edit granularity (3 tasks vs 9).**
   - mvp: one task edits all 7 prompts ("ceremony with no reviewability gain; the per-name test loop already pins each prompt").
   - risk: split mechanical emitter wiring (one risk class) from per-prompt decision-shape rewrites (R4–R8), isolating options + milestone as solo locked-decision owners.
   - verification: split too, but groups init/onboard/rule/inbox into one task while soloing options/milestone/quick.
   - **Default taken: risk's split, refined by verification — but rule.md is pulled out of the grouped task into its own t-7** rather than folded with init/onboard (risk grouped init+onboard and left rule unaddressed as a decision-shape task; verification grouped four including rule). I split rule solo because it carries the locked-decision-free but still-bundling-prone action-select + text/severity turns and deserves an owner, matching risk's "one risk per owner" principle.
   - **Why it matters:** with mvp's single task a red anchor test can't name which rewrite broke; the split makes every failure point at one task. Cost: 10 tasks vs 3, more commits/`make install` churn.

2. **Where the test contract sits — one file authored late vs guard-in-wave-1.**
   - verification: ALL tests in one file (t-7) gated behind every wave-1 edit.
   - risk: emitter/contract guard (t-2) in wave 1 parallel to the edit (reads source, not content), with anchor + audit guards split into later waves where their anchors are stable.
   - mvp: one test task in wave 2, parallel to the audit task, not chained.
   - **Default taken: risk's staged tests** — t-2 (emitter guard) wave 1, t-8 (anchors) wave 3 after the rewrites, t-10 (audit guard) after the flip.
   - **Why it matters:** the emitter guard catches t-1 getting it wrong with maximum parallelism; deferring the anchor tests until the rewrites exist avoids keying on strings that don't yet exist (R10). verification's single-file-late model is simpler but serializes the whole test surface behind every edit.

3. **Is `make install` + observed-green its own task?**
   - verification: YES — explicit t-8 gate task, citing RULE r-01 (hard) + the false-green safety rule that a result must be observed before claiming pass.
   - mvp: NO — "a standalone make-install task is untestable and violates the no-task-without-contract rule; fold it into execution discipline."
   - risk: NO standalone task, but states the execute step runs `make install` before any live invocation; the guard tests deliberately read source so they gate without a live run.
   - **Default taken: a hybrid — fold the gate into t-10** (the audit-conformance test task) rather than a contract-less standalone task. t-10 already must run after the last edit; appending "run `make install`, then `go test ./...`, observe green" gives the gate a real deliverable (the audit guard) while satisfying r-01 and the observed-green rule.
   - **Why it matters:** mvp is right that a bare "run make install" task has no contract; verification is right that the observed-green step must be an explicit, gated act, not a buried note. Attaching it to t-10 honors both — but note risk's point that the grep/anchor tests pass against source even on a stale binary, so the live `make install` mainly de-risks the human dogfooding the retrofitted commands, not the test suite itself.

4. **Generic no-bundled-turns net — present or absent?**
   - verification: adds `TestSetupConfigNoBundledTurns` (every flagged decision section holds exactly one AskUserQuestion) ON TOP of the three specific anchor tests, to catch future bundling the named anchors miss.
   - risk: per-prompt anchors only, section-scoped counts, no separate generic net.
   - mvp: per-prompt anchor checks only.
   - **Default taken: keep both** — risk's named per-prompt anchors (R10-proof, point at one owner) PLUS verification's generic net, folded into t-8.
   - **Why it matters:** phase 11's verify found c-3 "weak (audit-doc proxy only)"; the named anchors fix that for the locked decisions, but the generic net guards the *non*-locked turns (rule, inbox) and future additions a hand-written anchor wouldn't cover. Small redundancy, meaningfully wider regression catch.
