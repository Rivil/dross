# MVP lens — survivor-lifecycle

Phase survivor-lifecycle — 5 tasks across 3 waves

```
Wave 1
  t-1  Add survivor identity + acceptance store
       files:    internal/survivor/survivor.go
                 internal/survivor/store.go
                 internal/survivor/survivor_test.go
                 internal/mutation/adapter.go
       covers:   c-2, c-3, c-4, c-5
       depends:  —
       desc:     New internal/survivor package: Compute(root,file,line,op) -> key +
                 ambiguous flag (sha256 over the normalized mutated line, ambiguous when
                 that normalized text occurs more than once in the file); Store
                 load/save/Add over .dross/survivors.toml with [[category]] prose reuse
                 and reason validation; Stale(produced, mutatedFiles, exists) returning
                 per-entry staleness reasons; the four lifecycle-state constants.
                 mutation.Mutant gains Key / Lifecycle / Note, alongside Origin.
       contract: - Store.Add with Reason=="" and no category (or a category carrying no
                   prose) returns an error naming the missing reason and leaves the file
                   byte-identical; the same Add with a reasonless entry but a prose-
                   carrying category succeeds.
                 - Compute returns the identical key after 20 blank lines are inserted
                   above the mutated line, and a different key once that line's own
                   source text is edited.
                 - Compute reports ambiguous=true for a line whose normalized text occurs
                   twice in the file, false when it occurs once.
                 - Stale flags (a) an entry whose file no longer exists and (b) an entry
                   whose file WAS mutated this run but whose key is absent from produced;
                   it does NOT flag an entry in a file the run never mutated.
                 - Store round-trips through TOML with category-only entries preserved
                   (encode -> decode -> deep-equal).

  t-2  Ship drain policy as builtin rule
       files:    internal/rules/rules.go
                 internal/rules/rules_test.go
                 internal/cmd/rule_test.go
                 .dross/rules.toml
       covers:   c-6
       depends:  —
       desc:     Append a `dross-survivor-drain` entry to rules.Builtins carrying r-02's
                 prose, retargeted at the new verbs (`dross survivor accept --reason` /
                 `dross survivor route --target`). Delete the [[rule]] r-02 block from
                 this repo's .dross/rules.toml.
       contract: - rules.Render(nil) — the empty merged set, i.e. a repo with no project
                   rules — contains "[builtin/hard/dross-survivor-drain]" and the phrase
                   "only ever shrinks"; the existing "(no user rules configured)" line is
                   still emitted beside it.
                 - A test reading this repo's own .dross/rules.toml asserts no [[rule]]
                   text contains "Pre-existing faults are not furniture" — the project-
                   local duplicate re-appearing fails it.

Wave 2 (depends t-1)
  t-3  Classify survivors in verify output
       files:    internal/verify/lifecycle.go
                 internal/verify/lifecycle_test.go
                 internal/verify/verify.go
                 internal/cmd/verify.go
       covers:   c-1, c-2, c-5, c-7
       depends:  t-1
       desc:     verify.Classify stamps Key + Lifecycle on every kept survivor and every
                 out_of_scope entry: accepted keys drop out of Surviving (and so out of
                 the FLAG findings) while the killed/survived counts stay untouched;
                 routed keys render lifecycle=routed with the target in Note; an
                 ambiguous match never suppresses and carries an ambiguity Note; a
                 survivor whose key cannot be computed is lifecycle=unclassified.
                 Skeleton adds one aggregate NOTE for suppressed acceptances and one for
                 stale ones. internal/cmd/verify.go loads .dross/survivors.toml from the
                 repo root, gathers routed keys from every phase spec's [[deferred]]
                 target entries, and passes both in.
       contract: - A key present in .dross/survivors.toml with a reason vanishes from
                   languages[].mutation.surviving and from the "mutant survived" FLAG
                   findings on the next Skeleton, while summary.mutants_survived is
                   unchanged from the pre-acceptance run.
                 - An acceptance written while phase A was current still suppresses on a
                   verify run for phase B (store read from .dross/, not
                   .dross/phases/<id>/) — moving the store phase-local fails this.
                 - A survivor whose mutated line text is duplicated in its file is still
                   emitted despite a matching acceptance, with a Note naming the
                   ambiguity.
                 - A survivor whose key matches a routed [[deferred]] entry renders
                   lifecycle="routed" with the target slug in Note and is not counted as
                   unclassified.
                 - A survivor whose file is gone at classify time renders
                   lifecycle="unclassified" rather than defaulting to in-diff.
                 - Every entry in tests.json out_of_scope carries a non-empty key and a
                   lifecycle value.
                 - An acceptance for a file the run mutated whose key no adapter produced
                   yields exactly one stale NOTE finding; an acceptance in an unmutated
                   file yields none.

  t-4  Add dross survivor accept|route|list
       files:    internal/cmd/survivor.go
                 internal/cmd/survivor_test.go
                 cmd/dross/main.go
                 README.md
       covers:   c-2, c-3, c-7
       depends:  t-1
       desc:     Cobra tree `dross survivor` with accept (<file>:<line> --op --reason
                 [--category]), route (<file>:<line> --op --target [--from], appending a
                 [[deferred]] entry whose text carries the key so `dross deferred`
                 already owns it), and list (--phase / --stale / --json, merging
                 tests.json in-scope + out_of_scope rows against the store and routed
                 set). Registered in main.go's tree; README command table gains the row.
       contract: - `dross survivor accept a.go:3 --op X` with no --reason exits non-zero
                   and .dross/survivors.toml is not created; the same call with --reason
                   exits 0 and `dross survivor list --json` then reports that key with
                   state "accepted".
                 - The store is written at <root>/.dross/survivors.toml; the test asserts
                   the exact path and fails if it regresses to .dross/phases/<id>/.
                 - `dross survivor route` on a row taken from tests.json out_of_scope
                   appends a [[deferred]] entry whose text contains the survivor key and
                   whose target is the named phase, and `dross deferred list --target
                   <phase>` then lists it.
                 - `dross survivor list --stale` names an acceptance whose file has been
                   deleted and omits acceptances whose files still exist.
                 - `dross survivor accept` on a file:line whose normalized text is
                   duplicated still writes the entry but prints the ambiguity warning
                   (the acceptance is recorded, the suppression is not promised).

Wave 3 (depends t-3, t-4)
  t-5  Rewrite verify prompt survivor handling
       files:    assets/prompts/verify.md
                 internal/cmd/verify_prompt_test.go
       covers:   c-1, c-7
       depends:  t-3, t-4
       desc:     Replace §"Do not re-list the out_of_scope entries" with the drain
                 instruction: read each survivor's `lifecycle`, and close out every
                 in-diff / unclassified / out_of_scope row the same run via `dross
                 survivor accept --reason` or `dross survivor route --target`. Document
                 the accepted-suppressed and stale NOTE lines.
       contract: - verify_prompt_test greps assets/prompts/verify.md for "dross survivor
                   accept", "dross survivor route" and "unclassified", and fails if the
                   string "for the survivor-lifecycle phase to route" (the defer-to-a-
                   successor instruction this phase makes false) is still present.
```

## Coverage

| Criterion | Tasks |
|---|---|
| c-1 (one lifecycle state per survivor; unclassified is explicit) | t-3, t-5 |
| c-2 (accepted not re-emitted; reasonless acceptance rejected) | t-1, t-3, t-4 |
| c-3 (CLI action, record outlives the phase) | t-1, t-4 |
| c-4 (identity survives line drift; ambiguity does not suppress) | t-1 |
| c-5 (stale acceptance surfaced) | t-1, t-3 |
| c-6 (drain policy as builtin; project r-02 removed) | t-2 |
| c-7 (out_of_scope routable) | t-3, t-4, t-5 |

7/7 criteria covered.

## Judgment calls

- **One `internal/survivor` package for keying + store + staleness, not three tasks.** Rejected splitting identity from the store: they share the key type and neither delivers a criterion alone, so a split would buy one extra wave and no independent verification.
- **Lifecycle fields go on `mutation.Mutant` (Key/Lifecycle/Note), not a parallel verify-side survivor list.** `Origin` already lives there for exactly this reason — kept survivors reach tests.json through `languages[].mutation.surviving`. A side list would make "exactly one lifecycle state" (c-1) a join instead of a field.
- **The struct-field change to `mutation.Mutant` is folded into t-1, not t-3.** t-3 was otherwise a 5-file task, tripping the split rule; the field addition is a schema change belonging with the identity work anyway.
- **`unclassified` means "key could not be computed", not "no origin tag".** Every in-scope survivor already gets an in-hunk/inherited origin, so the honest empty state is the one where identity itself fails (file gone, line out of range, unreadable) — that is what must not blend into the list.
- **Accepted survivors are dropped from `surviving` and the findings but stay in killed/survived counts.** Rejected removing them from the denominator: the mutant did survive, and laundering the score is a different (worse) failure than a noisy worklist. The lock only requires they not be re-emitted.
- **Staleness is scoped to files the run actually mutated.** Rejected "key not produced this run ⇒ stale": a single phase's verify only produces mutants for its own scope, so the unscoped rule would declare every other acceptance stale on every run — the exact re-flood `no_acceptance_ttl` forbids.
- **`survivor accept|route` address a survivor by `<file>:<line> --op` and compute the key from disk, not by an index into tests.json.** This keeps t-4 off t-3's output shape (both stay wave 2) and makes in-scope and out-of-scope rows addressable by the same syntax.
- **Routing reuses `[[deferred]]` with the key embedded in the item text.** Rejected a new `routed` table in survivors.toml — the lock says the store stays accepted-only and routing reuses `dross deferred`, and text-embedding means `deferred list/route/unroute/dismiss` need no changes at all.
- **The prompt rewrite is its own wave-3 task rather than merged into t-3 or t-5's neighbour.** It is one file and ~15 minutes, but it depends on both wave-2 outputs; merging it into either would create a false dependency between them and serialize the wave.
- **No `dross survivor unaccept` / edit verb.** Not traceable to a criterion; `survivors.toml` is tracked and hand-removable, and c-3 only requires that *creating* the record is a CLI action.
- **No migration of the ~76-entry existing drain into survivors.toml as a task.** Not a criterion; it is the first use of the shipped verbs, not part of shipping them.
