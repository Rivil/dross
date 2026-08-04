# c-5 red proof — a hostile `.dross/` reproduces every vector against pre-phase code

This is the record backing acceptance criterion **c-5**: the fixture in this
directory reproduces each config-derived attack, and every test in the
regression suite **fails against the pre-phase code**. A regression suite that
was never seen red proves only that it agrees with the code it was written
beside.

Unlike the `terraform-c3` / `iac-multi-c5` fixtures — which are recorded manual
runs, because shelling `trivy`/`tflint` out of `go test` fights the
single-static-binary ethos — this fixture **is** a `go test` gate. Nothing here
shells out to a tool dross doesn't already run.

## Threat model

You cloned a repo. It has a `.dross/` directory. You ran a dross command.

That is the whole precondition. Nothing below needs the user to type an
attacker-supplied argument, edit a config, or approve a prompt — every payload
is committed data that dross reads and hands to `git` or to an authenticated
HTTP client. The trust boundary this phase draws is: *`.dross/` is untrusted
input, not configuration you wrote.*

## Fixture

| file | role |
| --- | --- |
| `project.toml` | the hostile config — carries the ref payloads, the leading-dash `branch_pattern`, and the attacker-controlled `api_base` / `base_url` |
| `local-tracked.toml` | a `.dross/local.toml` the attacker committed, trying to self-authorize `attacker.example` through the escape hatch |
| `expected-refusals.txt` | the refusal contract — one row per vector, pinned before the guards existed |

`__SENTINEL__` in a payload is rewritten by the harness to a path inside the
test's temp dir. It is the fail-open detector: if any guard lets the payload
reach git, that file exists afterwards.

### The three payload shapes

1. **`--output=<file>`** — git's diff option parser accepts this on `log`,
   `show` and `diff` and writes the command's output there. `repair_state.go`
   runs `git log <mainBranch> …`, so a committed `[repo].git_main_branch` of
   this shape makes `dross repair` an arbitrary-file write. This is a live
   vector, not a theoretical one.
2. **`--upload-pack=<cmd>`** — git runs the named command on the remote side of
   `ls-remote`/`fetch`. Carried as the `phase-checkout` row's payload.
3. **an off-allowlist `api_base`** — every authenticated request, and the secret
   named by `[remote].auth_env` with it, is redirected to a host the *repo*
   chose rather than the user.

## Vectors

Eleven rows in `expected-refusals.txt`, driven one subtest each by the suite:

| id | command | what fails open without the guard |
| --- | --- | --- |
| `phase-complete` | `dross phase complete` | ref positional read as a flag |
| `phase-checkout` | `dross phase checkout -- <payload>` | user-derived ref read as a flag |
| `phase-create` | `dross phase create` | `branch_pattern` renders a `-`-leading branch |
| `milestone-create` | `dross milestone create` | ref positional read as a flag |
| `ship-recover` | `dross ship --recover` | ref positional read as a flag |
| `repair-state` | `dross repair` | **arbitrary file write** via `git log --output=` |
| `board-client` | board client construction | board PAT sent to `attacker.example` |
| `ship-open` | `dross ship` (PR open) | `$GITHUB_TOKEN` sent to `attacker.example` |
| `tracked-local` | `readAllowHosts` | hostile repo self-authorizes its own host |
| `doctor-branch` | `dross doctor` | broken/hostile ref undiagnosable until a command dies |
| `doctor-host` | `dross doctor` | off-allowlist host undiagnosable until a command dies |

## Reproduce (green — the shipped code refuses)

Pre-req (rule r-01): `make install` first, so the installed binary is not stale
against source.

```
go test -count=1 ./internal/cmd/ -run TestHostileConfig
go test -count=1 ./internal/forge/ -run TestHostileConfig
```

## Reproduce (red — the pre-phase code does not)

The replay runs the same suite against this phase's base commit in a detached
worktree, so the guards are absent but the fixture and the tests are present:

```
BASE=<pinned below>
git worktree add /tmp/dross-redproof "$BASE"
cp internal/cmd/hostile_config_test.go   /tmp/dross-redproof/internal/cmd/
cp internal/forge/hostile_config_test.go /tmp/dross-redproof/internal/forge/
cp -R fixtures/hostile-config-c5         /tmp/dross-redproof/fixtures/
cd /tmp/dross-redproof && go test -count=1 ./internal/cmd/ ./internal/forge/ -run TestHostileConfig
```

## Recorded red replay

> **NOT YET RECORDED.** This section is filled in by task **t-15**, after the
> replay above has actually been run and its output read — never before. Per the
> execution-safety rule, a result that was not observed is not written down, and
> a red proof asserted from expectation is worth less than no red proof at all,
> because it *looks* like evidence.
>
> t-15 must replace this block with:
>
> - the **base commit SHA** the worktree was created at (pinned, and asserted to
>   exist by `TestRedProofPinsBaseCommit`);
> - the **verbatim `go test` failure output**, with one named failing test per
>   vector id, so `/dross-verify` can re-run the replay rather than trust prose;
> - for `repair-state`, the observed **existence of the sentinel file** under the
>   pre-phase code — the vector's proof is the file, not the message.

## Conclusion

Pending the recorded replay above. The claim this fixture exists to support:
against pre-phase dross, cloning a repo and running an ordinary command is
enough to write a file of the attacker's choosing and to hand two tokens to a
host the attacker named — and after this phase, each of those is a refusal that
names the committed line responsible.
