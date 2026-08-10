# survivor-lifecycle — verification lens

Every task below was derived by writing the criterion's failing test first, then
cutting the smallest seam that makes that test writable. Where an existing gate
already exists in this repo (`TestEveryStructuredShowAcceptsJSON`,
`TestReadmeAdvertisesOnlyRealCommands`, `verify_prompt_test.go`), the contract
names it rather than inventing a parallel one.

```
Phase survivor-lifecycle — 9 tasks across 4 waves

Wave 1
  t-1  Add survivor identity key + ambiguity
       files:    internal/survivor/identity.go, internal/survivor/identity_test.go
       desc:     New package. Key(file, op, normText) hashes file+op+normalized
                 line text. Resolver reads the working tree at (file, line),
                 normalizes it, counts occurrences in that file, reports
                 ambiguous when the text occurs more than once.
       covers:   c-4
       depends:  —
       contract: - the same source line indented differently, or with trailing
                   whitespace, hashes to the SAME key; two different ops on the
                   same line hash to DIFFERENT keys
                 - a fixture file where the target text is moved from line 40 to
                   line 88 (unrelated inserts above) resolves to the same key —
                   line number is not an input to the hash
                 - a fixture with the identical normalized text on two lines
                   returns ambiguous=true and still returns the key
                 - a line index past EOF, or a blank/whitespace-only line,
                   returns an error and an EMPTY key — an empty-text key must
                   never be constructible, or one acceptance would suppress
                   every blank-line survivor in the repo

  t-2  Add survivors.toml store with required reason
       files:    internal/survivor/store.go, internal/survivor/store_test.go
       desc:     Store at <root>/.dross/survivors.toml: [[accepted]] entries
                 (key, file, op, text, category, reason, accepted_at, phase) plus
                 [[category]] tables holding shared reason prose once. Load/Save
                 (atomic temp+rename, mirroring internal/findings.saveAtomic),
                 Add/Get/Remove.
       covers:   c-2, c-3
       depends:  —
       contract: - Add with an empty reason and no category returns an error AND
                   leaves survivors.toml byte-identical (assert file bytes before
                   and after; a rejected acceptance must not half-write)
                 - Add naming a category that has no prose returns an error;
                   naming one that does resolves the reason from the category
                 - two acceptances sharing one category round-trip through
                   Save→Load with the prose stored exactly once (assert the
                   encoded TOML contains the reason string a single time)
                 - Load of a missing survivors.toml returns an empty store and a
                   nil error (first-run case), not an error
                 - Add of a key already present replaces rather than duplicates —
                   Get after two Adds returns the second reason and len==1

  t-3  Ship drain rule as builtin, drop r-02
       files:    internal/rules/rules.go, internal/rules/rules_test.go,
                 internal/cmd/rule_test.go, .dross/rules.toml
       desc:     Add a fourth entry to rules.Builtins carrying the
                 drain-don't-relist policy, and delete the project-local r-02
                 duplicate from this repo's .dross/rules.toml.
       covers:   c-6
       depends:  —
       contract: - Render(nil) (no global, no project rules) contains
                   `[builtin/hard/dross-survivor-drain]` AND still emits
                   "(no user rules configured)" — the builtin must not be
                   mistaken for a user rule
                 - the builtin's text names the accept-with-reason escape and the
                   route escape, so the rule and the CLI verbs agree
                 - a test reading this repo's .dross/rules.toml asserts no rule
                   with id "r-02" and no rule whose text contains "Pre-existing
                   faults are not furniture" — the duplicate cannot creep back
                 - rules.Builtins has exactly 4 entries with unique ids (guards a
                   copy-paste id collision that would silently drop one)

  t-4  Carry survivor key on deferred items
       files:    internal/phase/phase.go, internal/phase/phase_test.go,
                 internal/cmd/deferred.go, internal/cmd/deferred_test.go
       desc:     Add an optional `survivor` field to phase.Deferred and surface
                 it in deferredEntry / `dross deferred list --json`.
       covers:   c-7
       depends:  —
       contract: - a spec.toml with `[[deferred]] survivor = "<key>"` survives
                   LoadSpec→Save round-trip with the field intact (the field is
                   what makes a routed survivor findable; dropping it on save is
                   the exact failure that killed the board mapping)
                 - `dross deferred list --json` emits `survivor` on entries that
                   carry one and OMITS the key entirely on entries that do not
                 - `dross deferred list --target <slug>` returns a survivor-
                   carrying entry alongside ordinary ones — routed survivors are
                   not a second class hidden from the existing filters
                 - `dross deferred unroute` on a survivor-carrying entry clears
                   target and leaves `survivor` set

Wave 2 (depends t-1, t-2)
  t-5  Classify survivors into lifecycle states
       files:    internal/verify/lifecycle.go, internal/verify/lifecycle_test.go,
                 internal/verify/verify.go, internal/verify/verify_test.go
       desc:     Classify() maps every in-scope survivor and every out_of_scope
                 entry to exactly one of in-diff | routed | accepted |
                 unclassified, using an injected Identifier interface (satisfied
                 by survivor.Resolver, faked in tests) plus accepted/routed
                 lookup maps. Skeleton consumes it: accepted emits nothing,
                 routed emits a NOTE naming the destination, in-diff and
                 unclassified emit FLAGs.
       covers:   c-1, c-2, c-4, c-7
       depends:  t-1, t-2
       contract: - over a fixture with one in-scope survivor, one routed
                   out_of_scope entry, one accepted entry and one bare
                   out_of_scope entry, every returned record has a non-empty
                   state and the four states are exactly one each
                 - the accepted survivor produces NO finding; deleting only its
                   acceptance from the fixture produces exactly one FLAG for it
                   (the suppression is attributable to the acceptance, nothing
                   else)
                 - a survivor whose key resolves ambiguous is NOT suppressed even
                   when its key is accepted — it re-emits with the ambiguity note
                   in its finding text (the locked safe-failure direction)
                 - an out_of_scope entry with no acceptance and no route is
                   labelled `unclassified` in its own finding; the old single
                   "filtered N out-of-scope survivor(s)" NOTE no longer absorbs it
                 - duplicate Surviving rows with identical file/line/op (gremlins
                   emits these — see mutation-diff-scope/tests.json) collapse to
                   ONE classified record, so an acceptance suppresses both
                 - an Identifier returning an error for a survivor yields
                   `unclassified` with the error in the note, never a panic and
                   never silent omission

  t-6  Detect stale acceptances structurally
       files:    internal/survivor/stale.go, internal/survivor/stale_test.go
       desc:     Stale(root, store) returns each acceptance whose subject no
                 longer exists, with a reason — file deleted, or the recorded
                 normalized text no longer occurs in the file. No age input.
       covers:   c-5
       depends:  t-1, t-2
       contract: - an acceptance whose file was deleted is returned with reason
                   "file no longer exists"
                 - an acceptance whose file exists but whose recorded text was
                   edited away is returned with reason "mutant no longer produced"
                 - an acceptance whose text moved to a different line is NOT
                   stale (shares t-1's drift behaviour — staleness must not be a
                   line-number check wearing a different name)
                 - Stale() takes no time input at all: the signature has no clock
                   and no TTL parameter, pinning the no_acceptance_ttl lock
                 - an unreadable file (permission error) is reported with the
                   read error, not silently classified stale — a stale verdict
                   invites deletion of a live acceptance

Wave 3
  t-7  Add dross survivor accept|route|list
       files:    internal/cmd/survivor.go, internal/cmd/survivor_test.go,
                 cmd/dross/main.go
       desc:     New cobra command registered on the root tree.
                 `accept <phase> <file>:<line> --op OP --reason TEXT|--category C`
                 resolves the key and writes the repo-level store;
                 `route <phase> <file>:<line> --op OP --target SLUG` appends a
                 survivor-carrying deferred item to that phase's spec.toml;
                 `list [--stale] [--json]` renders the store.
       covers:   c-2, c-3, c-5, c-7
       depends:  t-1, t-2, t-4, t-6
       contract: - `survivor accept` with neither --reason nor --category exits
                   non-zero AND .dross/survivors.toml does not exist afterwards
                 - accept writes to <root>/.dross/survivors.toml — the test
                   asserts that exact path and asserts nothing was written under
                   .dross/phases/<id>/ (the phase-local store is the failure mode
                   the acceptance_store lock exists to prevent)
                 - `survivor route` on a file/line taken from a phase's
                   tests.json out_of_scope list appends a [[deferred]] entry to
                   THAT phase's spec.toml with both `survivor` and `target` set,
                   and `dross deferred list --target <slug>` then lists it
                 - `survivor list --json` emits a bare JSON array that unmarshals
                   (matching the locked json_shape: no `# ` header line)
                 - `survivor list --stale` prints only entries t-6 calls stale;
                   an empty store prints "(no accepted survivors)" and exits 0
                 - `dross survivor frob` exits non-zero via EnforceSubcommandKnown
                   rather than printing help and exiting 0
                 - accepting a file:line whose text is ambiguous still records
                   the acceptance but warns on stdout — the CLI does not silently
                   record something that will never suppress

  t-8  Wire lifecycle into dross verify output
       files:    internal/cmd/verify.go, internal/cmd/verify_test.go,
                 internal/cmd/verify_scoping_test.go
       desc:     verify loads the repo-level store, collects routed survivor keys
                 from every phase spec, builds a survivor.Resolver over the
                 working tree, passes both into RunScoped/Skeleton, prints a
                 per-state count line, and emits a NOTE per stale acceptance.
       covers:   c-1, c-2, c-3, c-5
       depends:  t-2, t-5, t-6
       contract: - an acceptance recorded with `--phase A` suppresses the matching
                   survivor in a `dross verify B` run — the assertion that the
                   record outlives its phase, run end-to-end rather than at the
                   store layer
                 - the summary prints
                   `survivors: <n> in-diff, <n> routed, <n> accepted, <n> unclassified`
                   and the unclassified count is non-zero for a fixture whose
                   tests.json has an unrouted out_of_scope entry
                 - with survivors.toml absent, verify's findings are byte-identical
                   to the pre-phase behaviour for in-scope survivors (a missing
                   store is not an error path)
                 - a stale acceptance produces exactly one NOTE naming the file
                   and the stale reason; it does not change the verdict or the
                   mutation score
                 - tests.json carries the resolved `key` and `state` on each
                   classified survivor, so the prompt can copy a key without
                   re-deriving it

Wave 4 (depends t-7, t-8)
  t-9  Retire the deferral text in docs + prompt
       files:    assets/prompts/verify.md, internal/cmd/verify_prompt_test.go,
                 README.md
       desc:     Rewrite the verify prompt's out-of-scope section: the lifecycle
                 exists now, so unclassified survivors get routed or accepted in
                 the same run. Add the `dross survivor` README row and update the
                 `dross verify` row's out_of_scope wording.
       covers:   c-1, c-7
       depends:  t-7, t-8
       contract: - verify_prompt_test asserts assets/prompts/verify.md no longer
                   contains "for the survivor-lifecycle phase to route" — the
                   sentence that becomes a lie the moment t-7 lands
                 - the prompt contains `dross survivor route` and
                   `dross survivor accept`, and names all four lifecycle states
                 - the prompt instructs that an `unclassified` survivor is
                   resolved in the same run (the r-02 / builtin drain rule
                   restated at the point of use)
                 - README has a `dross survivor {accept,route,list}` command-table
                   row, so `TestReadmeAdvertisesOnlyRealCommands` has a real
                   command behind every advertised name and the row-presence check
                   used for `dross local` / `dross phase` passes for it too
```

## Coverage

| criterion | tasks |
|---|---|
| c-1 (exactly one state; unclassified surfaced) | t-5, t-8, t-9 |
| c-2 (accepted not re-emitted; reasonless rejected) | t-2, t-5, t-7, t-8 |
| c-3 (CLI not hand-edit; record outlives phase) | t-2, t-7, t-8 |
| c-4 (identity survives drift; no false match) | t-1, t-5 |
| c-5 (stale acceptance surfaced) | t-6, t-7, t-8 |
| c-6 (builtin drain rule; r-02 removed) | t-3 |
| c-7 (out_of_scope routable) | t-4, t-5, t-7, t-9 |

7/7 criteria covered.

## Judgment calls

- **Identity resolution injected as an interface, not a filesystem call inside
  `internal/verify`.** Chose an `Identifier` interface defined at the consumer
  (t-5) and satisfied by `survivor.Resolver`; rejected having verify read the
  working tree directly. `internal/verify/scope.go` is deliberately pure, and a
  filesystem dependency there means every classification test needs a temp repo
  instead of a fake — the contract "ambiguous acceptance does not suppress"
  becomes expensive to assert and therefore under-asserted.
- **CLI takes `<file>:<line> --op`, not a bare `--key`.** Rejected accepting a
  pre-computed key from tests.json: an acceptance must store file, op and the
  normalized text for c-5 staleness to be checkable, and those are only
  recoverable by resolving from the working tree. The key still lands in
  tests.json (t-8) for human cross-reference.
- **Duplicate `Surviving` rows collapse at classification time.** This repo's own
  `mutation-diff-scope/tests.json` contains byte-identical duplicate survivor
  entries from gremlins. Chose to dedupe by resolved key in t-5; rejected fixing
  it in the adapter, which is a different phase's diff and would leave older
  tests.json files un-handled anyway.
- **Stale detection is a wave-2 library, not a verify-side afterthought.** Chose
  to give it its own task before both consumers (t-7 CLI, t-8 verify) so the
  no-clock signature is pinned by a unit test; rejected folding it into t-8,
  which would have made the "no TTL" lock testable only through a verify run.
- **Routed survivors live in the originating phase's spec.toml.** Chose the phase
  whose tests.json produced the entry as the deferred item's home; rejected a
  single central deferred file, which the `routed_state_source` lock rules out by
  requiring reuse of the existing `dross deferred` machinery — that machinery
  addresses items as `<phase> <idx>`.
- **Docs and prompt are one task at the end, not spread across the CLI tasks.**
  Chose to keep `README.md` and `assets/prompts/verify.md` out of t-7/t-8 so no
  file is edited in two waves; the cost is that the CLI lands one wave before its
  docs, which no existing gate fails on (the README parity test only fires the
  other direction, on advertised-but-absent commands).
- **c-6's "removed from this repo" is asserted by a test that reads the repo's
  own `.dross/rules.toml`.** Rejected treating the deletion as a bare edit —
  precedent exists (`readme_doc_test.go`, `gitignore_test.go` both assert against
  real repo files), and without the test the duplicate silently returns the next
  time someone runs `dross rule add`.
