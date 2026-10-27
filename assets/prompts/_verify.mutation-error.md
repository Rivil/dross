If mutation testing fails to run (e.g. Stryker not installed), surface the error to the user and ask:
- `install Stryker` — guide them through `pnpm add -D @stryker-mutator/core` (or equivalent for their package manager from `project.toml`)
- `skip mutation` — re-run with `--skip-mutation`; verdict will note that mutation efficacy is unverified
- `abort`
