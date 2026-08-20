package forge

import "testing"

// Lifecycle state distinctness, per provider.
//
// c-3 asks for a UAT state "distinct from in-progress and from shipped": a
// phase awaiting a verdict is neither being worked nor delivered, and
// collapsing it into either is exactly what makes a board stop reflecting where
// work actually is. c-2 makes the same shape of claim one level down — picking
// a task and committing it must land in different columns.
//
// Nothing was asserting the distinctness itself. YouTrack's "UAT" is pinned by
// the bundle test only because that value has to be CREATED; Jira's
// defaultJiraStateMap["uat"] was keyed but its value unread, so collapsing it
// into "In Progress" would have satisfied every existing test while quietly
// deleting the state c-3 is about.
//
// What is asserted here is the RELATIONSHIP, not either provider's strings. A
// project is free to rename its columns — that is what [board].state_map is for
// — and a guard written against "UAT" or "In Review" would fight that. A guard
// written against "these two statuses must not be the same column" holds
// whatever the columns are called.

// distinctPairs are the lifecycle statuses that must never share a board
// column, each with the reason a reader needs when it fails.
var distinctPairs = []struct {
	a, b string
	why  string
}{
	{"uat", "in-progress", "a phase awaiting a verdict is not a phase still being worked (c-3)"},
	{"uat", "shipped", "a phase awaiting a verdict is not a phase already delivered (c-3)"},
	{"task-in-review", "task-in-progress", "a committed task is not a task still being written (c-2)"},
	{"task-complete", "task-in-review", "a task whose phase has shipped is not a task still awaiting the phase's verdict (c-2)"},
	{"task-complete", "task-in-progress", "a task whose phase has shipped is not a task still being written (c-2)"},
}

// TestStateMapsKeepTheLifecycleStatesDistinct gates both default maps.
//
// Both are checked, independently: a collapse in one provider's map is a real
// bug for whoever uses that tracker, and it is the provider whose value was
// never read that the collapse would hide in.
func TestStateMapsKeepTheLifecycleStatesDistinct(t *testing.T) {
	for _, m := range []struct {
		provider string
		varName  string
		states   map[string]string
	}{
		{"jira", "defaultJiraStateMap", defaultJiraStateMap},
		{"youtrack", "defaultYouTrackStateMap", defaultYouTrackStateMap},
	} {
		t.Run(m.provider, func(t *testing.T) {
			for _, p := range distinctPairs {
				av, aok := m.states[p.a]
				bv, bok := m.states[p.b]
				// A missing key would make the comparison below vacuous —
				// two absent statuses both read as "". The divergence guard
				// in internal/cmd owns presence; this says so out loud rather
				// than passing on nothing.
				if !aok || !bok {
					t.Errorf("%s is missing %s — the distinctness of %q and %q cannot be checked at all",
						m.varName, missingOf(p.a, aok, p.b, bok), p.a, p.b)
					continue
				}
				if av == bv {
					t.Errorf("%s maps both %q and %q to %q — %s", m.varName, p.a, p.b, av, p.why)
				}
			}
		})
	}
}

// missingOf names whichever side of a pair the map does not hold, for the error
// above.
func missingOf(a string, aok bool, b string, bok bool) string {
	switch {
	case !aok && !bok:
		return "both " + a + " and " + b
	case !aok:
		return a
	default:
		return b
	}
}

// TestTerminalTaskStateResolves pins the two values the task lane's terminal
// status maps to. The distinctness table above says what must NOT collide;
// this says what the default actually is, so a rename shows up as a decision
// rather than as a silently different column.
//
// Both are done-category states on their tracker, which is what makes the close
// read-back in closeBoardIssue able to verify a task card at all.
func TestTerminalTaskStateResolves(t *testing.T) {
	if got, ok := resolveYouTrackState("task-complete", nil); !ok || got != "Verified" {
		t.Errorf(`resolveYouTrackState("task-complete", nil) = %q,%v want "Verified",true`, got, ok)
	}
	if got, ok := resolveJiraState("task-complete", nil); !ok || got != "Done" {
		t.Errorf(`resolveJiraState("task-complete", nil) = %q,%v want "Done",true`, got, ok)
	}
}
