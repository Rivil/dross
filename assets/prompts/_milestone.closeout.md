`--check` exited non-zero: it printed every unresolved item, grouped by kind, each with a `clear:` line naming what clears it, plus any acknowledged items and stale acks. Stale acks never block; mention them once and move on. Do not open the PR yet — `dross milestone complete <version>` would refuse anyway.

Walk the unresolved items **one per turn**, in the order `--check` listed them. For each, one `AskUserQuestion` naming the item and its detail, with these options:

- **resolve it now** — do what its `clear:` line names (`dross deferred absorb`, `dross survivor accept` / `route`, a slash command, …). Lead with this when the fix is small.
- **acknowledge it** — it rides the integration PR unresolved, and the reason goes into the PR body. The reason is the user's: take it from their answer, or ask — never invent one. Then:

  ```
  dross milestone ack <item> <version> --reason "<why>"
  ```

  `<item>` is the id the line starts with (`phase:<slug>`, `deferred:<source>#<idx>`, `survivor:<key>`). An empty reason is refused; re-acking an item replaces its reason.

Two kinds get different options:

- **Unverified** phases take no acknowledgement — `--check` prints them without an ack id. Offer **verify it** (`/dross-verify <slug>`) or, only on the user's explicit say-so, override the kind: `dross milestone complete <version> --check --force-unverified`, carrying `--force-unverified` on every later `--check` and on the open. The override is not recorded anywhere.
- An `ERROR` line is a source dross could not read: fix the file it names. It is never acknowledged.

After each item, re-run `dross milestone complete <version> --check` and keep walking until it exits 0; only then return to §8's confirm step and open the PR. Never open the integration PR while `--check` exits non-zero.
