package cmd

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Rivil/dross/internal/changes"
	"github.com/Rivil/dross/internal/milestone"
)

// The reap classifier. `dross issue reap` sweeps mirror cards the forward
// lifecycle left behind — cards whose artefact finished before the edge that
// would have closed them existed, or on a run where that edge failed.
//
// The whole design rests on one rule: a close decision is derived from the
// ON-DISK RECORD, never from the card. The board is what is wrong here; asking
// it whether a card should be closed would be asking the patient for the
// diagnosis. A card's own state is read for exactly one purpose — to skip a
// card that is already terminal — and never to decide that a card SHOULD be
// closed.
//
// The verdict is deny-by-default: only a record that positively shows the
// artefact finished yields a close. Everything else is either left alone
// (still live work) or named as unattributable (no record can speak for it).

// reapVerdict is what the classifier concluded about one recorded mirror.
//
// It mirrors backlogVerdict's three-way shape deliberately — the load-bearing
// value is the third one, for the same reason: a card no record explains is NOT
// thereby resolved, and closing on absence of evidence would resolve work
// nobody finished.
type reapVerdict int

const (
	// reapStillOpen — the artefact is live. The card is correctly open.
	reapStillOpen reapVerdict = iota
	// reapStranded — the record shows the artefact finished; the card did not
	// follow.
	reapStranded
	// reapUnattributable — no record can speak for this card. Named in the
	// plan, never closed.
	reapUnattributable
)

// reapCard is one classified mirror. Why names the on-disk record that
// justified the verdict, so a plan line can be audited without re-deriving it.
type reapCard struct {
	Key      string // the tracker's readable issue id
	Lane     string // the board.Board map field this mirror is recorded in
	Terminal string // the lifecycle status the sweep would write
	Why      string // the record that justified the verdict
}

// reapPlan is the classified whole-board inventory.
type reapPlan struct {
	// Cards are stranded: their record shows the artefact done and the card is
	// not yet terminal.
	Cards []reapCard
	// Unattributable are named but never closed — a quick with no completion
	// record, a slug whose phase directory is gone, a backlog key no deferred
	// store explains. Surfaced rather than swallowed, per the survivor-drain
	// habit: an unexplained mirror is a real loose end.
	Unattributable []reapCard
}

// reapLane is one mirror class: the board.Board map field that records it and
// the lifecycle status the sweep writes when it closes one.
//
// The terminal is the SAME state the forward lifecycle writes for that class —
// the locked reap_state decision. A sweep-specific state would make the board
// two histories instead of one, and the state map is what mirror-terminal-state
// just made trustworthy.
type reapLane struct {
	Name     string
	Terminal string
}

// reapLanes is the production lane registry. Its Name values are board.Board's
// map field names, which is what lets the namespace filter validate against
// reflection over that struct rather than against a literal list that would
// silently go stale.
var reapLanes = []reapLane{
	{Name: "Phases", Terminal: "complete"},
	{Name: "Tasks", Terminal: statusTaskComplete},
	{Name: "Milestones", Terminal: "complete"},
	{Name: "Backlog", Terminal: "complete"},
	{Name: "Quicks", Terminal: "complete"},
}

func reapLaneNames() []string {
	names := make([]string, 0, len(reapLanes))
	for _, l := range reapLanes {
		names = append(names, l.Name)
	}
	return names
}

// candidate is a mirror plus the verdict its record produced, before the
// already-terminal filter runs.
type candidate struct {
	card    reapCard
	verdict reapVerdict
}

// classifyReap builds the plan for the named lanes (empty = whole board). It
// issues no write of any kind: not to the tracker, and not to disk. The only
// tracker traffic is the read that skips cards already sitting terminal.
func classifyReap(ctx *boardCtx, namespaces []string) (*reapPlan, error) {
	lanes, err := resolveReapLanes(namespaces)
	if err != nil {
		return nil, err
	}
	var cands []candidate
	for _, lane := range lanes {
		var lc []candidate
		var err error
		switch lane.Name {
		case "Phases":
			lc = classifyPhaseMirrors(ctx, lane)
		case "Tasks":
			lc = classifyTaskMirrors(ctx, lane)
		case "Milestones":
			lc = classifyMilestoneMirrors(ctx, lane)
		case "Backlog":
			lc, err = classifyBacklogMirrors(ctx, lane)
		case "Quicks":
			lc = classifyQuickMirrors(ctx, lane)
		default:
			return nil, fmt.Errorf("lane %q has no classifier", lane.Name)
		}
		if err != nil {
			return nil, err
		}
		cands = append(cands, lc...)
	}
	return buildReapPlan(ctx, cands)
}

// buildReapPlan applies the already-terminal filter and splits the survivors
// into the two plan lists.
//
// The card read happens HERE and nowhere else, which is what keeps the verdict
// record-derived: by the time a card's state is looked at, the decision that it
// ought to close has already been made from disk. A card the tracker already
// holds resolved is dropped entirely — not "closed again" — which is what makes
// a second dry run after a full apply print an honestly empty plan.
func buildReapPlan(ctx *boardCtx, cands []candidate) (*reapPlan, error) {
	plan := &reapPlan{}
	for _, c := range cands {
		if c.verdict == reapStillOpen {
			continue
		}
		done, err := boardIssueIsDone(ctx, c.card.Key)
		if err != nil {
			// A card that cannot be read cannot be shown stranded. Leaving it
			// out of Cards is the deny-by-default reading; it stays visible as
			// unattributable so the run does not silently lose it.
			plan.Unattributable = append(plan.Unattributable, withWhy(c.card, fmt.Sprintf("could not read the card back: %v", err)))
			continue
		}
		if done {
			continue // already terminal — not stranded
		}
		if c.verdict == reapUnattributable {
			plan.Unattributable = append(plan.Unattributable, c.card)
			continue
		}
		plan.Cards = append(plan.Cards, c.card)
	}
	return plan, nil
}

func withWhy(c reapCard, why string) reapCard {
	c.Why = why
	return c
}

// resolveReapLanes maps requested namespace names onto lanes, preserving the
// registry's order so a plan reads the same way every run.
func resolveReapLanes(namespaces []string) ([]reapLane, error) {
	if len(namespaces) == 0 {
		return reapLanes, nil
	}
	want := map[string]bool{}
	for _, n := range namespaces {
		want[strings.ToLower(strings.TrimSpace(n))] = true
	}
	var out []reapLane
	for _, l := range reapLanes {
		if want[strings.ToLower(l.Name)] {
			delete(want, strings.ToLower(l.Name))
			out = append(out, l)
		}
	}
	if len(want) > 0 {
		var unknown []string
		for n := range want {
			unknown = append(unknown, n)
		}
		sort.Strings(unknown)
		return nil, fmt.Errorf("unknown namespace(s) %s; expected one of %s",
			strings.Join(unknown, ", "), strings.Join(reapLaneNames(), ", "))
	}
	return out, nil
}

// --- per-lane classifiers ---

// phaseRecordVerdict is the shared gate for the phase and task lanes: both
// classes of card are closed on the completion of the SAME artefact, so they
// must not be able to disagree about it.
//
// It is narrower than phaseDone on purpose, and the narrowing is not a second
// doneness answer: phaseDone answers "did this phase finish its run?", which
// StatusShipped satisfies. The sweep asks a different question — "has this
// phase reached the state the Phases lane's terminal claims?" — and only
// StatusComplete answers that. A record stuck at shipped is a phase whose
// finalize half has not run; its card belongs in `shipped`, which is a live
// forward state, not a stranded one. Reaping it would announce a completion the
// record does not carry, which c-3 forbids.
func phaseRecordVerdict(root, slug string) (reapVerdict, string) {
	if !phaseDirExists(root, slug) {
		return reapUnattributable, fmt.Sprintf("no phase directory .dross/phases/%s/ — renamed or deleted", slug)
	}
	c, err := changes.Load(changes.FilePath(root, slug), slug)
	if err != nil {
		return reapUnattributable, fmt.Sprintf("phases/%s/changes.json is unreadable: %v", slug, err)
	}
	if c.Status == changes.StatusComplete {
		return reapStranded, fmt.Sprintf("phases/%s/changes.json status=%s", slug, c.Status)
	}
	return reapStillOpen, ""
}

func classifyPhaseMirrors(ctx *boardCtx, lane reapLane) []candidate {
	var out []candidate
	for _, slug := range sortedMapKeys(ctx.board.Phases) {
		issue := ctx.board.Phases[slug]
		if issue == "" {
			continue
		}
		v, why := phaseRecordVerdict(ctx.root, slug)
		out = append(out, candidate{card: reapCard{Key: issue, Lane: lane.Name, Terminal: lane.Terminal, Why: why}, verdict: v})
	}
	return out
}

func classifyTaskMirrors(ctx *boardCtx, lane reapLane) []candidate {
	var out []candidate
	for _, key := range sortedMapKeys(ctx.board.Tasks) {
		link := ctx.board.Tasks[key]
		if link.Issue == "" {
			continue
		}
		slug, _, ok := strings.Cut(key, "/")
		if !ok || slug == "" {
			out = append(out, candidate{
				card:    reapCard{Key: link.Issue, Lane: lane.Name, Terminal: lane.Terminal, Why: fmt.Sprintf("task key %q names no phase", key)},
				verdict: reapUnattributable,
			})
			continue
		}
		v, why := phaseRecordVerdict(ctx.root, slug)
		out = append(out, candidate{card: reapCard{Key: link.Issue, Lane: lane.Name, Terminal: lane.Terminal, Why: why}, verdict: v})
	}
	return out
}

// classifyMilestoneMirrors follows the milestone's own toml status, never the
// epic's state.
//
// The lane is skipped wholesale where the milestones slot does not hold an
// issue at all — a YouTrack version bundle name, an agile board name, a numeric
// forge milestone id. That is not a stranded card being ignored: it is not a
// card. checkMilestoneClosable is the same gate `issue milestone sync --close`
// uses, and it exists because a numeric milestone id shares an id space with
// those backends' issue keys, so addressing it as one would resolve a human's
// issue #7.
func classifyMilestoneMirrors(ctx *boardCtx, lane reapLane) []candidate {
	if err := checkMilestoneClosable(ctx); err != nil {
		return nil
	}
	var out []candidate
	for _, version := range sortedMapKeys(ctx.board.Milestones) {
		id := ctx.board.Milestones[version]
		if id == "" {
			continue
		}
		card := reapCard{Key: id, Lane: lane.Name, Terminal: lane.Terminal}
		m, err := milestone.Load(milestone.FilePath(ctx.root, version))
		if err != nil {
			card.Why = fmt.Sprintf("no milestone toml for %s", version)
			out = append(out, candidate{card: card, verdict: reapUnattributable})
			continue
		}
		if m.Milestone.Status == milestoneStatusComplete {
			card.Why = fmt.Sprintf("milestones/%s.toml status=%s", version, m.Milestone.Status)
			out = append(out, candidate{card: card, verdict: reapStranded})
			continue
		}
		out = append(out, candidate{card: card, verdict: reapStillOpen})
	}
	return out
}

// classifyBacklogMirrors decides each recorded backlog mirror from disk alone.
//
// It answers the same three-way question backlogVerdictFor does and returns the
// same shape, but it cannot reuse that function: backlogVerdictFor resolves a
// routed item by reading its TARGET PHASE'S CARD, and c-3 forbids a close
// decision derived from any card's state. Here the routed branch reads the
// target phase's changes.json instead — the record the card was supposed to be
// mirroring in the first place.
//
// It is also whole-board rather than per-milestone: the sweep has no single
// version in hand, so a key is explained by whatever record owns it (a phase
// directory for `slug:`, a deferred store entry for `someday:`) rather than by
// membership in one milestone's live set.
func classifyBacklogMirrors(ctx *boardCtx, lane reapLane) ([]candidate, error) {
	// collectDeferred, not ensureDeferredIDs: the latter stamps missing ids
	// back into spec.toml, and a dry run must not write to disk either. An
	// id-less entry is still reachable under its legacy positional key.
	deferred, err := collectDeferred(ctx.root)
	if err != nil {
		return nil, err
	}
	byKey := map[string]deferredEntry{}
	for _, d := range deferred {
		if d.ID != "" {
			byKey[deferredBacklogKey(d.ID)] = d
		}
		byKey[legacyDeferredBacklogKey(d.Source, d.Index)] = d
	}

	var out []candidate
	for _, key := range ctx.board.BacklogKeys() {
		issue, ok := ctx.board.BacklogID(key)
		if !ok || issue == "" {
			continue
		}
		v, why := reapBacklogVerdict(ctx, key, byKey)
		out = append(out, candidate{card: reapCard{Key: issue, Lane: lane.Name, Terminal: lane.Terminal, Why: why}, verdict: v})
	}
	return out, nil
}

func reapBacklogVerdict(ctx *boardCtx, key string, deferred map[string]deferredEntry) (reapVerdict, string) {
	if slug, ok := strings.CutPrefix(key, "slug:"); ok {
		// A roadmap slug leaves the backlog the moment it is scaffolded, and
		// the phase directory is the proof — the same evidence backlog sync
		// uses. A slug with no directory was renamed or belongs elsewhere; it
		// is not thereby resolved.
		if phaseDirExists(ctx.root, slug) {
			return reapStranded, fmt.Sprintf("phases/%s/ exists — the slug was scaffolded", slug)
		}
		return reapUnattributable, fmt.Sprintf("no phase directory for slug %q — renamed, or another milestone's", slug)
	}
	d, ok := deferred[key]
	if !ok {
		return reapUnattributable, fmt.Sprintf("no deferred entry explains backlog key %q", key)
	}
	if d.Dismissed {
		// A dismissed idea is a decision, not a loose end.
		return reapStranded, fmt.Sprintf("deferred item %s %d is dismissed", d.Source, d.Index)
	}
	if d.Target != "" {
		v, why := phaseRecordVerdict(ctx.root, d.Target)
		switch v {
		case reapStranded:
			return reapStranded, fmt.Sprintf("routed to %s; %s", d.Target, why)
		case reapUnattributable:
			return reapUnattributable, fmt.Sprintf("routed to %s but %s", d.Target, why)
		}
	}
	return reapStillOpen, ""
}

// classifyQuickMirrors names every quick card and closes none.
//
// A quick task leaves no completion record on disk. state.json's version
// counter is the only trace, and it proves that a LATER bump happened, not that
// this quick finished: the internal digit advances when the next quick starts,
// so ordering evidence would close a quick that was abandoned halfway exactly
// as readily as one that shipped. The lane therefore has a reap path in the
// only honest sense — it is classified, listed, and left for a human.
func classifyQuickMirrors(ctx *boardCtx, lane reapLane) []candidate {
	var out []candidate
	for _, ref := range sortedMapKeys(ctx.board.Quicks) {
		issue := ctx.board.Quicks[ref]
		if issue == "" {
			continue
		}
		out = append(out, candidate{
			card: reapCard{
				Key: issue, Lane: lane.Name, Terminal: lane.Terminal,
				Why: fmt.Sprintf("quick %s has no completion record on disk — close it by hand if it finished", ref),
			},
			verdict: reapUnattributable,
		})
	}
	return out
}

// sortedMapKeys gives every lane a stable walk order, so two runs over the same
// board print the same plan.
func sortedMapKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
