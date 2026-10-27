Each survivor in `tests.json` carries exactly one `Lifecycle` state, and each state has one job:

| state | what it means | what you do |
| --- | --- | --- |
| `in-diff` | in a file this phase touched | judge it as above — downgrade the criterion, or FLAG it |
| `routed` | a deferred item carries its key plus a destination | leave it; the skeleton NOTEs it with its target |
| `accepted` | recorded in `.dross/survivors.toml` with a reason | leave it; it is already silent, by decision |
| `unclassified` | no state applies — out of the diff, unrouted, unaccepted, or its identity would not resolve | **drain it this run** |

Drain an `unclassified` survivor with one of exactly two verbs, then re-run `dross verify`:
```
dross survivor accept <file>:<line> --op <OP> --reason "<why it is permanently acceptable>"
dross survivor route  <file>:<line> --op <OP> --target <phase-slug>
```
`accept` is for a survivor that is genuinely unkillable (e.g. gremlins' switch-case / const-initializer attribution ceiling) — it is the only state that earns silence, and the reason is mandatory. `route` is for real debt with a home: it stays visible, labelled with where it went.

Leaving `unclassified` rows to be re-listed by the next run is the standing-backlog failure the `dross-survivor-drain` builtin rule forbids. Do not re-state them as extra findings either — the skeleton already FLAGs each one; your job is to clear them, not to copy them.
