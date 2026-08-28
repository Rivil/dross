# MVP lens — lane-prepare-step

Phase lane-prepare-step — 4 tasks across 3 waves

```
Wave 1
  t-1  Add lane prepare field, add + edit flags
       files:    internal/project/project.go
                 internal/cmd/test_lane.go
                 internal/cmd/test_lane_test.go
       covers:   c-1, c-6
       contract: `dross test lane add go --prepare "npm ci"` then `dross test lane list`
                 prints a `prepare:` row for go and none for a lane added without the
                 flag; drop the list branch and the second assertion fails
       contract: `dross test lane edit go --prepare "make gen"` leaves that lane's
                 `match` globs and `command` byte-identical in project.toml, and
                 `--prepare ""` deletes the key so the block round-trips to its
                 pre-prepare bytes; an edit implemented as remove-then-re-add loses
                 the globs and fails the first half
       contract: TestTomlFieldsCarryMatchingJSONTags (json_tag_parity_test.go) fails
                 if Prepare ships without a matching `json:"prepare,omitempty"` tag
       description:
                 Adds `Prepare string` to project.TestLane with toml/json
                 `prepare,omitempty`. `dross test lane add` gains `--prepare`
                 (trimmed, written only when non-empty). `dross test lane list`
                 prints `prepare:` only for lanes that declare one. New
                 `dross test lane edit <name> --prepare "<cmd>"` sets or clears the
                 field in place, leaving every other lane field untouched, and
                 prints the `dross trust --lane <name>` hint after the write.

Wave 2 (depends t-1)
  t-2  Fingerprint prepare into the lane grant
       files:    internal/cmd/trust.go
                 internal/cmd/doctor.go
                 internal/cmd/trust_lane_test.go
       covers:   c-4, c-6
       depends:  t-1
       contract: grant lane go, then change ONLY its prepare — LaneConsented returns
                 ConsentStale; a fingerprint still taken over lane.Command alone
                 returns ConsentGranted and fails this
       contract: lane (prepare "a", command "bc") and lane (prepare "ab", command "c")
                 fingerprint differently; a naive prepare+command concatenation makes
                 them equal and fails
       contract: a lane with NO prepare keeps the fingerprint it had before this phase
                 — a grant written as Fingerprint(command) still reads ConsentGranted,
                 so no existing lane is silently un-trusted by the upgrade
       contract: `dross trust --lane go` output contains the prepare line AND the
                 command line before the grant is written; removing either print
                 fails the assertion, and the recorded fingerprint covers both
       contract: `dross doctor`'s row for a lane whose only change is its prepare says
                 consent is stale, not trusted
       description:
                 Adds laneGrantLine(lane) — an unambiguous length-prefixed encoding of
                 prepare + command, falling back to the bare command when prepare is
                 empty. LaneConsented / GrantLaneConsent / laneConsentRefusal take the
                 lane so the grant, the refusal text and doctor's row all read the same
                 encoding. trustLane prints prepare then command before granting.

  t-4  Document prepare, edit and exit 7
       files:    README.md
                 assets/prompts/options.md
                 assets/prompts/execute.md
                 internal/cmd/options_docs_test.go
                 internal/cmd/execute_prompt_test.go
       covers:   c-1, c-3, c-6
       depends:  t-1
       contract: TestReadmeDocumentsTestLanes gains `--prepare` and
                 `dross test lane edit`; deleting either from README.md's lane row
                 fails it, and dropping the `7` prepare-failed clause from the exit
                 contract fails the exit-status assertion beside it
       contract: TestOptionsDocumentsTheSelectorSurfaceCorrectly still passes with the
                 new text — options.md keeps "remove-then-re-add" for match, command,
                 selector and empty_exit while naming `dross test lane edit --prepare`,
                 and every `--flag` in its `dross test lane add` examples is one
                 testLaneAdd() registers
       contract: new TestExecutePromptDocumentsPrepareExit fails if execute.md's exit
                 list omits **7** or still says "2, 3, 4, 5 and 6 all mean the run did
                 not happen" without 7
       description:
                 README's `dross test` row gains exit 7 and the new rank position; its
                 `dross test lane` row gains --prepare and the edit verb. options.md
                 §test lanes gains prepare and the narrowed edit surface.
                 execute.md's exit table gains the 7 bullet.

Wave 3 (depends t-2)
  t-3  Run prepare per lane, exit 7 on failure
       files:    internal/cmd/test.go
                 internal/cmd/test_lane_prepare_test.go
                 internal/cmd/test_files_test.go
       covers:   c-2, c-3, c-4, c-5
       depends:  t-1, t-2
       contract: a lane whose prepare exits non-zero exits 7 and its command NEVER
                 reaches the recorder — the recorder holds the prepare line and not
                 the lane command
       contract: lanes go (prepare fails) and docs (green), both matched: docs' command
                 IS in the recorder and the run still exits 7 — a prepare failure that
                 returned early fails the first half
       contract: a failed prepare beside a RED lane exits 7, not 1 — exitRank must put
                 exitPrepareFailed above exitSuiteFailed
       contract: transcript order — the prepare line is printed before the
                 `lane go: <line>` header and before any of that lane's output; a
                 prepare printed after the header fails the index comparison
       contract: the same lane under `--local` and under a granted remote produces the
                 same prepare line: the local recorder's line equals the prepare half
                 of the `sh -c` script the remote recorder received
       contract: a lane skipped before spawn records ZERO spawns — once for a
                 selector miss whose paths were all deleted, once for a
                 consent-refused lane
       contract: a prepare line beginning with `-` is refused by the up-front argfence
                 sweep with zero spawns, naming runtime.test_lane[<name>]
       contract: TestExitCodesArePairwiseDistinct fails if exitPrepareFailed collides
                 with an existing code
       description:
                 Adds exitPrepareFailed = 7 and its exitRank slot above
                 exitSuiteFailed. runTestLanes fences each matched lane's prepare in
                 the existing up-front sweep, then — after the one tree sync, after the
                 consent gate, after laneRunLine's miss check — prints and runs the
                 lane's prepare through runOneLane's transport before its command,
                 folding a failure into worst and skipping that lane's command only.
```

## Coverage

| criterion | tasks |
|---|---|
| c-1 (declare + list prepare; no-prepare lane unchanged) | t-1, t-4 |
| c-2 (runs after sync, before the command, same host, named first) | t-3 |
| c-3 (failure skips the lane, exits 7, outranks red, others run) | t-3, t-4 |
| c-4 (covered by the lane's grant; refuses that lane only) | t-2, t-3 |
| c-5 (same invocation local and remote) | t-3 |
| c-6 (`lane edit --prepare` keeps match+command, re-prompts consent) | t-1, t-2, t-4 |

Every criterion is claimed by at least one task; no task exists without a criterion.

## Judgment calls

- **Merged the schema field, `lane add --prepare`, `lane list` and the whole new
  `lane edit` verb into one task.** Rejected splitting `edit` out: it is ~40 lines in
  the file `add` and `list` already live in, and a separate task would leave two tasks
  editing test_lane.go with nothing between them.
- **c-6's "re-prompts for consent" is delivered by t-2's fingerprint, not by t-1
  calling RevokeLaneConsent.** Rejected an explicit revoke on edit: once the
  fingerprint covers prepare, an edited prepare is already ConsentStale, and stale is a
  strictly better message than absent — it tells the user the line *changed*. An
  explicit revoke would downgrade that to "never trusted".
- **The prepare-less fingerprint stays byte-identical to today's
  `Fingerprint(command)`.** Rejected encoding every lane uniformly: that is marginally
  cleaner but un-trusts every lane in every repo on upgrade, and c-1 says a lane
  declaring no prepare behaves exactly as it does today. The encoding is still
  unambiguous — length-prefixed whenever a prepare exists.
- **One run-wiring task rather than three (ordering / exit code / locality).** c-2,
  c-3 and c-5 are three assertions about one loop in one function; split, each part
  would have to stub the others to compile.
- **Docs are wave 2, parallel with t-2, not wave 3.** Exit 7 and its rank position are
  *locked in the spec*, not discovered by t-3, so the doc text does not need t-3's
  output — only t-1's registered flags, which the options doc gate cross-checks
  against `testLaneAdd()`.
- **No `dross validate` rule for prepare.** Rejected as untraceable: no criterion asks
  for one, an empty prepare is simply an absent prepare, and the argfence sweep in t-3
  already refuses the one shape (`-x`) that could reach a shell as an option.
- **Dedup, and a second consent key for the prepare line, are both absent by lock**
  (prepare_scope, prepare_consent) — recorded here so their absence reads as
  deliberate rather than as a gap.
