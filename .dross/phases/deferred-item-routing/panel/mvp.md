Phase deferred-item-routing — 3 tasks across 2 waves

Wave 1
  t-1  Add deferred target field and `dross deferred list`
       files:    internal/phase/phase.go, internal/cmd/deferred.go, cmd/dross/main.go, internal/cmd/deferred_test.go
       covers:   c-1, c-4, c-6
       desc:     Add optional `Target string` to the Deferred struct (target=<slug>;
                 absent = someday). Add a cobra `dross deferred list` that scans
                 .dross/phases/*/spec.toml [[deferred]] with flags --target <slug>,
                 --someday, --milestone <v>, --routed, --json, and a source column
                 naming the originating phase; register it in cmd/dross/main.go.
       contract: if `Target` is removed from the Deferred struct, the round-trip test
                 (write a spec with target, LoadSpec, assert the value reads back) fails;
                 if validate rejects a target-bearing [[deferred]], the "validate passes
                 with target present and with it absent" test fails; if --someday stops
                 excluding entries that have a target, the someday-omits-routed filter
                 test fails; if --target/--routed/--json/source-column regress, their
                 matching deferred_test assertions fail.

Wave 2 (depends t-1)
  t-2  Route deferred items in /dross-spec
       files:    assets/prompts/spec.md, internal/cmd/spec_prompt_test.go
       covers:   c-2, c-3, c-4, c-5
       desc:     §4 gives every deferred item a destination — pull-into-current-phase
                 (move out of [[deferred]] into a new [[criteria]]), park-in-milestone-
                 backlog (coin a slug, run `dross milestone add <v> phases <slug>`, stamp
                 target=<slug>), attach-to-named-phase (stamp target=<existing-slug>), or
                 leave unrouted only on explicit "someday". §0/§1 seed candidate criteria
                 from `dross deferred list --target <new-slug> --json` (accept/reword/drop);
                 items whose target is already set are never re-offered.
       depends:  t-1
       contract: if §4 loses the park-in-backlog branch (slug + `dross milestone add` +
                 target stamp), the spec_prompt_test anchor asserting "milestone add" and
                 "target" in §4 fails; if the re-surface seed stops calling
                 `dross deferred list --target`, the list-backed candidate-criteria anchor
                 fails; if the "someday = leave target absent" wording is dropped, the
                 unrouted-only-on-explicit-someday anchor fails; if the don't-re-offer-
                 routed-items guidance is removed, the no-duplicate-routing anchor fails.

  t-3  Add deferred items as a second inbox triage source
       files:    assets/prompts/inbox.md, internal/cmd/inbox_prompt_test.go
       covers:   c-6
       desc:     inbox §1 also runs `dross deferred list --someday --json`; §2 lists those
                 someday/unrouted items alongside inbound board issues and routes each
                 through the same funnel (new phase / milestone-backlog / quick task /
                 dismiss).
       depends:  t-1
       contract: if inbox.md drops the `dross deferred list --someday` source, the
                 inbox_prompt_test anchor for the deferred triage source fails; if someday
                 items aren't sent through the four-way new-phase/backlog/quick/dismiss
                 funnel, the funnel-anchor assertion fails.

## Coverage
- c-1 → t-1
- c-2 → t-2
- c-3 → t-2
- c-4 → t-1 (CLI backing), t-2 (prompt re-surface)
- c-5 → t-2
- c-6 → t-1 (CLI backing), t-3 (prompt source)

## Judgment calls
- Merged the schema field into the CLI task (rejected a standalone schema task): `Target` is a one-line struct add with no value except as input to `dross deferred list`, its only consumer.
- Merged §4 outbound routing and §0/§1 inbound re-surface into one spec.md task (rejected per-region split): both edit a single prompt file plus one prompt-test, well under the 5-file/2-layer split threshold.
- Kept inbox.md as its own task (rejected folding it into the spec.md prompt task): different file and command surface with an independent c-6 trace; folding would blur which prompt delivers c-6.
- Placed both prompt tasks in wave 2 (rejected wave 1): each references `dross deferred list` and the new target field, so they strictly need t-1's surface to exist — a prompt invoking a non-existent command is broken.
- No validate.go change (rejected adding validation logic): the BurntSushi decoder accepts the new key once the struct field exists, so c-1 ("validate passes with/without target") is satisfied by the field add plus a validate-passes test in t-1.
