package cmd

import (
	"strings"
	"testing"
)

// The board's terminal close moved out of ship's prompt into `dross phase
// complete`, reap learned to create a completed phase's missing cards, and a
// routed item's mirror now closes only on a disposition record. A reader who
// learns the board from README or ARCHITECTURE.md would otherwise keep running
// the retired ship close lines, or keep expecting routed cards to close when
// their destination ships. These tests pin both documents to the new shape.

// readmeDeferredVerbs returns the verbs README's `dross deferred {…}` row
// lists, so a test asks whether one verb is listed rather than pinning the
// whole brace list and breaking on every verb added after it.
func readmeDeferredVerbs(t *testing.T) []string {
	t.Helper()
	doc := docText(t, "README.md")
	const head = "dross deferred {"
	at := strings.Index(doc, head)
	if at < 0 {
		t.Fatal("README.md has no `dross deferred {…}` row")
	}
	rest := doc[at+len(head):]
	end := strings.Index(rest, "}")
	if end < 0 {
		t.Fatal("README.md's `dross deferred {…}` row never closes its brace list")
	}
	return strings.Split(rest[:end], ",")
}

func hasVerb(verbs []string, verb string) bool {
	for _, v := range verbs {
		if v == verb {
			return true
		}
	}
	return false
}

// TestReadmeDocumentsBoardFinalize: README names the absorb verb, says
// `dross phase complete` closes the board cards, and says reap creates the
// cards a completed phase is missing.
func TestReadmeDocumentsBoardFinalize(t *testing.T) {
	readme := docText(t, "README.md")
	for _, want := range []struct{ phrase, why string }{
		{"dross deferred absorb", "the absorb verb records what a routed item's mirror closes on"},
		{"dross phase complete finalizes the board", "complete is where the board's terminal close now runs"},
		{"every task card closes at task-complete and the phase card at complete", "the cards complete closes, at their terminal statuses"},
		{"dross issue phase finalize <id>", "the retry when the board was unreachable at completion"},
		{"creates the cards a completed phase is missing", "reap's catch-up of completed phases' missing cards"},
	} {
		if !strings.Contains(readme, want.phrase) {
			t.Errorf("README.md never says %q — %s", want.phrase, want.why)
		}
	}
	if !hasVerb(readmeDeferredVerbs(t), "absorb") {
		t.Error("README's `dross deferred {…}` row does not list the `absorb` verb")
	}
}

// TestArchitectureDocumentsBoardFinalize: ARCHITECTURE.md no longer gives ship
// the terminal closes, no longer keeps `dross phase complete` off the board,
// no longer closes a routed item on its destination shipping, and places the
// Phases/Tasks terminal emissions in finalize.go rather than the prompts.
func TestArchitectureDocumentsBoardFinalize(t *testing.T) {
	arch := docText(t, "ARCHITECTURE.md")
	for _, stale := range []struct{ phrase, why string }{
		{"no board coupling", "dross phase complete finalizes the board"},
		{"status task-complete --close", "the task cards' close is FinalizePhase's, not a ship prompt line"},
		{"complete --close at finalize", "the phase card's close is FinalizePhase's, not a ship prompt line"},
		{"a routed item whose target phase shipped", "a routed item closes on its disposition record, never on its destination finishing"},
		{"terminal emission at a real call site in the prompt corpus", "the Phases and Tasks terminal emissions live in finalize.go"},
	} {
		if strings.Contains(arch, stale.phrase) {
			t.Errorf("ARCHITECTURE.md still says %q — %s", stale.phrase, stale.why)
		}
	}

	start := strings.Index(arch, "### issue board sync")
	if start < 0 {
		t.Fatal("ARCHITECTURE.md has no Issue board sync section")
	}
	section := arch[start:]
	if end := strings.Index(section[len("### issue board sync"):], "### "); end >= 0 {
		section = section[:len("### issue board sync")+end]
	}
	for _, want := range []struct{ phrase, why string }{
		{"dross phase complete writes the terminal close itself", "complete owns the board's terminal close"},
		{"dross issue phase finalize <id>", "the named retry for a board complete could not reach"},
		{"internal/boardsync/finalize.go", "the Phases and Tasks terminal emissions' home"},
		{"a routed item closes on its disposition record, never on its destination finishing", "backlog sync's routed-item rule"},
		{"the sweep also creates what is missing", "reap's catch-up of completed phases' missing cards"},
	} {
		if !strings.Contains(section, want.phrase) {
			t.Errorf("ARCHITECTURE.md's Issue board sync section never says %q — %s", want.phrase, want.why)
		}
	}
}
