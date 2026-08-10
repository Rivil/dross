# survivor-lifecycle — panel synthesis (cold judge)

## Scores

| draft | criteria coverage | test-contract specificity | granularity | wave correctness |
|---|---|---|---|---|
| **risk** | 7/7; every criterion has ≥1 owner and c-1/c-2/c-7 have three each — no criterion rides on a single task's success | Strongest failure-direction contracts in the panel: the sum assertion on lifecycle buckets, byte-identical-store-on-reject, `ignoresPath` on `.dross/survivors.toml`, corrupt-store-must-not-read-as-empty. Every contract names what breaking it looks like | 8 tasks; clean single-owner-per-failure-mode split. Only softness is t-2 carrying both store I/O and load-time validation, and t-6 absorbing a `phase.go` schema change without naming it | 4 waves, no cycles; deliberately avoided the t-6↔t-7 cycle by having the CLI recompute keys. But t-6 sits in wave 2 while quietly needing a deferred-schema change no wave-1 task owns |
| **mvp** | 7/7 on paper, but c-4 and c-5 rest entirely inside one 4-file task — a criterion whose only owner is a mega-task is weakly owned | Contracts are concrete and TOML/JSON-shaped, and it is the only draft that pins what accepted survivors do to the *score* (dropped from `surviving`, kept in killed/survived). Coarser per bullet: several fold two behaviours into one assertion | Weakest. t-1 is 4 files spanning identity + store + staleness + a `mutation.Mutant` schema change and covers 4 criteria; t-3 spans `internal/verify` and `internal/cmd` in one task, exactly the pure-function-green-masks-broken-wiring shape | 3 waves and internally consistent, but the compression is bought by merging tasks that fail differently, not by finding real parallelism |
| **verification** | 7/7 with the best distribution; the only draft that gives c-7 a persistence owner (`survivor` field on `phase.Deferred`) instead of assuming the routed key survives a spec round-trip | Highest bullet count and most negative cases (no-clock signature pins `no_acceptance_ttl`; unreadable file ≠ stale; duplicate `Surviving` rows collapse — grounded in this repo's real gremlins output). **Penalty:** it anchors three contracts to gates by name and two of them do not exist — `TestEveryStructuredShowAcceptsJSON` and `TestReadmeAdvertisesOnlyRealCommands` are absent from the repo (`verify_prompt_test.go` is real). The *conventions* behind them are real (`TestMilestoneAndDefaultsShowJSONDropTheHeader` for bare-array JSON; `readme_doc_test.go` / `verify_prompt_test.go` for README-row assertions), so the contracts survive rewording, but as written they cite fiction | Best. 9 single-responsibility tasks; no file edited in two waves; stale detection is its own library so the no-TTL lock is unit-pinned rather than only reachable through a verify run | 4 waves, correct; wave 3 correctly holds both the CLI and the verify wiring because both need wave-2 output, and neither needs the other |

**Skeleton: `verification`.** It has the cleanest task boundaries, the only owner for the routed-key persistence seam, and it is the only draft where no file is touched in two waves. Its weakness is factual (two invented gate names) and local — reworded, not restructured. `risk` is the runner-up and supplies most of the grafted contracts; `mvp` is rejected as a skeleton because its t-1 and t-3 each merge tasks that fail in different ways.

All file paths in the merged plan below were checked against the repo. Existing: `internal/verify/verify.go`, `internal/verify/verify_test.go`, `internal/rules/rules.go` (`Builtins` currently has exactly 3 entries), `internal/rules/rules_test.go`, `internal/cmd/rule_test.go`, `internal/cmd/deferred.go`, `internal/cmd/deferred_test.go`, `internal/cmd/verify.go`, `internal/cmd/verify_test.go`, `internal/cmd/verify_scoping_test.go`, `internal/cmd/verify_prompt_test.go`, `internal/phase/phase.go`, `internal/phase/phase_test.go`, `internal/mutation/adapter.go`, `cmd/dross/main.go`, `assets/prompts/verify.md` (the sentence "for the survivor-lifecycle phase to route" is real, at line 78), `README.md`. Real helpers cited: `ignoresPath` (`internal/cmd/gitignore.go:127`), `EnforceSubcommandKnown` (`internal/cmd/subcommand_guard.go:17`), `saveAtomic` (`internal/findings/state.go:134`). New by design: everything under `internal/survivor/`, `internal/verify/lifecycle.go`, `internal/cmd/survivor.go`.

## Merged plan

```
Phase survivor-lifecycle — 9 tasks across 4 waves

Wave 1
  t-1  Survivor identity key + ambiguity + lifecycle fields on the mutant structs
       files:    internal/survivor/identity.go, internal/survivor/identity_test.go,
                 internal/mutation/adapter.go
       covers:   c-4
       origin:   [verification skeleton + risk contracts + mvp schema change]
       contract: - the same source line indented differently, with trailing whitespace,
                   or CRLF-terminated, hashes to the SAME key; two different ops on the
                   same line hash to DIFFERENT keys; a token reorder does not match
                 - a fixture where the target text moves from line 40 to line 88 (or
                   after 20 blank lines are inserted above it) resolves to the same key
                   — line number is not an input to the hash
                 - identical normalized text on two lines returns ambiguous=true with
                   occurrences=2 and still returns the key; one occurrence returns false
                 - a deleted file, a line index past EOF, or a blank/whitespace-only
                   line returns ErrSubjectGone and an EMPTY key — an empty-text key must
                   never be constructible, or one acceptance suppresses every missing
                   subject in the repo
                 - Mutant and OutOfScopeMutant gain Key/Lifecycle/Note beside Origin, and
                   both round-trip through tests.json JSON with the new fields present
                   [mvp: the fields must live on Mutant because languages[].mutation.
                   surviving serialises []Mutant directly — adapter.go:95-99]

  t-2  survivors.toml store with reason enforced at load, not just at write
       files:    internal/survivor/store.go, internal/survivor/store_test.go
       covers:   c-2, c-3
       origin:   [verification skeleton + risk load-time validation]
       contract: - Add with an empty reason and no category returns an error AND leaves
                   survivors.toml byte-identical (assert bytes before/after — a rejected
                   acceptance must not half-write)
                 - LOAD rejects a reasonless entry too, not only Add: a hand-edited or
                   merge-mangled `reason = ""` entry fails load rather than silently
                   suppressing [risk — c-2's "rejected" belongs where suppression is
                   decided]
                 - an entry naming a category absent from [category], or a category
                   carrying no prose, is rejected at load; one naming a category with
                   prose resolves its reason from it
                 - two entries with the same key fail load with "duplicate survivor key";
                   Add of an already-present key replaces rather than duplicates (Get
                   after two Adds returns the second reason, len==1)
                 - two acceptances sharing one category round-trip Save→Load with the
                   prose encoded exactly once
                 - Load of a missing survivors.toml returns an empty store and a nil
                   error — first run is not an error path
                 - Save is read-modify-write: entries the writer never touched survive,
                   and a failed encode leaves the prior file byte-identical (atomic
                   temp+rename, mirroring internal/findings/state.go:134 saveAtomic)
                 - the store path resolves to <repo-root>/.dross/survivors.toml when
                   called from a nested subdir [risk]

  t-3  Ship the drain policy as a builtin rule, drop project-local r-02
       files:    internal/rules/rules.go, internal/rules/rules_test.go,
                 internal/cmd/rule_test.go, .dross/rules.toml
       covers:   c-6
       origin:   [risk+mvp+verification — all three agree]
       contract: - Render over the EMPTY merged set (no global, no project rules)
                   contains `[builtin/hard/dross-survivor-drain]` and the phrase "only
                   ever shrinks", AND still emits "(no user rules configured)" — the
                   builtin must not read as a user rule
                 - the builtin's text names both escapes (`dross survivor accept
                   --reason`, `dross survivor route --target`) so rule and CLI agree
                 - a test reading this repo's own .dross/rules.toml asserts no rule with
                   id "r-02" and no rule text containing "Pre-existing faults are not
                   furniture" — the duplicate cannot creep back
                 - the rendered block contains the drain text exactly once (an occurrence
                   count, not a contains — that is what catches builtin+project overlap)
                 - rules.Builtins has exactly 4 entries with unique ids (it has 3 today;
                   guards a copy-paste id collision that would silently drop one)

  t-4  Carry a survivor key on deferred items
       files:    internal/phase/phase.go, internal/phase/phase_test.go,
                 internal/cmd/deferred.go, internal/cmd/deferred_test.go
       covers:   c-7
       origin:   [verification]
       contract: - a spec.toml with `[[deferred]] survivor = "<key>"` survives
                   LoadSpec→Save round-trip with the field intact — dropping it on save
                   is the exact failure that killed the board mapping
                 - `dross deferred list --json` emits `survivor` on entries carrying one
                   and OMITS the key entirely on entries that do not
                 - `dross deferred list --target <slug>` returns a survivor-carrying
                   entry alongside ordinary ones — routed survivors are not a second
                   class hidden from the existing filters
                 - `dross deferred unroute` on a survivor-carrying entry clears target
                   and leaves `survivor` set

Wave 2 (depends t-1, t-2)
  t-5  Classify survivors into exactly one lifecycle state, and emit it
       files:    internal/verify/lifecycle.go, internal/verify/lifecycle_test.go,
                 internal/verify/verify.go, internal/verify/verify_test.go
       covers:   c-1, c-2, c-4, c-7
       depends:  t-1, t-2
       origin:   [verification skeleton + risk sum/precedence + mvp score contract]
       contract: - len(in-diff)+len(routed)+len(accepted)+len(unclassified) equals the
                   input survivor count on every fixture — the sum assertion is what
                   catches double-listing and silent drops [risk; this is the sharpest
                   c-1 contract in the panel]
                 - over a fixture with one in-scope survivor, one routed out_of_scope
                   entry, one accepted entry and one bare out_of_scope entry, every
                   record has a non-empty state and the four states appear once each
                 - the accepted survivor produces NO finding; deleting only its
                   acceptance from the fixture produces exactly one FLAG for it — the
                   suppression is attributable to the acceptance and nothing else
                 - a survivor whose key resolves ambiguous is NOT suppressed even when
                   its key is accepted; it re-emits carrying the ambiguity note (the
                   locked safe-failure direction)
                 - a survivor matching BOTH an acceptance and a routed entry lands in
                   exactly one bucket, accepted wins [risk — the alternative makes an
                   accepted survivor reappear whenever a stale deferred entry lingers]
                 - accepted survivors leave summary.mutants_survived and the mutation
                   score unchanged from the pre-acceptance run — they drop out of the
                   worklist, not out of the denominator [mvp]
                 - an unrouted, unaccepted out_of_scope entry gets its OWN FLAG labelled
                   unclassified, and the existing "filtered N out-of-scope survivor(s)"
                   NOTE (verify.go:467) counts only the post-lifecycle unclassified
                   remainder — accepting one decrements N [risk]
                 - duplicate Surviving rows with identical file/line/op (gremlins emits
                   these — see mutation-diff-scope/tests.json) collapse to ONE record, so
                   one acceptance suppresses both [verification]
                 - an Identifier returning ErrSubjectGone yields unclassified with the
                   error in the note — never a panic, never a silent drop, never a
                   default to in-diff
                 - a survivor reaching tests.json with state == "" fails the test rather
                   than blending into the list [risk]
       note:     identity is injected as an Identifier interface (satisfied by
                 survivor.Resolver, faked in tests) — internal/verify/scope.go is
                 deliberately pure, and a filesystem dependency makes the ambiguity
                 contract expensive to assert and therefore under-asserted

  t-6  Detect stale acceptances structurally, with no clock
       files:    internal/survivor/stale.go, internal/survivor/stale_test.go
       covers:   c-5
       depends:  t-1, t-2
       origin:   [verification+risk]
       contract: - an acceptance whose file was deleted is returned with reason "file no
                   longer exists"
                 - an acceptance whose file exists but whose recorded normalized text no
                   longer occurs anywhere in it is returned with reason "mutant no longer
                   produced"
                 - an acceptance whose text merely moved to a different line is NOT stale
                   — staleness must not be a line-number check wearing a different name
                 - an acceptance in a file the run never examined, whose subject is
                   intact, is NOT stale — a phase touching one package must not condemn a
                   neighbour's acceptance [risk]
                 - Stale() takes no time input at all: the signature has no clock and no
                   TTL parameter, pinning the no_acceptance_ttl lock as a unit test
                 - an unreadable file (permission error) is reported with the read error,
                   not silently classified stale — a stale verdict invites deletion of a
                   live acceptance
                 - the store is byte-identical after a staleness pass — staleness
                   reports, it never deletes [risk]

Wave 3 (depends t-1, t-2, t-4, t-5, t-6)
  t-7  Add dross survivor accept|route|list
       files:    internal/cmd/survivor.go, internal/cmd/survivor_test.go,
                 cmd/dross/main.go
       covers:   c-2, c-3, c-5, c-7
       depends:  t-1, t-2, t-4, t-6
       origin:   [verification skeleton + risk gitignore/path guards + mvp addressing]
       contract: - `survivor accept <file>:<line> --op OP` with neither --reason nor
                   --category exits non-zero AND .dross/survivors.toml does not exist
                   afterwards
                 - accept writes to <root>/.dross/survivors.toml — assert that exact
                   path and assert nothing was written under .dross/phases/<id>/ (the
                   phase-local store is the failure the acceptance_store lock exists to
                   prevent); `survivor list` from a nested subdir prints the repo-root
                   store's entries [risk]
                 - the repo .gitignore does not match ".dross/survivors.toml" — assert
                   ignoresPath(body, ".dross/survivors.toml") == false; an ignored store
                   dies at the squash-merge exactly the way the board mapping did [risk;
                   ignoresPath is real at internal/cmd/gitignore.go:127]
                 - `survivor route <file>:<line> --op OP --target SLUG` appends a
                   [[deferred]] entry to that phase's spec.toml with both `survivor` and
                   `target` set, and `dross deferred list --target <slug>` then lists it;
                   routing to a nonexistent phase slug errors before any write [risk]
                 - `survivor list --json` emits a bare JSON array that unmarshals with no
                   `# ` header line, matching the convention pinned by
                   TestMilestoneAndDefaultsShowJSONDropTheHeader
                 - `survivor list --stale` prints only entries t-6 calls stale; an empty
                   store prints "(no accepted survivors)" and exits 0
                 - `dross survivor frob` exits non-zero via EnforceSubcommandKnown rather
                   than printing help and exiting 0
                 - accepting a file:line whose normalized text is ambiguous still records
                   the acceptance but warns on stdout — the CLI does not silently record
                   something that will never suppress

  t-8  Wire the store, staleness and lifecycle counts into dross verify
       files:    internal/cmd/verify.go, internal/cmd/verify_test.go,
                 internal/cmd/verify_scoping_test.go
       covers:   c-1, c-2, c-3, c-5
       depends:  t-2, t-5, t-6
       origin:   [risk+verification]
       contract: - an acceptance recorded while phase A was current suppresses the
                   matching survivor in a `dross verify B` run, end-to-end — the store is
                   read from the repo root, never the phase dir. This is the c-3
                   durability test and moving the store phase-local fails it
                 - the summary prints `survivors: <n> in-diff, <n> routed, <n> accepted,
                   <n> unclassified`, the counts sum to the survivor total, and
                   unclassified is non-zero for a fixture whose tests.json has an
                   unrouted out_of_scope entry
                 - with survivors.toml absent, verify's findings are byte-identical to
                   the pre-phase behaviour for in-scope survivors — a missing store is
                   not an error path
                 - a CORRUPT survivors.toml makes `dross verify` fail with a "load
                   survivors" error rather than proceeding with zero acceptances — a
                   parse error must never read as "nothing is accepted" and re-emit the
                   whole drained backlog [risk]
                 - a stale acceptance produces exactly one NOTE naming the file and the
                   stale reason, verify still exits 0, and the verdict and mutation score
                   are unchanged — staleness surfaces, it never fails a phase
                 - tests.json carries the resolved `key` and `state` on each classified
                   survivor and on each out_of_scope entry, so the prompt can copy a key
                   without re-deriving it

Wave 4 (depends t-7, t-8)
  t-9  Retire the deferral text in the verify prompt and README
       files:    assets/prompts/verify.md, internal/cmd/verify_prompt_test.go, README.md
       covers:   c-1, c-7
       depends:  t-7, t-8
       origin:   [verification+mvp]
       contract: - verify_prompt_test asserts assets/prompts/verify.md no longer contains
                   "for the survivor-lifecycle phase to route" — the sentence at line 78
                   that becomes a lie the moment t-7 lands
                 - the prompt contains `dross survivor route` and `dross survivor accept`
                   and names all four lifecycle states including "unclassified"
                 - the prompt instructs that an in-diff / unclassified / out_of_scope row
                   is closed out in the SAME run (the builtin drain rule restated at the
                   point of use), replacing the current "Do not re-list the out_of_scope
                   entries as findings" section
                 - README gains a `dross survivor {accept,route,list}` command-table row,
                   and the `dross verify` row's out_of_scope wording is updated to match
                   the shipped behaviour (precedent: TestReadmeStatesScopingGuarantee in
                   verify_prompt_test.go:162 already asserts against that row)
       note:     rule r-01 — assets/ edits are not live until `make install`; run it
                 before relying on the prompt change
```

Coverage: c-1 → t-5, t-8, t-9 · c-2 → t-2, t-5, t-7, t-8 · c-3 → t-2, t-7, t-8 · c-4 → t-1, t-5 · c-5 → t-6, t-7, t-8 · c-6 → t-3 · c-7 → t-4, t-5, t-7, t-9. 7/7.

## Disagreements

**D1 — Where a routed survivor's key lives.**
`verification` adds an optional `survivor` field to `phase.Deferred` and gives it its own wave-1 task (t-4). `mvp` explicitly rejects a schema change and embeds the key inside the deferred item's `Text`, arguing `deferred list/route/unroute/dismiss` then need no changes at all. `risk` implies a field (it lists `internal/phase/phase.go` among its CLI task's files) but never gives it an owner.
*Provisional default:* the field, as its own wave-1 task. *Why it matters:* with the key in prose, "is this survivor routed?" becomes a substring match over free text, `deferred list --json` cannot filter on it, and any later reword of the item text silently unroutes the survivor. The field costs one round-trip test; the text-embedding cost is a lookup that degrades silently. If the field is rejected, t-4 disappears and t-7 drops back to wave 2.

**D2 — What makes an acceptance stale.**
`verification` and `risk` both check the subject against the working tree only: file deleted, or the recorded normalized text no longer occurs in the file. `mvp` instead checks the run's produced mutant set — an entry is stale if its file WAS mutated this run but its key is absent from `produced` — and scopes that to mutated files so neighbours are not condemned.
*Provisional default:* the disk-only check (2 of 3 lenses, and it needs no run state threaded into the store package, which is what lets t-6 sit in wave 2 with a no-clock signature). *Why it matters:* c-5 says "file deleted, **or the mutant is no longer produced**". Disk-only approximates the second clause by text-gone; it will not catch a mutant that stopped being produced while its source line is unchanged (an adapter config change, a mutator removed from the allowlist). `mvp`'s rule catches that case but requires the run's produced set as an input, which couples staleness to a verify run and makes the no-TTL lock testable only through one. Taking the default means accepting that one staleness shape goes undetected until the line itself changes.

**D3 — Can you accept a survivor no verify run reported?**
`risk` requires that accepting a `file:line` no run reported errors out, so a typo cannot mint a phantom acceptance. `mvp` and `verification` both resolve the key purely from the working tree and explicitly reject indexing into tests.json.
*Provisional default:* resolve from disk, no tests.json cross-check. *Why it matters:* `risk`'s guard is real protection against a silent typo'd acceptance, but it couples `survivor accept` to a fresh tests.json existing for the right phase — which breaks the c-7 case of accepting an out_of_scope survivor surfaced by a *different* phase's run. The merged plan buys back part of the protection with the ambiguity warning and the `ErrSubjectGone`/empty-key rule in t-1: a typo'd line that is blank or past EOF errors anyway. A typo landing on a real, different line still records a harmless never-matching entry.

**D4 — One verify task or two.**
`risk` and `verification` both split the `internal/verify` classification/emission from the `internal/cmd` wiring, with `risk` giving the reason explicitly: they fail differently, and one task lets a green pure-function test mask a broken store lookup. `mvp` merges them into a single t-3 spanning both packages.
*Provisional default:* split (t-5 / t-8). *Why it matters:* this is the false-green shape the execution rules exist to prevent — a passing classification test over a hand-built `Tests` fixture says nothing about whether `internal/cmd/verify.go` found the store at the repo root. Merging saves one task and costs the ability to attribute a failure.

**D5 — Where the docs land.**
`verification` puts prompt + README in one wave-4 task. `mvp` puts README inside the CLI task and the prompt in its own wave-3 task. `risk` folds the prompt into its verify-wiring task and has no README work at all.
*Provisional default:* `verification`'s single wave-4 docs task. *Why it matters:* it keeps every file single-wave (README and the prompt are each edited exactly once) and keeps a repo-config/doc edit out of a commit gated on unrelated Go tests. The cost `verification` itself names: the CLI ships one wave before its docs. `risk`'s omission of the README row is a genuine gap — the merged plan takes the README work from `mvp`/`verification`.

**D6 — Is a reasonless acceptance rejected at write time or at load time?**
`risk` enforces it at LOAD, arguing that rejecting only in `survivor accept` leaves the real hole open: a hand-edited or merge-mangled `reason = ""` entry would suppress silently. `mvp` and `verification` validate in `Store.Add` only.
*Provisional default:* both, with load-time as the binding one (grafted into t-2). *Why it matters:* c-2 says an acceptance without a reason "is rejected", and suppression is decided at load, not at write — the store is a tracked, hand-editable TOML file by design, so the write path is not the only way an entry gets in. The cost is that a malformed `survivors.toml` now fails `dross verify` outright rather than being partially usable; t-8's corrupt-store contract makes that failure explicit and deliberate rather than a silent read-as-empty.

---

**Factual correction carried into the merged plan (not a divergence):** `verification` anchors contracts to `TestEveryStructuredShowAcceptsJSON` and `TestReadmeAdvertisesOnlyRealCommands`. Neither exists in this repo. The merged plan re-anchors those two contracts to real gates — `TestMilestoneAndDefaultsShowJSONDropTheHeader` (`internal/cmd/json_show_test.go:111`) for the bare-JSON-array shape, and `TestReadmeStatesScopingGuarantee` (`internal/cmd/verify_prompt_test.go:162`) as the precedent for asserting against a README command row. `verify_prompt_test.go` itself is real and its file-level contracts stand.
