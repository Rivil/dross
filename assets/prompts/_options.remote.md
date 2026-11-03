A host named in the tracked `project.toml` is refused by name — only the untracked `local.toml` can authorize one. The grant is never written through the generic key-writer; this section invokes the verb that owns it:

- **Grant / change** → `dross mutation remote grant <host> <workdir>`
- **Revoke** → `dross mutation remote revoke`
- **Keep**

The verb prints the host and workdir it is about to authorize before writing them. Read that line back to the user rather than summarizing it: a grant is code execution on that machine, as them, and a grant they did not read is a rubber stamp.

After any grant change, run `dross doctor` and surface its `Remote mutation:` line — it reports whether the host is reachable and whether the configured adapters' toolchains are installed there. Learning that here costs a second; learning it mid-verify costs a pushed tree and an empty report.

**Tuning knobs** — these *are* settable, via `dross local set <key> "<value>"`:

- `mutation_workers` — how many mutants run at once. **Unset is not zero**; it means the adapter picks its own default (half the local cores, or half the *remote's* cores under a grant). Only set a number if a run has actually misbehaved — a hardcoded value sizes a 32-core host's run by this laptop.
- `mutation_test_cpu` — CPUs each mutant's test run may use. Unset = 1. Raising it multiplies against `mutation_workers`; over-subscribing the box produces spurious timeouts that read as surviving mutants.
- `remote_scratch_base` — absolute path on the REMOTE host for the throwaway build cache a mutation run compiles into. Empty derives it from the granted workdir's parent, which is the right guess and not a guarantee: on one host that parent turned out to be a 75 GB root volume while the 300 GB volume provisioned for the work sat elsewhere, and a run filled root at ~1.3 GB/min. Set this when the derived location is the wrong volume. (`DROSS_SCRATCH_BASE` is the same lever for THIS machine, and `DROSS_SCRATCH_MIN_FREE_GB` sets the free-space floor a run refuses to start below — default 20, `0` disables.)
- `mutation_remote_env` — csv of variable **NAMES** a remote run needs (e.g. `DATABASE_URL,NODE_ENV`). Names only, never `NAME=value`: dross reads each value from its own environment at run time and stores none of them. A name listed here that is unset locally is an error, not an empty export. Ask for names the way you would any CSV — they are not secrets, so this one is safe to type in chat, unlike §11.
