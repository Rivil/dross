When the mutation leg is long — a big Go repo, a remote host, anything you would
not want to sit through — do not hold the session waiting for it. Dispatch it and
come back:

```
dross verify <phase> --detach            # returns immediately; the run outlives the connection
dross verify <phase> --detach --at 02:00 # same, but the host starts it off-hours
```

The run is owned by the host from that moment: the link can drop, the laptop can
close, the session can be `/clear`ed. Collect it later — in this session or a
completely new one:

```
dross verify status                       # what is in flight, and where
dross verify results <phase>              # collect it; writes tests.json + verify.toml
```

`verify results` writes the same two artefacts an attached run does, through the
same pipeline, so everything below — the criterion mapping, the survivor
cross-check, the verdict — is unchanged. A detached verdict is not a second-class
one.

**A non-finished result is not a verdict about the code.** `verify results` exits
`10` scheduled, `11` running, `12` the run produced nothing measurable, `13` the
host was unreachable, `14` the run is gone. Only `12`, `13` and `14` need action,
and none of them is a red suite — a run that **did not report** says nothing
about the tests. On `10` or `11` the run is alive and fine: wait and collect
again. Never fill in verify.toml from a run you did not collect.

**A `scheduled` run naming a holder is waiting its turn, not stalled.** Every
remote mutation leg takes the host lock (`/tmp/dross-host.lock`) before it runs,
so a run dispatched while another repo's leg holds the host reads as `scheduled`
with a reason line — `waiting on <project>/<phase> run <id> (pid N) since <time>`
— in `verify status` and `verify results` (still exit `10`). That is the lock
working: it is **not a failure**, needs no cancel and no cleanup, and the run
starts by itself the moment the holder releases (a holder that dies releases it
too — there is no stale lock to clear). An attached `dross verify` waits the same
way, printing the holder once and a heartbeat every few minutes. Use `--no-wait`
only for an unattended probe that must not block — it refuses a held host with
exit `15`, naming the holder, and writes nothing; it never makes the run happen
sooner.
