# risk lens — survivor-lifecycle

Failure modes drive the graph. The four that shape it: (1) a false suppression
hides a real bug, (2) an acceptance written in one phase dies at the squash and
the backlog refloods, (3) a run that never touched a file condemns that file's
acceptance as stale, (4) a survivor lands in two buckets or none and the counts
stop meaning anything. Each gets exactly one owner.

```
Phase survivor-lifecycle — 8 tasks across 4 waves

Wave 1
  t-1  Key survivors by file+op+line-text hash
       files:    internal/survivor/identity.go, internal/survivor/identity_test.go
       covers:   c-4
       contract: an unrelated edit that shifts an accepted line 40 lines down yields the
                 same key; changing only the mutation op yields a different one;
                 tabs→spaces, trailing whitespace and CRLF hash equal while a token
                 reorder does not; a file with `return nil` on 3 lines reports
                 ambiguous=true occurrences=3, a single occurrence reports false;
                 hashing a deleted file or a line past EOF returns ErrSubjectGone
                 rather than a hash of "" (an empty hash collides every missing
                 subject onto one key).

  t-2  Add tracked survivors.toml acceptance store
       files:    internal/survivor/store.go, internal/survivor/store_test.go
       covers:   c-2, c-3
       contract: an entry with empty reason and no category is rejected at LOAD time,
                 not only by the CLI — a hand-edited reasonless entry must never
                 suppress; an entry naming a category absent from [category] is
                 rejected; two entries with the same key fail load with "duplicate
                 survivor key"; a save preserves entries the writer never touched
                 (read-modify-write, not clobber) and a failed encode leaves the
                 prior survivors.toml byte-identical; the store path resolves to
                 <repo-root>/.dross/survivors.toml when called from a nested subdir.

  t-3  Ship drain policy as builtin rule
       files:    internal/rules/rules.go, internal/rules/rules_test.go, .dross/rules.toml
       covers:   c-6
       contract: rules.Render over an EMPTY merged set contains the drain rule's id and
                 text (a repo with no project rules still gets it); this repo's
                 .dross/rules.toml no longer defines r-02, and the rendered block
                 contains the drain-don't-relist text exactly once — a test counting
                 occurrences catches the builtin+project duplicate.

Wave 2 (depends t-1, t-2)
  t-4  Classify each survivor into one lifecycle state
       files:    internal/survivor/classify.go, internal/survivor/classify_test.go
       covers:   c-1, c-2, c-4
       depends:  t-1, t-2
       contract: an accepted unambiguous survivor is absent from the in-diff list and
                 present in accepted with its reason; an accepted survivor whose line
                 text now occurs 3× in the file is re-emitted as in-diff carrying an
                 "ambiguous acceptance" note — suppression must NOT happen; a survivor
                 whose key matches a routed deferred entry is emitted routed with its
                 target and is not counted unclassified; a survivor matching both an
                 acceptance and a routed entry appears in exactly one bucket (accepted
                 wins) and len(in-diff)+len(routed)+len(accepted)+len(unclassified)
                 equals the input count — the sum assertion is what catches double
                 listing and silent drops; a survivor whose source line errors with
                 ErrSubjectGone lands unclassified, never dropped.

  t-5  Detect stale acceptances structurally
       files:    internal/survivor/stale.go, internal/survivor/stale_test.go
       covers:   c-5
       depends:  t-1, t-2
       contract: an acceptance whose file was deleted is reported stale with reason
                 "file gone"; one whose file exists but whose normalized line text no
                 longer occurs anywhere in it is reported stale with reason "text
                 gone"; an acceptance for a file the run never examined is NOT
                 reported stale (a phase touching one package must not condemn a
                 neighbour's acceptance); the store is byte-identical after a
                 staleness pass — staleness reports, it never deletes.

  t-6  Add dross survivor accept|route|list
       files:    internal/cmd/survivor.go, internal/cmd/survivor_test.go,
                 internal/phase/phase.go, cmd/dross/main.go
       covers:   c-2, c-3, c-7
       depends:  t-1, t-2
       contract: `survivor accept --file X --line N --op OP` with neither --reason nor
                 --category exits non-zero and leaves survivors.toml unwritten;
                 accepting a file:line that no verify run reported errors instead of
                 inventing an entry (a typo'd line cannot create a phantom
                 acceptance); `survivor route --target <slug>` appends a [[deferred]]
                 entry to that phase's spec.toml carrying key = <identity> and
                 `dross deferred list --target <slug>` then lists it; routing to a
                 nonexistent phase slug errors before any write; `survivor list` run
                 from a nested subdir prints the repo-root store's entries; the repo
                 .gitignore does not match ".dross/survivors.toml" (assert
                 ignoresPath == false — an ignored store dies at the squash-merge
                 exactly the way the board mapping did).

Wave 3 (depends t-4)
  t-7  Emit lifecycle state in tests.json and findings
       files:    internal/verify/verify.go, internal/verify/verify_test.go
       covers:   c-1, c-2, c-7
       depends:  t-4
       contract: Skeleton emits zero FLAGs for an accepted survivor, one FLAG per
                 unclassified survivor, and a routed survivor's FLAG text names its
                 target phase; every entry in tests.json `out_of_scope` carries a key
                 and a non-empty state, and an out-of-scope survivor present in the
                 store is absent from that list entirely; the "filtered N
                 out-of-scope survivor(s)" NOTE counts only the post-lifecycle
                 unclassified remainder, so accepting one decrements N; a survivor
                 reaching tests.json with state == "" fails the emission test rather
                 than blending into the list.

Wave 4 (depends t-5, t-6, t-7)
  t-8  Wire store, staleness and prompt into the verify run
       files:    internal/cmd/verify.go, internal/cmd/verify_test.go,
                 assets/prompts/verify.md, internal/cmd/verify_prompt_test.go
       covers:   c-1, c-3, c-5
       depends:  t-5, t-6, t-7
       contract: a verify run for phase B suppresses a survivor accepted while phase A
                 was current (store read from the repo root, never the phase dir) —
                 this is the c-3 durability test; verify prints one NOTE per stale
                 acceptance naming its file and still exits 0 (staleness surfaces,
                 never fails a phase); the summary prints in-diff / routed / accepted
                 / unclassified counts that sum to the survivor total; a corrupt
                 survivors.toml makes `dross verify` fail with a "load survivors"
                 error rather than proceeding with zero acceptances (a parse error
                 must never read as "nothing is accepted" and re-emit the whole
                 drained backlog); verify_prompt_test asserts assets/prompts/verify.md
                 names `dross survivor route` and no longer instructs the LLM to leave
                 out_of_scope entries unrouted.
```

## Coverage

| criterion | tasks |
|---|---|
| c-1 (exactly one state, unclassified is its own bucket) | t-4, t-7, t-8 |
| c-2 (accepted never re-emitted; reasonless acceptance rejected) | t-2, t-4, t-6, t-7 |
| c-3 (CLI not hand-edit; record outlives the phase) | t-2, t-6, t-8 |
| c-4 (identity survives line drift; no false match) | t-1, t-4 |
| c-5 (stale acceptance surfaced) | t-5, t-8 |
| c-6 (builtin drain rule; project r-02 removed) | t-3 |
| c-7 (out_of_scope routable) | t-6, t-7, t-8 |

7/7 criteria covered.

## Judgment calls

- **Identity computed from the working tree, not from `Mutant.Snippet`.** Only
  the Stryker adapter populates `Snippet` (internal/mutation/stryker.go:251);
  gremlins leaves it empty (internal/mutation/gremlins.go:416). Keying off a
  field two of three adapters never set would make every Go acceptance hash the
  empty string and collide. t-1 reads the file at the reported line instead —
  which is also what makes ErrSubjectGone (c-5) a natural product of the same
  code rather than a second mechanism.
- **`survivor route` resolves its key itself rather than reading a key out of
  tests.json.** The alternative — t-7 stamps keys into tests.json, t-6 reads
  them — makes wave 2 depend on wave 3 and creates a cycle. Recomputing from
  file+line+op keeps the CLI in wave 2 and removes a stale-artifact failure
  mode: a key read from a tests.json written three commits ago can be wrong,
  a key recomputed from the current tree cannot.
- **Load-time validation, not just CLI validation, for the reason requirement.**
  Rejecting a reasonless acceptance only in `survivor accept` leaves the real
  hole open: a hand-edited or merge-mangled entry with `reason = ""` would
  suppress silently. c-2's "rejected" is enforced where suppression is decided.
  Rejected the softer option of warning-and-ignoring — an ignored entry and a
  suppressing entry look identical in the output.
- **Staleness is scoped to what the run actually examined.** The obvious
  implementation — every acceptance whose subject text is missing is stale —
  marks a neighbour package's acceptance stale on any run that didn't mutate it,
  which is a false-positive flood on exactly the entries this milestone just
  drained. t-5 owns the narrower rule and tests the negative case directly.
- **Accepted beats routed when a survivor matches both.** Ambiguity here is the
  c-1 failure mode (a survivor in two buckets, counts that don't sum). Chose
  accepted-wins because acceptance is the explicit, reasoned, repo-level claim
  and routing is a parking decision; the alternative (routed-wins) would make an
  accepted survivor reappear in the run whenever an old deferred entry lingers.
- **Prompt edit lands in t-8, not its own task.** assets/prompts/verify.md
  currently instructs the LLM *not* to route out_of_scope entries (line 78) —
  leaving it in place would have the prompt contradict c-7 the moment the code
  ships. It rides with the command wiring so the prompt and the CLI it names
  land in one commit. Note rule r-01: `make install` before relying on it.
- **Split t-7 (verify package) from t-8 (cmd wiring) despite both being "make
  verify use it".** They fail differently — t-7 fails as wrong finding output on
  a hand-built Tests fixture, t-8 fails as a store loaded from the wrong path or
  a corrupt file read as empty. One task would let a green pure-function test
  mask a broken store lookup, which is the false-green shape rule 1 of the
  execution rules exists to prevent.
- **t-3 is small enough to merge but stays standalone.** It touches
  internal/rules and this repo's own .dross/rules.toml — a different package and
  a config file from every other task. Folding it into a code task would put a
  repo-config edit inside a commit gated on unrelated tests.
