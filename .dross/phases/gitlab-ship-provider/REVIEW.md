# Plan Review — gitlab-ship-provider

Reviewed: 2026-06-29
Plan: 7 tasks across 3 waves

## BLOCKING
(none)

## FLAG
- [test-contract] t-3's prompt-presence test (`TestShipPromptGitLabSections`) asserts the
  tokens `success, failed, canceled, manual, skipped, squash=true, should_remove_source_branch`,
  but the locked `pipeline_status_mapping` decision also mandates the *keep-polling* states
  (`running`/`pending`/`created`/`preparing`) and the *no-pipeline-for-the-SHA → ask* path.
  A prompt that silently dropped the polling states or the absent-pipeline handling would
  still pass this test. The test pins roughly half the locked mapping.
  Suggestion: extend the token list to also require at least one polling-state term and a
  no-pipeline/ask marker, so the full locked mapping is enforced.

## NOTE
- [granularity] t-5 ("Implement GitLab forge backend + enable") is the heaviest task: a whole
  second forge backend (project-ref + auth scheme + milestones + issue create/update/label/
  close/get/list, plus the `iid` keying that diverges from Forgejo's `number`/label-id model)
  landing alongside the `issue enable` + openBoard threading. It does not trip the explicit
  granularity thresholds (3 files, 2 layers), and the operations are cohesive, so a split would
  be artificial — but it carries the most implementation risk and the existing `Client.path()`
  / `Client.do()` are hardcoded to Forgejo semantics (`/repos/{o}/{r}`, `Authorization: token`),
  so the GitLab client cannot simply reuse them. Sequence and verify this one most carefully.
- [scope] t-4 bundles a telemetry-classifier change ("gitlab backend" substring → `provider`
  bucket) that is not traceable to any spec criterion. This is sound parity work — the classifier
  already carries `github backend`/`forgejo backend` arms (internal/telemetry/telemetry.go:269-271)
  — so it is correct, just untracked by a criterion. No action needed; recorded for traceability.
- [coverage] All six criteria (c-1..c-6) appear in at least one task's `covers`. c-1 is split
  cleanly (config/detect in t-1, doctor in t-4); c-2/c-3 split between the backend tasks (t-2/t-6)
  and the state-recording wiring (t-7). No gaps.
- [strengths] Test contracts are unusually specific and falsifiable — they name exact endpoints
  (`POST /api/v4/projects/me%2Fp/merge_requests`), the `source_branch`/`target_branch` (not
  head/base) distinction, the `123` numeric-project-id-wins case, and the PRIVATE-TOKEN-vs-Bearer
  mutual exclusion. This is the right altitude for a contract.
- [strengths] The plan explicitly mirrors existing Forgejo behavior as the reference
  (`TestOpenForgejoPRReviewerFailureNonFatal`, the `Draft:` title-prefix convention, the
  provider-dispatch shape), which both lowers risk and keeps the new path consistent.
- [strengths] Wave structure is sound: wave 1 packs three genuinely independent tasks
  (t-1 config, t-2 open MR, t-3 prompt) with no file or symbol overlap; every wave-2/3
  `depends_on` is a real symbol-level dependency (t-6 reuses t-2's `gitlabAuthHeader`/
  `gitlabProjectRef`; t-4/t-5 need t-1's `Remote.AuthScheme`/`ProjectID`; t-7 needs both
  opts structs). No task is serialized without cause; r-01 (prompt edits need `make install`)
  is explicitly acknowledged in t-3.

## Summary
A strong, well-sequenced plan with full criterion coverage and no locked-decision conflicts —
the only actionable item is tightening t-3's prompt-presence test so it pins the entire locked
pipeline-status mapping rather than half of it.
