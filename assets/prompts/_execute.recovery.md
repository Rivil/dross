Handle each that applies before the run starts:

- **Wrong branch** (HEAD is not `phase/<id>`) — that's the divergence pattern the phase-branch model exists to prevent.
  - Switch with `dross phase checkout <id>` and proceed. It is guarded, and refuses rather than create a branch missing locally.
  - Otherwise: stop. The phase wasn't created with `dross phase create` (which auto-checks out). Have the user run `dross phase create` or migrate existing work to a branch before executing.
- **A writer still in flight** — `.dross/gate/writer.json` names the `in_progress` task with `"state": "launched"`: its writer outlived the session that spawned it, and its report went with that session. Never resume the task inline, and handle it before the two arms below. Set its code aside, then fail it:
  ```
  git stash push -u -- . ':(exclude).dross'
  dross task status <phase> <task-id> failed --reason "writer: still in flight on resume"
  ```
- **Dirty tree** — surface the diff to the user and ask: "Commit existing work first / stash / abort". Never execute on a dirty tree — atomic commits need a clean baseline.
- **A task left `in_progress`** — ask the user "continue task X / reset to pending and pick fresh".
