- **2** — a `--files` path names something outside this repository. Fix the
  argv, not the config.
- **3** — the granted host could not be reached.
- **4** — the transfer to it did not complete, so whatever ran, ran against an
  incomplete tree.
- **5** — the file set matched no declared lane, or no test relates to it, so
  **nothing was measured**. Route it by the paths:
  - Exempt-only: in a repo with a vouching lane, every path matches
    `runtime.test_exempt`. Continue to §1f and commit with no test run; a current
    grant admits it. A commit refused for want of a grant waits on the user's own
    `dross trust --test-exempt`, never run on their behalf.
  - A changed path no test reaches: write a test for it and re-run, or run bare
    `dross test`.
  - No vouching lane: widen a lane (`dross test lane list`) or run the whole
    suite bare.
- **6** — a matched lane's command is untrusted or stale on this machine, so
  that lane did not run. Show the user the exact lane command and let them run
  `dross trust --lane <name>` themselves, never on their behalf — a lane grant
  is code execution the repo chose, exactly like the whole-suite one.
- **7** — a matched lane's `prepare` or `relate` command failed, so that lane's
  tests never ran. That line is the thing to fix, not the code: read the one the
  failure names. `relate` only maps paths to tests, so its failure is no
  bootstrap fault. The run's other lanes still ran, so a 7 can be hiding a
  green elsewhere — it is not a partial pass.
- **8** — a matched lane's toolchain is on neither the granted host nor this
  machine, so the lane never spawned. Read the binary the refusal names and
  tell the user which machine is missing it; the run's other lanes still ran,
  so an 8 can be hiding a green elsewhere. A lane the granted host alone lacks
  is not this — that one falls back and runs here, announced by name, and
  reports its own suite result.

**2, 3, 4, 5, 6, 7 and 8 all mean the run did not happen** — in whole or in part.
None of them is a reason to commit (an exempt-only 5 commits on its grant, not
on the run), and none of them is a red suite: treating one as a test failure
sends you hunting a bug in code that was never executed.
