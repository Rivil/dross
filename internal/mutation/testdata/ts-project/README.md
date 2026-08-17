# dross TypeScript mutation fixture

A deliberately small, real TypeScript project. It exists so dross's Stryker
adapter can be proven **end to end** — the argv it builds, the config it hands
Stryker, the report path it reads back and the tool's actual output format all
have to agree, and every other Stryker test in this repo asserts one of those in
isolation against a canned JSON report.

`src/tally.ts` is written so the run produces both outcomes:

- `tally` and `classify` are covered by `src/tally.test.ts`, so their mutants
  are **killed** — which is what proves the pipeline measured something.
- `describe` is deliberately uncovered, so at least one mutant **survives** —
  which is what proves the report distinguishes the two rather than reporting
  everything as killed.

It is not shipped and is not part of the dross binary. Dependencies are not
vendored: `internal/mutation/stryker_e2e_test.go` skips with a named reason when
the Node toolchain is absent, rather than failing or silently passing.
