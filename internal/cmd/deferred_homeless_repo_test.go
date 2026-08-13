package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// homelessMarkers are distinctive fragments of the two findings this phase was
// meant to take off "Open loops" and into the deferred backlog. They are matched
// case-insensitively against the handoff's Open-loops section.
var homelessMarkers = map[string]string{
	"test-suite hermeticity gap":          "hermeticity",
	"verify survivor-vocabulary mismatch": "unclassified_in_scope",
}

// TestHandoffParksNoHomelessFinding is a SELF-RETIRING guard, not a presence
// check. It asserts an invariant about how findings are recorded: neither
// finding may sit as an "Open loops" bullet, because `dross deferred add` now
// exists and a parked bullet is the failure mode this phase removed.
//
// Deliberately NOT pinned to the filed items (e.g. `target = mutation-score-truth`):
// that would go red the day mutation-score-truth consumes the item, punishing a
// correct action. Phrased this way, a finding parked instead of filed turns it
// red, while a filed-then-consumed item leaves it green forever.
func TestHandoffParksNoHomelessFinding(t *testing.T) {
	path := filepath.Join(repoRootFromTest(t), ".dross", "handoff.md")
	body, err := os.ReadFile(path)
	if err != nil {
		// handoff.md is machine-local and gitignored (.gitignore:12), so it is
		// simply absent in CI. Skip VISIBLY rather than passing: a version that
		// silently returns on a missing file is a vacuous green — a gate that
		// reports success having checked nothing, which is the exact shape of
		// failure this phase exists to stop shipping.
		t.Skipf("no .dross/handoff.md at %s (gitignored, absent on a fresh clone) — nothing to check", path)
	}

	loops := openLoopsSection(string(body))
	for name, marker := range homelessMarkers {
		if strings.Contains(strings.ToLower(loops), marker) {
			t.Errorf("the %s finding is parked as an Open-loops bullet; file it with `dross deferred add` instead", name)
		}
	}
}

// openLoopsSection returns the text of handoff.md's "## Open loops" section, or
// "" when the document has none. Scoping to that section matters: the Thread and
// Next sections legitimately narrate a finding after it has been filed, and
// matching those would make the guard un-retirable.
func openLoopsSection(body string) string {
	lines := strings.Split(body, "\n")
	var out []string
	in := false
	for _, ln := range lines {
		trimmed := strings.TrimSpace(ln)
		if strings.HasPrefix(trimmed, "## ") {
			in = strings.EqualFold(trimmed, "## Open loops")
			continue
		}
		if in {
			out = append(out, ln)
		}
	}
	return strings.Join(out, "\n")
}
