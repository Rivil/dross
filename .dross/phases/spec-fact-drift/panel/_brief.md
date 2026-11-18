## 2. Goal-backward decomposition

Walk through criteria one by one. For each: **what's the smallest task that delivers this criterion?** Working backward from acceptance, not forward from tech.

For each task, decide:

| Field | Notes |
|---|---|
| `id` | `t-1`, `t-2`, … sequential |
| `wave` | `1` = runs first, `2` = depends on wave-1 output, etc. |
| `title` | Imperative, ≤8 words ("Add tags + meal_tags schema") |
| `files` | Concrete paths, not patterns. Read existing files first if uncertain. |
| `description` | 1-3 lines. What changes, not why. |
| `covers` | Criterion ids this task delivers (`["c-1", "c-2"]`) |
| `test_contract` | One or more **specific** statements: "if X breaks, test Y fails". Generic ("tests pass") is rejected. |
| `depends_on` | Task ids in lower waves. Empty = pure wave-1. |
| `status` | `pending` |

### Granularity rules

- **Too small:** if a task touches one file and is < 10 minutes of work, it probably belongs merged into another.
- **Too large:** if a task spans 5+ files OR more than 2 layers (e.g. db + api + ui), split it.
- **Wave correctness:** task X is wave N+1 only if it strictly needs the output of a wave-N task. If it doesn't, drop it to wave N for parallelism.

### Test contract quality

A test contract is **specific** when:
- It names the surface that breaks ("the unique constraint", "the rate limiter", "the 401 path")
- The user can imagine the test from the description ("11th tag insert returns 400")

Reject:
- "tests pass"
- "covered by existing tests"
- "integration test exists"


## Display format (use this for your draft)

```
Phase 03-meal-tagging — 5 tasks across 3 waves

Wave 1
  t-1  Add tags + meal_tags schema
       files:    db/schema.ts, db/migrations/0042_tags.sql
       covers:   c-1, c-2
       contract: unique constraint rejects duplicate name_normalized

Wave 2 (depends t-1)
  t-2  Tag CRUD endpoints
       ...
```

## Repo-specific planning notes (dross, Go)

- Code lives under `internal/<pkg>/` and `internal/cmd/` (cobra commands), entrypoint `cmd/dross/`. Prompts live in `assets/prompts/*.md` (installed into ~/.claude via `make install`).
- Existing pieces worth reading before you draft:
  - `internal/architecture/links.go` (+ `links_test.go`) — the ARCHITECTURE.md link check: Status (moved/gone classification), Resolve, matchSymbols, lastComponent fallback. c-2 and c-8 hinge on reusing this.
  - `internal/codex/` — the symbol index (`codex.Index`, `codex.SupportsFile`, `Symbol`). c-3 hinges on this.
  - `internal/cmd/phase_edges.go`, `internal/cmd/state_edges_gate_test.go`, `internal/cmd/validate.go` — the existing plan-lock gate pattern (`dross state set current_phase_status planned` refuses on unresolved edges, sharing validate's rule set). c-5 should mirror it.
  - `internal/cmd/architecture.go` — `dross architecture check`; doctor's architecture-link section (grep for it).
  - `internal/phase/` — spec.toml / plan.toml schema types (the locked `new = [...]` spec-level list needs a schema field).
  - `assets/prompts/plan.md` — c-4 edits this prompt (the §1/§2 flow of /dross-plan).
- **Companion/guard files — list them in the task's `files` up front** (writers are confined to task files; omissions cost a red run each):
  - new serialized string field (e.g. spec `new`) → `internal/cmd/testdata/pathfence_scan/not_paths.txt`, and if it holds paths, `internal/pathfence/fields.go` + the name list in `internal/pathfence/fields_test.go`
  - new CLI command/flag or usage change → `cmd/dross/testdata/cli_tree.txt`
  - Go string narrating a not-yet-registered `dross <cmd>` → `narratedIgnore` in `cmd/dross/main_test.go`
  - prompt edits → check `assets/testdata/prompt_instructions.txt` and prompt size pins in `internal/cmd/prompt_expand_test.go`
  Verify each of these paths exists before citing it.
