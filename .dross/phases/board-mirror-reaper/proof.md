# Live proof — `dross issue reap` against the DRO board

Evidence for **c-1** (a single sweep closes every stranded mirror class at
source), **c-4** (a re-run after a full apply is a no-op) and the concrete half
of **c-7** (dross-authored mirrors no board.json namespace links are
classified, not skipped).

Everything below was observed against the live YouTrack board at
`https://issues.rivil.dev`, project `DRO`, on **2026-08-21**.

## Binary

Per rule r-01 the installed binary can be stale versus source, and every line
of reap is Go written earlier in this phase, so the run began with
`make build && make install`.

The sweep was re-built and re-installed three times, because the live run
itself surfaced three defects (below). The **final** whole-board state was
produced by:

```
$ dross --version
dross 0.1.0.0 (commit 6186cbf, built 2026-08-21T09:30:44Z)
```

`6186cbf` is this phase's `fix(board-mirror-reaper): an unscaffolded roadmap
slug is still open`. The intermediate binaries are named per lane below where
they matter, because the first 24 cards were closed by an earlier one and then
**reverted and re-swept** rather than left inconsistent.

| binary | commit | what it produced |
| --- | --- | --- |
| first | `60294bb` | the initial Phases / Milestones / Tasks apply (24 cards) — **reverted** |
| second | `bf2fdea` | the re-sweep of those 24, plus Backlog (63) |
| final | `6186cbf` | the reclassification of DRO-72 / 158 / 159 and the final clean re-run |

## Per-lane counts

Every count is the plan's own footer before the apply, and the tracker's own
`State.isResolved` verdict after it.

| lane | stranded before | reaped | after | terminal written | evidence |
| --- | --- | --- | --- | --- | --- |
| Phases | 2 | 2 | 0 | `complete` → `Verified` | `phases/<slug>/changes.json status=complete` |
| Milestones | 2 | 2 | 0 | `complete` → `Verified` | `milestones/<v>.toml status=complete` |
| Tasks | 20 | 20 | 0 | `task-complete` → `Verified` | the phase's `changes.json` |
| Backlog | 63 | 63 | 0 | `complete` → `Verified` | phase dir exists / target phase complete / dismissed |
| Quicks | 0 | 0 (never auto-closed) | 0 | — | no completion record exists for a quick |
| **total** | **87** | **87** | **0** | | |

No lane's after-count is short of its before-count. Each apply exited 0 and
each per-lane re-run printed `no stranded mirrors`.

### Cards closed, by issue id

**Phases (2)** — `DRO-23` `DRO-24`

**Milestones (2)** — `DRO-1` `DRO-7`

**Tasks (20)** — `DRO-121` `DRO-122` `DRO-123` `DRO-126` `DRO-127` `DRO-128`
`DRO-130` `DRO-131` `DRO-136` `DRO-137` `DRO-138` `DRO-139` `DRO-140` `DRO-141`
`DRO-143` `DRO-144` `DRO-145` `DRO-146` `DRO-147` `DRO-148`

**Backlog (63)** — `DRO-15` `DRO-39` `DRO-40` `DRO-41` `DRO-42` `DRO-43`
`DRO-44` `DRO-45` `DRO-46` `DRO-47` `DRO-50` `DRO-51` `DRO-52` `DRO-53`
`DRO-59` `DRO-60` `DRO-61` `DRO-63` `DRO-64` `DRO-65` `DRO-66` `DRO-67`
`DRO-68` `DRO-69` `DRO-70` `DRO-71` `DRO-73` `DRO-74` `DRO-75` `DRO-76`
`DRO-77` `DRO-78` `DRO-79` `DRO-80` `DRO-81` `DRO-83` `DRO-85` `DRO-86`
`DRO-90` `DRO-91` `DRO-92` `DRO-97` `DRO-98` `DRO-99` `DRO-100` `DRO-101`
`DRO-102` `DRO-103` `DRO-104` `DRO-105` `DRO-107` `DRO-108` `DRO-109`
`DRO-110` `DRO-113` `DRO-114` `DRO-115` `DRO-116` `DRO-119` `DRO-120`
`DRO-124` `DRO-157` `DRO-162`

Spot-checks read back off the tracker after the final sweep — state **and** the
`dross/status:` label, which is the fidelity the second defect below was about:

```
DRO-23   State=Verified isResolved=True   dross/status:complete
DRO-1    State=Verified isResolved=True   dross/status:complete
DRO-121  State=Verified isResolved=True   dross/status:task-complete
DRO-148  State=Verified isResolved=True   dross/status:task-complete
```

### The two quicks — closed by hand, never inferred

c-1 requires the quicks be *named for manual close rather than inferred
closed*, and they were: both appeared in every plan's unattributable list and
neither was ever written by the sweep.

| id | quick | closed by hand because |
| --- | --- | --- |
| `DRO-6` | `1.2.8.1` — disable git background maintenance in `internal/cmd` tests | `internal/cmd/cmd_test.go:149` sets `gc.auto 0`, with the comment describing the race it fixes |
| `DRO-11` | ship-body-routed-survivor-noise | the survivor-drain / survivor-lifecycle phases (`dbd29bb`, `6c642cc`) landed the grouping |

Both were closed with a direct tracker write, not by the sweep. That is the
prescribed path: version monotonicity proves another bump happened, not that a
quick finished, so no amount of on-disk evidence could have let the classifier
close them.

## The six unlinked mirrors (c-7)

`DRO-33` `DRO-36` `DRO-37` `DRO-38` `DRO-95` `DRO-96` are in **no board.json
namespace** — verified directly against the file:

```
DRO-33 linked in board.json: False      DRO-38 linked in board.json: False
DRO-36 linked in board.json: False      DRO-95 linked in board.json: False
DRO-37 linked in board.json: False      DRO-96 linked in board.json: False
```

All six carry the `dross` marker and so are reached by the marker sweep — the
link-only inventory cannot see any of them. What the sweep found is that **all
six were already resolved** (`State=Verified`, `isResolved=true`), closed by the
preceding `mirror-terminal-state` phase's backlog reconcile. Their recovered
identity differs:

| id | identity labels | recovered as | outcome |
| --- | --- | --- | --- |
| `DRO-33` | `dross/deferred:2d0d0092…`, `dross/target:cache-budget-prune` | routed backlog item | already terminal → correctly not stranded |
| `DRO-95` | `dross/deferred:704efc03…` | someday backlog item | already terminal → correctly not stranded |
| `DRO-96` | `dross/deferred:069d6104…` | someday backlog item | already terminal → correctly not stranded |
| `DRO-36` | *(marker only)* | unclassifiable | already terminal → not a loose end |
| `DRO-37` | *(marker only)* | unclassifiable | already terminal → not a loose end |
| `DRO-38` | *(marker only)* | unclassifiable | already terminal → not a loose end |

So c-7's concrete claim holds in the direction that matters — none of the six
is *skipped*; each is reached, classified, and correctly excluded — but the six
were **not** stranded by the time this phase ran. The count in the spec was
taken before `mirror-terminal-state` shipped.

`DRO-36/37/38` were the trigger for the first defect below: they carry the
marker with no identity label AND were already resolved, and the unclassifiable
list was built before the already-terminal filter, so all three were being
reported as open loose ends on every run — including a run immediately after a
full sweep.

## Cards left open, and why

24 cards are open on the board after the sweep. Every one is correctly open.
The census is by the State bundle's `isResolved` flag, **not** YouTrack's
`resolved` timestamp — this instance has no workflow rule stamping the
timestamp, so a `#Unresolved` query returns resolved cards and is not a usable
oracle here. (That is the same reason `youtrackIssue.toIssue` ORs the per-state
flag in.)

**Human-filed — never touched, never named (5):**
`DRO-3` `DRO-4` `DRO-5` `DRO-133` `DRO-134`

**dross-authored, artefact genuinely unfinished (5):**

| id | why it is correctly open |
| --- | --- |
| `DRO-72` | routed to `watch-pr-ci-status`, still on v0.9's roadmap, unscaffolded |
| `DRO-158` | `slug:reentry-signal-truth`, still on **v1.5's** roadmap, unscaffolded |
| `DRO-159` | routed to `reentry-signal-truth` — same |
| `DRO-160` | routed to `board-mirror-reaper` — *this* phase, not yet complete |
| `DRO-161` | a `someday:` item, not dismissed |

**This phase's own cards, in flight (14):**
`DRO-163` (phase) and `DRO-164`–`DRO-176` (its tasks).

## Final re-run (c-4)

Whole board, no `--namespace`, after everything above:

```
$ dross issue reap
no stranded mirrors — every card matches its record
$ echo $?
0
```

Zero cards classified, nothing written, exit 0.

## Defects the live run found

The synthetic fixtures could not have produced any of these; each came from
real board data. All three were fixed, tested and committed **before** the
final sweep, and the cards written by the earlier binaries were reverted and
re-swept rather than left inconsistent.

### 1. A resolved unclassifiable card was reported as a loose end — `60294bb`

`DRO-36/37/38` carry the marker with no identity label and were already
resolved. The unclassifiable list was assembled before the already-terminal
filter, so all three were named on every run forever. That is the inert
re-listing the survivor-drain habit exists to stop, and it would have kept a
post-sweep plan from ever reading clean.

### 2. A reaped card kept a stale `dross/status:` label — `bf2fdea`

Comparing a reaped card with a forward-closed sibling:

```
reaped   DRO-121  State=Verified  dross/status:task-in-review   <-- stale
forward  DRO-150  State=Verified  dross/status:task-complete
```

The sweep wrote the tracker state and left the label alone. That breaks the
locked `reap_state` decision in the way that actually matters: the point of
reaping to the forward path's own terminal is that the board reads as **one**
history, and a permanently distinguishable card defeats it. It could not
self-heal either — once resolved, a card is no longer stranded, so no later
sweep would revisit it.

24 cards had already been written by `60294bb` when this surfaced. They were
restored and re-swept:

- the Tasks run (20 cards) was reversed with `dross issue reap --undo`, which
  returned each card to its own journalled column and label — verified live:
  `DRO-121` and `DRO-148` both read `State=In Review, isResolved=False` with
  their original label set intact;
- the Phases (2) and Milestones (2) runs could not be reached that way, because
  `--undo` reverses **only the last run** (locked `undo_shape`). Those four were
  restored by writing back the exact `prior_state` the ledger had recorded —
  `DRO-23`/`DRO-24` → `In Progress`, `DRO-1`/`DRO-7` → `Submitted` — and then
  re-swept by the CLI.

That limitation is by design, and the live run is what made its shape concrete:
a multi-lane sweep run as separate applies is reversible one lane at a time,
newest first, and no further.

### 3. An unscaffolded roadmap slug was called unattributable — `6186cbf`

The backlog verdict decided a `slug:` mirror on one question — does the phase
directory exist? — which cannot tell *renamed or lost* from *on a roadmap and
not built yet*. Both are an absent directory. So `DRO-158`, `DRO-159` and
`DRO-72` — live v1.5 and v0.9 backlog — were reported as unexplained mirrors on
every run.

An unscaffolded slug now splits on whether it is still named in some
milestone's `phases` array. After the fix all three drop out of the plan
entirely, which is what turned the final re-run from
`0 stranded … 3 unattributable` into `no stranded mirrors`.

## Deltas from the spec's numbers

c-1 was written against a board snapshot taken before `mirror-terminal-state`
shipped, and it counts slightly differently from what the sweep found:

| c-1 said | observed | why |
| --- | --- | --- |
| 88 record-backed stranded | 87 | 63 backlog, not 64 — `DRO-160` is routed into *this* phase and is correctly open |
| 64 backlog | 63 | as above |
| 2 quicks unattributable | 2 | exact |
| 20 task, 2 phase, 2 epic | 20, 2, 2 | exact |
| 8 correctly-open cards | 10 pre-existing | plus 14 of this phase's own cards, which did not exist when the spec was written |
| 6 unlinked mirrors classified | 6 reached, all already resolved | closed by the preceding phase; none was skipped |
