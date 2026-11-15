# c-4 lens proof — edge-case and silent-failure lenses on a seeded and a clean diff

This is the recorded live run backing acceptance criterion **c-4** of phase
`review-edge-lenses`: `/dross-review` over a seeded fixture diff (one partial
Status switch, one swallowed error) reports both defects from the matching
lens; over a clean fixture diff neither new lens reports a finding.

The run spawns six LLM lens subagents, so it is not a `go test` gate. The
comments it produced are committed (`seeded-comment.md`, `clean-comment.md`).
`internal/prtriage/lensproof_test.go` re-reads them, splits each with
`prtriage.SplitReview`, and checks the findings' locations against the
defect lines it computes from the fixture source with go/parser.

## Fixture

A nested Go module (`go.mod`, module `example.com/lensproof`), so `./...`, vet
and gremlins never see it.

- `order/status.go` — `type Status` with `Pending`, `Paid`, `Shipped`,
  `Refunded`. Kept outside both reviewed diffs, so a lens must read the
  working tree to learn the full value set.
- `seeded/handler.go` — `Label` switches on Status without `Refunded` and has
  a silent `default: return ""`. `Archive` discards `os.Remove`'s error with
  no comment.
- `clean/handler.go` — the near-miss twin. The switch covers every value and
  its default returns an error. The same discard has an adjacent comment
  giving the reason.

## Reproduce

Pre-req (rule r-01): `make install`, so the installed prompts carry the six
lenses and `--diff` mode.

```
make install
dross --version

S=<scratch dir>
mkdir -p $S/seeded $S/clean
git diff --no-index /dev/null fixtures/review-edge-lenses-c4/seeded/handler.go > $S/seeded.patch
git diff --no-index /dev/null fixtures/review-edge-lenses-c4/clean/handler.go  > $S/clean.patch

/dross-review --diff $S/seeded.patch --out $S/seeded --phase review-edge-lenses
/dross-review --diff $S/clean.patch  --out $S/clean  --phase review-edge-lenses

cp $S/seeded/review-comment.md fixtures/review-edge-lenses-c4/seeded-comment.md
cp $S/clean/review-comment.md  fixtures/review-edge-lenses-c4/clean-comment.md
```

## Recorded run (2026-11-14, `dross 0.1.0.0 (commit 06450861, built 2026-11-14T13:09:22Z)`)

The solo main loop of `/dross-execute review-edge-lenses` ran both reviews.
It followed `review.md` Local mode and spawned the six lens templates, filled
verbatim, for each patch. Both panels were spawned in one tool block. No
verify.toml existed yet, so both new lenses graded FLAG.

### Seeded — `seeded-comment.md`

- **Edge cases:** FLAG at `seeded/handler.go:12`, the switch site, naming
  `Refunded` and the set's definition at `order/status.go:7-12`.
- **Silent failures:** FLAG at `seeded/handler.go:29`, the uncommented
  `_ = os.Remove(...)`.
- **Code quality:** no findings. It ceded the discard to silent failures (c-6).
- **Security:** no findings.
- Test efficacy and spec fidelity reported findings outside the proof's
  scope: no tests ship with the fixture, and the patch carries none of the
  phase's other criteria.

### Clean — `clean-comment.md`

- **Edge cases:** no findings. The switch is exhaustive and its default is
  loud.
- **Silent failures:** no findings. The discard's reason comment exempts it.
- **Code quality** and **security:** no findings.
- Test efficacy (NOTE) and spec fidelity (BLOCKING: the patch holds only the
  fixture) reported out-of-proof findings.

Severity is not asserted: the locked severity rule makes it depend on
spec/verify evidence, not on the defect.
