# Live proof — branch protection on `Rivil/dross`

Evidence for **c-1, c-2, c-3, c-4, c-7, c-9, c-10** observed against the live
repository, and an honest account of **c-6 and c-8**, which can't be observed
until v1.7 is on `main`. Everything below was run on **2026-10-01** (final
readings at 18:16Z) by the owner's account, an admin of the repo.

## Binary

Per rule r-01 the run began with `make install`. The live apply surfaced one
gap (below), fixed as t-17 and re-installed, so the final state was produced by:

```
$ dross version
dross 0.1.0.0 (commit c439294, built 2026-10-01T18:05:47Z)
```

| binary | commit | what it produced |
| --- | --- | --- |
| first | `c4358f6` | the first `--apply`: both rulesets created |
| final | `c439294` | the re-apply (t-17's fix): both rulesets updated in place |

## Reconcile while unprotected

Before applying, `main` was level with origin and local `milestone/v1.7` was
one commit ahead: `404cb4c chore(dross): board sync after completing
watch-bot-prs`, touching only `.dross/board.json`. With the owner's approval it
was pushed as a plain fast-forward (`04a8e5c..404cb4c`), so nothing sat
stranded behind the new rulesets.

## Preview, then apply

`dross protect` (preview) named, from origin/main's `pull_request` workflows,
the required checks `mutation-ts`, `shellcheck`, `test`, and listed
`goreleaser-check` as **not on main yet** — it lives in `milestone/v1.7`'s
`ci.yml` only (follow-up 1). `allow_auto_merge` and `allow_merge_commit` were
already on, so no warning.

After the owner approved, `dross protect --apply`:

```
applied dross: main (created #24323535)
applied dross: milestones (created #24323537)
allow_auto_merge: on
read back main: protected — every required check in place, no bypass
```

## Readback (as admin)

| ruleset | enforcement | `current_user_can_bypass` | bypass actors | include | rules |
| --- | --- | --- | --- | --- | --- |
| `dross: main` (#24323535) | active | **never** | 0 | `refs/heads/main` | deletion, non_fast_forward, pull_request, required_status_checks |
| `dross: milestones` (#24323537) | active | **never** | 0 | `refs/heads/milestone/*` | pull_request |

`GET rules/branches/main` lists `deletion`, `non_fast_forward`, `pull_request`
(0 approvals, `allowed_merge_methods` `[merge, squash]`) and
`required_status_checks` (not strict) whose contexts are exactly
`mutation-ts, shellcheck, test` — the job ids of origin/main's `ci.yml`, read
with a separate scan. Repo settings: `allow_auto_merge: true`,
`allow_merge_commit: true`.

- **c-1 / c-7** — main changes only through a PR, can't be force-pushed or
  deleted, and the owner can't bypass any of it: `current_user_can_bypass` is
  `never` with an empty bypass list. Main was never pushed to; the refusal is
  this readback plus the milestone probe below, where the same no-bypass rule
  refused the owner live.
- **c-2** — the required contexts equal `ci.yml`'s jobs.
- **c-4** — `merge` stays an allowed merge method, so milestone PRs still land
  as merge commits; neither ruleset touches `refs/tags/*`, so `release.yml`'s
  tag push is unaffected.

## Doctor and `--check` (c-3, c-9)

```
Branch protection:
  ✓ main is protected — changes only through a PR, all 3 pull_request job checks required, no force push, no deletion, no bypass
```

`dross protect --check main` → `protected`;
`dross protect --check milestone/v1.7` → `protected` (so t-7's path escaping of
`milestone%2Fv1.7` works against the live API); a phase branch → `unprotected`.

## Gap found live — the unattributed-changes approval (fixed as t-17)

The readback carried a `pull_request` parameter dross never sent:
`require_extra_approval_for_unattributed_changes: true`. GitHub fills it in
server-side (a 2026-08 addition), and under it a PR holding any commit not
attributed to a GitHub account needs one human approval — which a solo author
can't give, against locked decision `approvals`. Every author in use today is
attributed (the owner's linked emails, `github-actions[bot]`,
`dependabot[bot]`), so nothing was blocked in the window.

With the owner's approval it became task **t-17** (`c439294`): both rulesets
now send it explicitly as `false`. The re-apply read it back as `false` on both
rulesets.

## Idempotence

The re-apply updated both rulesets in place — `updates ruleset #24323535`,
`updates ruleset #24323537`, no create — and the repo holds exactly two
rulesets named `dross: *`. Read-back clean again.

## c-10 probe — `milestone/zz-protect-probe`

A throwaway ref, approved by the owner. Nothing else was touched.

| step | push | result |
| --- | --- | --- |
| create | `b3df5da` → `refs/heads/milestone/zz-protect-probe` | **accepted** (`* [new branch]`) — `milestone create` keeps working |
| update | `80684ef` (a `commit-tree` child, no local branch moved) | **refused**: `GH013: Repository rule violations found … Changes must be made through a pull request` — the ref stayed at `b3df5da` |
| delete | `--delete milestone/zz-protect-probe` | **accepted** (`- [deleted]`) — `--finalize` keeps working |

## Not yet observable — recorded, not passed

- **First chore PR into `milestone/v1.7`** (t-6's direct-merge fallback): no
  `.dross` chore was ahead of a protected milestone during this run (the one
  that was got pushed while unprotected, above). The first will be this
  phase's own completion record at `dross phase complete`, after its PR merges
  into `milestone/v1.7`. Whether it merges hands-free is recorded then.
- **c-6, Dependabot half**: no Dependabot PR was open, so there was no head
  commit to read contexts from. `ci.yml` runs on `pull_request` with no
  secrets (TestCIReadsNoSecrets), which is what Dependabot PRs get. Unobserved.
- **c-6, pin-currency half, and c-8**: `pin-currency.yml` (schedule) and
  `dependabot-automerge.yml` (`pull_request_target`) both run **main's** copy
  of the workflow, and main has neither the auto-merge workflow nor the
  dispatch wiring this phase relies on. **Unobservable until v1.7 is on
  main** — never a pass.

## Follow-ups

1. **Re-run `dross protect --apply` after v1.7 merges into main.** Only then is
   `goreleaser-check` in origin/main's `ci.yml`, and protect reads required
   checks from origin/main by design. Until then the ruleset requires three
   checks, not four, and doctor will name `goreleaser-check` as missing.
2. **Watch the first Dependabot `github-actions` PR that edits a workflow.**
   A `GITHUB_TOKEN`-armed auto-merge may be refused for a workflow change
   (the token lacks `workflows` permission) — record whether it lands or needs
   a hand merge.
3. **Merges done by a `GITHUB_TOKEN` auto-merge start no workflow run**, so
   `ci.yml`'s push-to-main run and `release.yml` won't fire on a Dependabot
   merge. Harmless (a bot bump changes no version; security bumps are released
   by hand per `dependabot.yml`), but observe it rather than assume it.
4. **First milestone chore PR**: record whether it merged hands-free (above).
