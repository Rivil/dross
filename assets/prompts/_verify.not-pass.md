Recommend next steps under the report:
- `uncovered` criteria: "add tests that exercise <criterion> and re-run /dross-verify"
- `weak` criteria: "tighten assertions in <test> — currently doesn't catch <surviving mutant> kind of breakage"
- low mutation score: "look at the surviving mutants in verify.toml to see what the tests miss"

In §5, after the verify-artefacts commit, print this instead of a pass block:
```
Phase <id> verdict: <verdict>. Open .dross/phases/<id>/verify.toml for full detail.

Next:
  /dross-execute <id>      — amend the failing task (add tests / fix code)
      ↳ --from <task-id>   — resume at the failing task, skipping earlier done tasks
  /dross-verify            — re-run after addressing blocking findings

state is on disk — safe to /clear · fresh session: /dross-execute <id>
```
