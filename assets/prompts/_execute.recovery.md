Handle each that applies before the run starts:

- **Wrong branch** (HEAD is not `phase/<id>`) — that's the divergence pattern the phase-branch model exists to prevent.
  - Switch with `dross phase checkout <id>` and proceed. It is guarded, and refuses rather than create a branch missing locally.
  - Otherwise: stop. The phase wasn't created with `dross phase create` (which auto-checks out). Have the user run `dross phase create` or migrate existing work to a branch before executing.
- **Dirty tree** — surface the diff to the user and ask: "Commit existing work first / stash / abort". Never execute on a dirty tree — atomic commits need a clean baseline.
- **A task left `in_progress`** — ask the user "continue task X / reset to pending and pick fresh".
