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

Twelve rows in `expected-refusals.txt`, driven one subtest each by the suite:

| id | command | what fails open without the guard |
| --- | --- | --- |
| `phase-complete` | `dross phase complete` | ref positional read as a flag |
| `phase-checkout` | `dross phase checkout -- <payload>` | user-derived ref read as a flag |
| `phase-create` | `dross phase create` | resolved base reaches `checkout -b` as a flag |
| `milestone-create` | `dross milestone create` | ref positional read as a flag |
| `ship-recover` | `dross ship recover` | ref positional read as a flag |
| `repair-state` | `dross repair` | **arbitrary file write** via `git log --output=` |
| `board-client` | board client construction | board PAT sent to `attacker.example` |
| `ship-open` | `dross ship` (PR open) | `$GITHUB_TOKEN` sent to `attacker.example` |
| `tracked-local` | `readAllowHosts` | hostile repo self-authorizes its own host |
| `doctor-branch` | `dross doctor` | broken/hostile ref undiagnosable until a command dies |
| `doctor-branch-pattern` | `dross doctor` | a `branch_pattern` git would reject goes unreported |
| `doctor-host` | `dross doctor` | off-allowlist host undiagnosable until a command dies |

`repo.branch_pattern` is deliberately a **doctor finding, not a refusal**: it is
a config key `dross project get/set` reads and nothing else consumes — phase
branch names are built as `"phase/"+id` — so its leading dash never reaches git
today. Reporting it is the honest treatment: the config is broken, and would
become a live vector the day something starts honouring it.

## Reproduce (green — the shipped code refuses)

Pre-req (rule r-01): `make install` first, so the installed binary is not stale
against source.

```
go test -count=1 ./internal/cmd/ -run TestHostileConfig
go test -count=1 ./internal/forge/ -run TestHostileConfig
```

## Reproduce (red — the pre-phase code does not)

The suite cannot be compiled against the base commit (it names symbols this
phase introduced), so the replay builds the base-commit BINARY and drives the
fixture through the real CLI — which is how a user meets a cloned repo anyway.
See the recorded replay below for the exact recipe and the observed output.

```
BASE=d62be4144c50fac1ba47fab681f46f57da579e6a
git worktree add --detach /tmp/dross-redproof "$BASE"
cd /tmp/dross-redproof && go build -o /tmp/dross-base ./cmd/dross
```

## Recorded red replay

**base commit: `d62be4144c50fac1ba47fab681f46f57da579e6a`** (`chore(dross): plan
config-trust-hardening`) — the last commit before any of this phase's work.

Recorded 2026-08-04. Everything below was observed in this session; nothing is
reconstructed from expectation.

### Why the replay drives the BINARY, not the suite

The first attempt was the worktree recipe above: check out the base commit, copy
the suite in, run `go test`. It does not compile —

```
internal/forge/hostile_config_test.go:12:2: no required module provides package
	github.com/Rivil/dross/internal/hostallow
internal/cmd/hostile_config_test.go:255:32: too many arguments in call to boardConfig
	have (project.Board, string, nil)
	want (project.Board)
internal/cmd/hostile_config_test.go:265:19: undefined: remotePolicy
internal/cmd/hostile_config_test.go:274:14: undefined: readAllowHosts
internal/cmd/hostile_config_test.go:394:27: undefined: gitRefArgs
```

— because the suite names symbols the phase introduced. That is a true statement
(the guards did not exist) but it is a **weaker** proof than the one c-5 asks
for: it shows the tests could not run, not that the attacks worked.

So the replay builds `dross` at the base commit and drives the fixture through
the real CLI, which is exactly how a user meets this repo. That reproduces each
vector as behaviour rather than as a compile error.

```
git worktree add --detach <wt> d62be4144c50fac1ba47fab681f46f57da579e6a
cd <wt> && go build -o /tmp/dross-base ./cmd/dross
# hostile repo: fixture project.toml with __SENTINEL__ substituted, plus a
# rules.toml and a state.json; HEAD on phase/other so repair judges state stale
```

### 1. `repair-state` — ARBITRARY FILE WRITE, observed

The strongest vector, and the one whose proof is a file rather than a message.
`dross repair` **in dry-run mode**, with no flags, on a freshly cloned repo:

```
$ /tmp/dross-base repair
Findings:
  ✗ .dross/state.json — missing or stale, will be reconstructed from git history

dry-run — pass --apply to write these fixes

$ ls -l $SENTINEL
-rw-r--r--@ 1 rivil  wheel  19  4 Aug 15:41 .../dross-pwned
```

The file exists. `git log --output=<file>` wrote it, from
`[repo].git_main_branch` alone. No flag, no prompt, no `--apply`.

### 2. `ship-recover` / `milestone-create` / `doctor` — payload accepted as a branch

```
$ /tmp/dross-base ship recover
Error: must be on --output=/.../dross-pwned before recovering (currently on phase/other)

$ /tmp/dross-base milestone create v9.9
created .../.dross/milestones/v9.9.toml          # proceeded

$ /tmp/dross-base doctor
  ✓ no recorded phase commits on local --output=/.../dross-pwned
```

Note doctor's line: pre-phase, it printed a **✓** naming the payload as though it
were an ordinary branch. There was nothing in the tool that could see this.

### 3. `phase-checkout` — refused, but for the wrong reason

```
$ /tmp/dross-base phase checkout -- '--upload-pack=touch /tmp/dross-pwned-up'
Error: no local branch phase/--upload-pack=touch /tmp/dross-pwned-up — ...
```

Refused only because the branch did not exist. The payload was never rejected as
a payload; a repo that also shipped a matching branch name would have got past it.

### 4. Same fixture, current build — every vector refuses

```
$ /tmp/dross-now repair
Error: check state.json: unsafe git ref for repo.git_main_branch:
  "--output=/.../dross-pwned-green" begins with "-", which git reads as an
  option rather than a ref name
$ ls ../dross-pwned-green
ls: ../dross-pwned-green: No such file or directory      # ← the delta

$ /tmp/dross-now ship recover
Error: unsafe git ref for repo.git_main_branch: "--output=/..." begins with "-" …

$ /tmp/dross-now milestone create v9.8
Error: unsafe git ref for repo.git_main_branch: "--output=/..." begins with "-" …

$ /tmp/dross-now doctor
  ✗ unsafe git ref for repo.git_main_branch: "--output=/..." begins with "-" …
  ✗ unsafe git ref for repo.branch_pattern: "-p/example-phase" begins with "-" …
  ✗ refusing to contact host "attacker.example": [remote].api_base resolves to
    attacker.example:443, which is not in the allowlist derived from
    [remote].url https://github.com/Rivil/dross plus the built-in known-SaaS hosts
    Fix (only if you trust this host): `dross local set allow_hosts attacker.example`
  ✗ refusing to contact host "attacker.example": [board].base_url resolves to …
```

### 5. Per-vector coverage in the committed suite

Every row of `expected-refusals.txt` is a named subtest, and the id set is
asserted equal to the driver set by `TestEveryVectorHasASubtest`:

| vector id | gating test |
| --- | --- |
| `phase-complete` | `TestHostileConfigVectors/phase-complete` |
| `phase-checkout` | `TestHostileConfigVectors/phase-checkout` |
| `phase-create` | `TestHostileConfigVectors/phase-create` |
| `milestone-create` | `TestHostileConfigVectors/milestone-create` |
| `ship-recover` | `TestHostileConfigVectors/ship-recover` |
| `repair-state` | `TestHostileConfigVectors/repair-state` + `TestHostileConfigNoPwnSentinel` |
| `board-client` | `TestHostileConfigVectors/board-client`, `TestHostileConfigForgeClientsRefuse` |
| `ship-open` | `TestHostileConfigVectors/ship-open` |
| `tracked-local` | `TestHostileConfigVectors/tracked-local` |
| `doctor-branch` | `TestHostileConfigVectors/doctor-branch` |
| `doctor-branch-pattern` | `TestHostileConfigVectors/doctor-branch-pattern` |
| `doctor-host` | `TestHostileConfigVectors/doctor-host` |

### What is NOT claimed

The four host vectors (`board-client`, `ship-open`, `doctor-host`,
`tracked-local`) were **not** replayed against the base binary by sending a real
request to a real host — doing so would mean deliberately transmitting a
credential. Their pre-phase behaviour is evident from the code at the base
commit: no constructor in `internal/forge` or `internal/ship` checked `api_base`
at all before reading `os.Getenv(cfg.AuthEnv)`, so every one of them attached the
token to whatever the committed config named. The committed suite gates them
going forward, with a fake host asserted to receive zero requests.

## Conclusion

Against pre-phase dross, cloning a repo and running `dross repair` — a dry-run
diagnostic, no flags — wrote a file of the repo author's choosing. `ship
recover`, `milestone create` and `doctor` all accepted an option-shaped string as
a branch name, doctor reporting it with a ✓. Every forge and ship constructor
attached the token named by `[remote].auth_env` to whatever `api_base` the repo
committed, with no host check anywhere in the path.

After this phase each of those is a refusal that names the committed line
responsible, the sentinel file does not appear, and `dross doctor` reports the
same config as findings that move its exit code — with the one escape hatch
(`dross local set allow_hosts`) named in the message, because a refusal with no
way forward is one people route around.
