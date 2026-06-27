# Stack profiles

A **stack profile** is a single declarative TOML file that tunes dross to one
technology stack. One profile supplies three things:

- **runtime command tuning** — the test / typecheck / format / build commands
  `dross init` and `dross onboard` seed into `project.toml [runtime]`;
- **the tool loadout** — the security scanners and quality analyzers that
  `dross-secure` and `dross-quality` run for this stack;
- **the agent loadout** — the MCP tools, guardrails, and conventions
  `dross stack loadout` emits for a coding agent working in this stack.

## Adding a new stack is a single TOML drop-in — zero code change

Profiles are pure data. **To add or override a stack, drop a `<id>.toml` file
into `~/.claude/dross/profiles/`.** No recompilation, no Go changes, nothing to
register — detection, runtime seeding, the tool catalogs, and the loadout all
read the profile by data alone. This is the whole point: shipping support for the
next framework is a new file, not a new build.

- The files in this directory (`go.toml`) are the **built-in** profiles, embedded
  in the binary so a fresh install works out of the box.
- A user-dir profile with the **same `id`** as a built-in **overrides** it
  (the user dir wins on collision).
- A profile with a **new `id`** adds a brand-new stack.

## Marker-file stacks

Some stacks have no source language of their own but ship build / config artifacts
worth scanning. They are detected by **file patterns** rather than source
extensions: a profile declares `[signals].file_patterns` (and no `exts`), so it is
surfaced *additively* into the `dross-secure` / `dross-quality` manifests by
`MarkerProfiles` — never selected as a primary stack by `dross stack detect`.

Some marker stacks (Kubernetes, CloudFormation) live in plain `*.yaml` / `*.json`
that an extension glob alone can't distinguish from any other YAML/JSON. Those add a
`[signals.content]` gate: after a glob selects a candidate, its body is read (capped,
case-sensitive) and the profile is surfaced only when the declared tokens match —
`all` = every token (AND), `any` = at least one (OR). A profile with no content gate
keeps the pure-glob fast path (its candidates' bodies are never read).

- **`docker`** — Dockerfiles and compose files: `hadolint` (lint), `trivy config`
  (compose misconfig), and `dockle` (container image-layer CIS). dockle is surfaced
  in detect, but it needs a *built* image — which dross never builds — so its
  no-image case is skipped-with-reason on the run path (supply `--image` /
  `$DROSS_IMAGE` to scan a prebuilt image).
- **`terraform`** — Terraform / IaC files (`*.tf`, `*.tf.json`, `*.tfvars`,
  `*.tfvars.json`, `*.hcl`): `trivy config` and `checkov` for misconfigurations,
  `tflint` for lint / error-handling.
- **`kubernetes`** — Kubernetes manifests (`*.yaml` / `*.yml` / `*.json`,
  content-confirmed by `apiVersion` + `kind`): `trivy config` and `checkov`
  scanners, `kube-linter` analyzer (error-handling).
- **`cloudformation`** — CloudFormation templates (`*.yaml` / `*.yml` / `*.json`,
  content-confirmed by `AWSTemplateFormatVersion` or `Resources`): `trivy config`
  and `checkov` scanners, `cfn-lint` analyzer (error-handling).

## Schema (see `internal/stack/profile.go` for the authoritative struct)

```toml
id    = "go"            # required, non-empty — how the profile is addressed
title = "Go"

[signals]              # how `dross stack detect` selects this profile
  files    = ["go.mod"]   # root marker files (strong signal)
  exts     = [".go"]      # source extensions (weak signal)
  priority = 10           # tiebreaker on a polyglot tree
  # file_patterns = ["*.yaml"]      # marker stacks: glob instead of exts
  # [signals.content]               # marker stacks in ambiguous YAML/JSON: confirm
  #   all = ["apiVersion", "kind"]  #   a glob candidate's body (all=AND, any=OR)

[[package_managers]]   # one entry per variant (npm/pnpm/yarn, pip/poetry/uv, …)
  name = "go"
  bin  = "go"
  lockfile = "go.sum"

[runtime.test]         # each slot is `run = "..."` or a list of `[[…variants]]`
  run = "go test -count=1 ./..."
# …typecheck / format / build likewise

[[tools]]              # scanners (kind="scanner") + analyzers (kind="analyzer")
  name      = "gocyclo"
  kind      = "analyzer"
  dimension = "complexity"   # analyzers only: the maintainability axis
  core      = true           # absence warrants a prominent warning
  optional  = false          # optional=true tools are silently skipped when absent
  # bin defaults to name; bin_by_os = { darwin = "...", linux = "..." } overrides per OS

[loadout]              # the agent loadout (`dross stack loadout`)
  mcp_tools   = ["…"]
  guardrails  = ["…"]
  conventions = ["…"]
```

Every field except `id` is optional. Unknown stacks fall back to the agnostic
tool set, and an unmatched detection returns `unsupported` — never a guessed
default.
