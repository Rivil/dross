package cmd

import (
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/milestone"
)

// This file audits THIS repo's own routed deferred backlog. survivor-drain
// exists to empty that backlog, and the only thing separating "drained" from
// "deferred again under a new name" is whether anything still points at this
// phase — or past it.
//
// Two failure shapes, both invisible to an eyeball over 60-odd entries:
//
//  1. An entry still routed to survivor-drain and not dismissed. The phase
//     would ship with its own inbox non-empty.
//  2. A survivor-keyed entry re-routed FORWARD, to a phase scheduled after
//     survivor-drain. That looks like disposal and is actually deferral: the
//     builtin dross-survivor-drain rule says the standing backlog only ever
//     shrinks, and a survivor pushed to a later phase shrinks nothing.
//
// The second check is deliberately scoped to SURVIVOR-KEYED entries — text
// carrying a `file.go:line (OP)` identity. Prose deferrals routed forward are
// ordinary roadmap work and always legitimate; this phase's own two
// ([[deferred]] about phase-create slug collisions and adapter mutant
// filtering) are exactly that, and must keep passing while routed to
// phase-create-adoption and mutation-score-truth.

// selfPhase is the phase this audit defends. Named once: it is both the
// forbidden target and the ordering pivot.
const selfPhase = "survivor-drain"

// survivorIdentityPattern matches a survivor identity as it is written into a
// routed deferred item — `dross survivor route` records the subject as
// `<file>:<line> (<OP>)`. Matching the text rather than the entry's Survivor
// field is load-bearing: routing predates that field, and every routed entry in
// this repo carries an empty one, so a field-based check would pass vacuously
// over the exact entries it exists to catch.
var survivorIdentityPattern = regexp.MustCompile(`[^\s:]+\.go:\d+ \([A-Z_]+\)`)

// backlogProblem is one audit finding, always naming the entry's stable handle
// (source phase + index) so it can be addressed with `dross deferred` without
// re-reading every spec.
type backlogProblem struct {
	Source string
	Index  int
	Detail string
}

func (p backlogProblem) String() string {
	return p.Source + "[" + strconv.Itoa(p.Index) + "]: " + p.Detail
}

// auditSurvivorBacklog reports every entry that leaves survivor-drain's backlog
// open, given a repo-wide phase ranking.
//
// A survivor-keyed entry routed to a target with no known rank is flagged too.
// The rank covers every phase in every milestone, so an unrankable target is
// one that cannot be shown to sit BEHIND survivor-drain — and "cannot be shown
// to be behind" is the same risk as forward, just less legible.
//
// active carries the phases of the milestone currently being delivered, and is
// the one forward direction that is disposal rather than deferral. selfPhase
// shipped in an earlier milestone, so EVERY phase that will ever exist ranks
// after it — without this the guard forbids routing any newly found survivor
// to any scheduled phase at all, which is a stricter rule than the builtin
// dross-survivor-drain one it enforces: that rule names routing to "a
// remediation task or scaffolded phase" as a legitimate disposal.
//
// The allowance is deliberately keyed to MEMBERSHIP of the active milestone,
// not to "the target outranks nothing" or "an active milestone exists". Debt
// parked inside the milestone being delivered is bounded and scheduled; debt
// pushed past it is the unbounded kind survivor-drain existed to clean up. It
// follows that closing a milestone with such an entry still open turns the
// guard red again — which is the intended pressure, not a regression.
func auditSurvivorBacklog(entries []deferredEntry, rank map[string]int, active map[string]bool, self string) []backlogProblem {
	var problems []backlogProblem
	selfRank, selfRanked := rank[self]
	for _, e := range entries {
		if e.Dismissed {
			continue // closed out; dismissal is the terminal state
		}
		if e.Target == self {
			problems = append(problems, backlogProblem{e.Source, e.Index,
				"still routed to " + self + " — the phase that exists to drain it cannot ship with its own inbox non-empty"})
			continue
		}
		if e.Target == "" || !survivorIdentityPattern.MatchString(e.Text) {
			continue // someday prose, or a non-survivor deferral: not this rule's business
		}
		targetRank, ok := rank[e.Target]
		if !ok {
			problems = append(problems, backlogProblem{e.Source, e.Index,
				"survivor routed to " + e.Target + ", which has no place in any milestone — it cannot be shown to sit behind " + self})
			continue
		}
		if selfRanked && targetRank > selfRank {
			if active[e.Target] {
				continue // scheduled inside the milestone being delivered: disposal, not deferral
			}
			problems = append(problems, backlogProblem{e.Source, e.Index,
				"survivor re-routed forward to " + e.Target + " — deferral wearing disposal's clothes; the standing backlog only ever shrinks"})
		}
	}
	return problems
}

// milestonePhaseRank ranks every phase across every milestone, earliest first.
//
// It orders milestones numerically rather than reusing milestone.List's order,
// which is a plain string sort — under that, v0.10 precedes v0.2 and the
// resulting ranking would call a v0.2 phase "forward" of a v0.10 one.
func milestonePhaseRank(t *testing.T, root string) map[string]int {
	t.Helper()
	all, err := milestone.LoadAll(root)
	if err != nil {
		t.Fatalf("load milestones: %v", err)
	}
	versions := make([]string, 0, len(all))
	for v := range all {
		versions = append(versions, v)
	}
	sort.Slice(versions, func(i, j int) bool {
		ai, bi := versionKey(versions[i]), versionKey(versions[j])
		if ai[0] != bi[0] {
			return ai[0] < bi[0]
		}
		return ai[1] < bi[1]
	})

	rank := map[string]int{}
	n := 0
	for _, v := range versions {
		for _, ph := range all[v].Phases {
			if _, seen := rank[ph]; seen {
				continue // a phase listed twice keeps its earliest position
			}
			rank[ph] = n
			n++
		}
	}
	return rank
}

// versionKey turns "v1.10" into (1, 10) for numeric comparison. An unparseable
// component sorts as 0, which only ever groups oddly-named milestones at the
// front rather than silently reordering well-formed ones.
func versionKey(v string) [2]int {
	parts := strings.SplitN(strings.TrimPrefix(v, "v"), ".", 2)
	out := [2]int{}
	for i := 0; i < len(parts) && i < 2; i++ {
		out[i], _ = strconv.Atoi(parts[i])
	}
	return out
}

// activeMilestonePhases returns the phases of the milestone currently being
// delivered — the one forward destination auditSurvivorBacklog treats as
// disposal.
//
// Derived from milestone status rather than from state.current_milestone: the
// audit is a property of what the specs on disk say, and reading it from
// mutable session state would let a `dross state set` widen the allowance
// without touching a single milestone.
//
// No active milestone yields an empty set, so the allowance vanishes and the
// guard behaves exactly as it did before it existed. That is the fail-closed
// direction: a repo between milestones has nowhere legitimate to route to.
func activeMilestonePhases(t *testing.T, root string) map[string]bool {
	t.Helper()
	all, err := milestone.LoadAll(root)
	if err != nil {
		t.Fatalf("load milestones: %v", err)
	}
	active := map[string]bool{}
	for _, m := range all {
		if m.Milestone.Status != "active" {
			continue
		}
		for _, ph := range m.Phases {
			active[ph] = true
		}
	}
	return active
}

// TestSurvivorBacklogAuditCatchesItsFailureShapes checks the checker. The repo
// audit below passes today by construction — the backlog was just emptied — so
// without this, a checker that returned nil unconditionally would look exactly
// as green as a working one, forever.
func TestSurvivorBacklogAuditCatchesItsFailureShapes(t *testing.T) {
	// survivor-drain sits at rank 2; earlier is behind, later is forward.
	// active-phase and future-phase are both forward and differ only in
	// membership of the active milestone, which is the whole distinction.
	rank := map[string]int{
		"survivor-lifecycle":   1,
		selfPhase:              2,
		"mutation-score-truth": 3,
		"active-phase":         4,
		"future-phase":         5,
	}
	active := map[string]bool{"active-phase": true}
	const keyed = "survivor internal/cmd/status.go:88 (CONDITIONALS_NEGATION)"

	caught := []struct {
		name   string
		entry  deferredEntry
		detail string
	}{
		{
			name:   "entry still routed to the draining phase",
			entry:  deferredEntry{Source: "completion-state-truth", Index: 1, Text: keyed, Target: selfPhase},
			detail: "still routed to survivor-drain",
		},
		{
			name:   "a prose entry routed to the draining phase counts too",
			entry:  deferredEntry{Source: "dross-repair", Index: 0, Text: "drain the standing backlog", Target: selfPhase},
			detail: "still routed to survivor-drain",
		},
		{
			name:   "survivor re-routed forward",
			entry:  deferredEntry{Source: "survivor-lifecycle", Index: 4, Text: keyed, Target: "mutation-score-truth"},
			detail: "re-routed forward",
		},
		{
			name:   "survivor routed past the active milestone",
			entry:  deferredEntry{Source: "survivor-lifecycle", Index: 9, Text: keyed, Target: "future-phase"},
			detail: "re-routed forward",
		},
		{
			name:   "survivor routed to a phase in no milestone",
			entry:  deferredEntry{Source: "survivor-lifecycle", Index: 5, Text: keyed, Target: "someday-maybe"},
			detail: "no place in any milestone",
		},
	}
	for _, tc := range caught {
		t.Run(tc.name, func(t *testing.T) {
			problems := auditSurvivorBacklog([]deferredEntry{tc.entry}, rank, active, selfPhase)
			if len(problems) != 1 {
				t.Fatalf("got %d problems, want exactly 1: %v", len(problems), problems)
			}
			if problems[0].Source != tc.entry.Source || problems[0].Index != tc.entry.Index {
				t.Errorf("problem addresses %s[%d], want %s[%d] — a finding that does not name the entry is not actionable",
					problems[0].Source, problems[0].Index, tc.entry.Source, tc.entry.Index)
			}
			if !strings.Contains(problems[0].Detail, tc.detail) {
				t.Errorf("detail = %q, want it to mention %q", problems[0].Detail, tc.detail)
			}
		})
	}

	// The shapes that must NOT be flagged. Each is a real entry class in this
	// repo, and flagging any of them would make the audit unusable — which is
	// how a gate gets deleted rather than fixed.
	clean := []struct {
		name  string
		entry deferredEntry
	}{
		{
			name:  "prose routed forward is ordinary roadmap work",
			entry: deferredEntry{Source: selfPhase, Index: 0, Text: "dross phase create coins a duplicate slug", Target: "mutation-score-truth"},
		},
		{
			name:  "survivor routed backward is disposal, not deferral",
			entry: deferredEntry{Source: "completion-state-truth", Index: 2, Text: keyed, Target: "survivor-lifecycle"},
		},
		{
			name:  "a dismissed entry is closed however it was routed",
			entry: deferredEntry{Source: "survivor-lifecycle", Index: 6, Text: keyed, Target: selfPhase, Dismissed: true},
		},
		{
			name:  "survivor routed into the milestone being delivered is scheduled disposal",
			entry: deferredEntry{Source: selfPhase, Index: 8, Text: keyed, Target: "active-phase"},
		},
		{
			name:  "an unrouted survivor is someday, not this phase's backlog",
			entry: deferredEntry{Source: "survivor-lifecycle", Index: 7, Text: keyed},
		},
	}
	for _, tc := range clean {
		t.Run(tc.name, func(t *testing.T) {
			if problems := auditSurvivorBacklog([]deferredEntry{tc.entry}, rank, active, selfPhase); len(problems) != 0 {
				t.Errorf("flagged a legitimate entry: %v", problems)
			}
		})
	}

	// The same entry the clean table just accepted, with no active milestone to
	// belong to. Stated as a pair rather than as its own fixture because the
	// only difference is the allowance itself: a repo between milestones has no
	// scheduled destination, so forward is forward again.
	t.Run("with no active milestone, forward is flagged as before", func(t *testing.T) {
		e := deferredEntry{Source: selfPhase, Index: 8, Text: keyed, Target: "active-phase"}
		problems := auditSurvivorBacklog([]deferredEntry{e}, rank, nil, selfPhase)
		if len(problems) != 1 {
			t.Fatalf("got %d problems, want exactly 1 — the allowance fired without an active milestone: %v", len(problems), problems)
		}
		if !strings.Contains(problems[0].Detail, "re-routed forward") {
			t.Errorf("detail = %q, want it to mention %q", problems[0].Detail, "re-routed forward")
		}
	})
}

// TestSurvivorDrainBacklogClosed is the gate itself, over this repo's real
// specs: survivor-drain's routed backlog is empty and nothing survivor-keyed
// was pushed past it. Enforced by CI rather than by a ship-time eyeball, so the
// backlog cannot quietly refill after the phase closes.
func TestSurvivorDrainBacklogClosed(t *testing.T) {
	root := filepath.Join(repoRootFromTest(t), RootDirName)

	entries, err := collectDeferred(root)
	if err != nil {
		t.Fatalf("collect deferred: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("no deferred entries found across the repo — the audit would pass vacuously")
	}

	rank := milestonePhaseRank(t, root)
	active := activeMilestonePhases(t, root)
	if _, ok := rank[selfPhase]; !ok {
		t.Fatalf("%s is in no milestone's phases array — the forward-routing half of this audit cannot be evaluated", selfPhase)
	}

	for _, p := range auditSurvivorBacklog(entries, rank, active, selfPhase) {
		t.Errorf("%s", p)
	}
}
