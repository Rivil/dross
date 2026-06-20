# Plan Review — 07-stack-profiles

Reviewed: 2026-06-20
Plan: 11 tasks across 3 waves

## BLOCKING
(none)

The prior BLOCKING is resolved. t-5 now lists `internal/project/project.go` in its
files (plan.toml:85) and owns a concrete round-trip contract —
`TestStackProfileFieldRoundTrips fails if the field is dropped on encode/decode`
(plan.toml:96). The `Profile` field is genuinely absent from project.Stack today
(internal/project/project.go:37–47), so t-5 creates the storage location before any
consumer reads it. t-8 now `depends_on = ["t-2","t-3","t-4","t-5"]` (plan.toml:140),
so its `[stack].profile == the matched id` assertion runs only after t-5 has added
the field. The referenced-but-unowned storage location is gone.

## FLAG
(none)

The four prior FLAGs are addressed:
- t-9 now `depends_on = ["t-3","t-5"]` (plan.toml:158) — the stack.go shared-file
  ordering against t-5 is declared.
- t-7's "no third copy" is now a source-grep assertion,
  `TestNoDuplicateExtLangMap scans the two files' source for the map literal`
  (plan.toml:133–134); the positive byte-identical form is retained
  (`TestSecurityReconDelegatesToStack`/`TestQualityReconDelegatesToStack`). The
  ext->lang map literal it must detect is real and present
  (internal/security/recon.go:11–29), so the grep target is concrete.
- The `dross stack apply` re-sync is extracted into new t-11
  (`depends_on = ["t-4","t-5"]`, plan.toml:193), shrinking t-8 to init/onboard seeding.
- t-3 remains bundled by accepted decision; not re-raised.

## NOTE
- [coverage] All five criteria still covered after the split: c-1 (t-2,t-5,t-7),
  c-2 (t-3,t-4,t-8,t-11), c-3 (t-1,t-3,t-6), c-4 (t-9,t-10), c-5 (t-1,t-2,t-3). The
  new t-11 adds a second c-2 owner alongside t-4/t-8; no criterion lost an owner.
- [wave-order] All dependency edges point strictly backward in wave order: every
  wave-3 task depends only on wave-1/wave-2 tasks, except t-10→t-9 (both wave 3,
  sequential). No backward-into-future edge introduced by the amendments.
- [shared-file] internal/cmd/stack.go is written by t-5 (creates), t-9 (loadout
  body), and t-11 (apply body). t-9 and t-11 are both wave-3 and both depend on t-5,
  but not on each other. Under dross-execute's sequential one-commit-per-task model
  this is not a race: t-5 creates the file first, and t-9/t-11 each edit it in
  separate, ordered commits. No genuine ordering gap — flagged only as confirmation.
- [shared-file] cmd/dross/main.go is touched only by t-5 (registration); no second
  writer, no contention.
- [scope] t-5 now spans 4 files (stack.go, main.go, project.go, stack_test.go). The
  project.go change is a single field plus its round-trip test — small and contained,
  not an over-squash. Acceptable granularity.
- [behavior-change] t-8's `TestInitUnsupportedLeavesRuntimeUnseeded` is consistent
  with today's init.go, which already seeds no [runtime] commands (internal/cmd/init.go:59–68);
  t-8 changes init to seed from the profile on the supported path while preserving the
  empty-on-unsupported behavior. No locked-decision conflict.
- [strength] Test contracts remain specific and pin locked decisions by name
  (TestUserDirWinsOnIDCollision → profile_home, TestGoRuntimeMatchesLocked → exact
  current [runtime] strings, TestLoadoutRendersFromLocked → agent_loadout_shape).
- [strength] t-10 still honors r-01 ("not live until 'make install'") in its
  description; execute.md exists and is the real consumer. No forbidden-action
  violation found.

## Summary
The amended plan is clean. The prior BLOCKING (the [stack].profile storage location
with no owning task) is fully resolved — t-5 owns internal/project/project.go with a
round-trip contract, and t-8 depends on t-5. All four prior FLAGs are addressed: t-9
declares the stack.go ordering, t-7 converts "no third copy" to a source-grep, and
the `dross stack apply` re-sync is split into t-11. Coverage of c-1..c-5 stays
complete, all dependency edges point backward in wave order, and the t-9/t-11 shared
stack.go is sequenced (not raced) under dross-execute's one-commit-per-task model. No
new BLOCKING, FLAG, locked-decision conflict, missing file, or vague contract
introduced. Ready to execute.
