package cmd

import (
	"strings"
	"testing"
)

// homelessMarkers are distinctive fragments of the two findings the
// deferred-add-command phase was meant to take off "Open loops" and into the
// deferred backlog. They are matched case-insensitively against the handoff's
// Open-loops section.
var homelessMarkers = map[string]string{
	"test-suite hermeticity gap":          "hermeticity",
	"verify survivor-vocabulary mismatch": "unclassified_in_scope",
}

// parksHomelessFinding reports the findings parked as Open-loops bullets in the
// given handoff body. A finding sitting there instead of filed with
// `dross deferred add` is the failure mode deferred-add-command removed.
func parksHomelessFinding(body string) []string {
	loops := strings.ToLower(openLoopsSection(body))
	var found []string
	for name, marker := range homelessMarkers {
		if strings.Contains(loops, marker) {
			found = append(found, name)
		}
	}
	return found
}

// TestParksHomelessFindingScopesToOpenLoops pins the detection an earlier
// version of this test applied to the real repo's .dross/handoff.md.
//
// That version read a gitignored, machine-local file: present on the author's
// box, absent on a fresh checkout, so it Skipf'd in CI. A skip is a test that
// does not run rather than one that passes — the whole guard was vacuous
// exactly where it was meant to hold. The logic worth pinning is the SCOPING
// (Open loops is a finding parked; Thread and Next legitimately narrate a
// finding after it has been filed), and that is testable on every host.
func TestParksHomelessFindingScopesToOpenLoops(t *testing.T) {
	cases := []struct {
		name string
		body string
		want bool
	}{
		{
			name: "parked as an open loop",
			body: "## Thread\nwork\n\n## Open loops\n- [ ] the test-suite HERMETICITY gap is still unfiled\n",
			want: true,
		},
		{
			name: "narrated in Thread after filing",
			body: "## Thread\nfiled the hermeticity finding with dross deferred add\n\n## Open loops\n- [ ] something else\n",
			want: false,
		},
		{
			name: "narrated in Next after filing",
			body: "## Next\n- [ ] consume the unclassified_in_scope item\n\n## Open loops\n- [ ] unrelated\n",
			want: false,
		},
		{
			name: "no open loops section at all",
			body: "## Thread\nhermeticity and unclassified_in_scope both mentioned\n",
			want: false,
		},
		{
			name: "clean handoff",
			body: "## Open loops\n- [ ] check the build cache volume\n",
			want: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parksHomelessFinding(tc.body)
			if (len(got) > 0) != tc.want {
				t.Errorf("parksHomelessFinding = %v, want parked=%v", got, tc.want)
			}
		})
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
