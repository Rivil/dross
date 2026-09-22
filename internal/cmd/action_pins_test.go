package cmd

import (
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Every `uses:` in every workflow is pinned to a 40-hex commit SHA and carries a
// trailing `# vX.Y.Z` comment. The SHA is the security property — a tag is
// mutable, so `@v4` runs whatever the action's owner (or whoever compromises
// them) last pushed to it. The comment is the reviewability property: a bare
// 40-hex diff line says nothing about what moved, and this repo's currency
// updates now arrive as Dependabot PRs whose entire content is that line.
// Phase dependency-update-automation, criterion c-4.
//
// Line-based on purpose, matching toolchain_source_test.go and
// workflow_run_expressions_test.go: no YAML dependency in a repo whose stack
// decision is a single static binary.

var (
	// actionSHA is a full git commit SHA — the only ref shape a pin may carry.
	actionSHA = regexp.MustCompile(`^[0-9a-f]{40}$`)
	// actionVersionComment is the accepted comment shape: a three-part semver
	// tag. A major-only or two-part comment (`# v5`, `# v4.2`) is rejected —
	// every pin in ci.yml and release.yml carries three parts today, and an
	// action that publishes only a major tag is then a deliberate hand edit
	// rather than a silent pass.
	actionVersionComment = regexp.MustCompile(`^v\d+\.\d+\.\d+$`)
)

// actionPin is one `uses:` step: the action, the ref it is pinned to, and the
// version recorded in the trailing comment.
type actionPin struct {
	line    int
	action  string
	ref     string
	version string
}

// actionPins walks a workflow by line and collects every `uses:` step, whether
// written as a list item (`- uses: ...`) or as a key inside one. Full-line
// comments are skipped, so a commented-out `uses:` is not swept.
func actionPins(workflow string) []actionPin {
	var pins []actionPin
	for i, raw := range strings.Split(workflow, "\n") {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		rest, ok := strings.CutPrefix(trimmed, "- uses:")
		if !ok {
			rest, ok = strings.CutPrefix(trimmed, "uses:")
		}
		if !ok {
			continue
		}
		value, comment := splitYAMLComment(rest)
		action, ref, _ := strings.Cut(strings.Trim(strings.TrimSpace(value), `'"`), "@")
		pins = append(pins, actionPin{line: i + 1, action: action, ref: ref, version: comment})
	}
	return pins
}

// splitYAMLComment separates a line's content from its trailing `# ...`
// comment. A full-line comment yields empty content. YAML requires whitespace
// before a `#` for it to open a comment, so the split is on " #" — a `#` inside
// a scalar stays part of the value.
func splitYAMLComment(line string) (value, comment string) {
	if strings.HasPrefix(strings.TrimSpace(line), "#") {
		return "", strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "#"))
	}
	i := strings.Index(line, " #")
	if i < 0 {
		return line, ""
	}
	return line[:i], strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(line[i+1:]), "#"))
}

// workflowActionPins returns every pin across .github/workflows/*.yml, keyed by
// the workflow's repo-relative path.
func workflowActionPins(t *testing.T) map[string][]actionPin {
	t.Helper()
	root := repoRootFromTest(t)
	workflows, err := filepath.Glob(filepath.Join(root, ".github", "workflows", "*.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(workflows) == 0 {
		t.Fatal("no .github/workflows/*.yml found")
	}
	out := map[string][]actionPin{}
	for _, wf := range workflows {
		rel, _ := filepath.Rel(root, wf)
		out[rel] = actionPins(readRepoFile(t, rel))
	}
	return out
}

// TestWorkflowActionsArePinned sweeps every workflow: a mutable tag ref is a
// supply-chain hole, and a SHA without its version comment is an unreviewable
// bump.
func TestWorkflowActionsArePinned(t *testing.T) {
	total := 0
	for rel, pins := range workflowActionPins(t) {
		total += len(pins)
		for _, p := range pins {
			if !actionSHA.MatchString(p.ref) {
				t.Errorf("%s:%d `uses: %s@%s` is not pinned to a 40-hex commit SHA — a tag or short ref can be moved under us", rel, p.line, p.action, p.ref)
			}
			if !actionVersionComment.MatchString(p.version) {
				t.Errorf("%s:%d `uses: %s@%s` has version comment %q; want a trailing `# vX.Y.Z` — without it a bot's SHA bump is an unreviewable diff line", rel, p.line, p.action, p.ref, p.version)
			}
		}
	}
	if total == 0 {
		t.Fatal("no `uses:` steps found in any workflow — the sweep cannot pass over an empty set")
	}
}

// TestActionPinScanner pins the line parser against an inline fixture so the
// sweep above cannot go green by failing to match `uses:` lines at all.
func TestActionPinScanner(t *testing.T) {
	const wf = `jobs:
  a:
    steps:
      # - uses: actions/commented-out@dead  # v9.9.9
      - uses: actions/checkout@11bd71901bbe5b1630ceea73d27597364c9af683  # v4.2.2
      - name: two-part comment
        uses: actions/setup-node@49933ea5288caeca8642d1e84afbd3f7d6820020  # v4.4
      - uses: 'actions/no-comment@abc123'
      - uses: actions/tag-pinned@v4  # v4.0.0
      - run: echo "uses: not-a-step@sha  # v1.2.3"
`
	got := actionPins(wf)
	want := []actionPin{
		{line: 5, action: "actions/checkout", ref: "11bd71901bbe5b1630ceea73d27597364c9af683", version: "v4.2.2"},
		{line: 7, action: "actions/setup-node", ref: "49933ea5288caeca8642d1e84afbd3f7d6820020", version: "v4.4"},
		{line: 8, action: "actions/no-comment", ref: "abc123", version: ""},
		{line: 9, action: "actions/tag-pinned", ref: "v4", version: "v4.0.0"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d pins %+v, want %d", len(got), got, len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("pin %d: got %+v, want %+v", i, got[i], want[i])
		}
	}

	// The fixture's last three pins are each a shape the sweep must reject.
	for _, tc := range []struct {
		pin      actionPin
		sha, ver bool
	}{
		{want[0], true, true},
		{want[1], true, false},  // `# v4.4` — two-part comment
		{want[2], false, false}, // short ref, no comment
		{want[3], false, true},  // tag ref
	} {
		if gotSHA := actionSHA.MatchString(tc.pin.ref); gotSHA != tc.sha {
			t.Errorf("%s@%s: SHA match = %v, want %v", tc.pin.action, tc.pin.ref, gotSHA, tc.sha)
		}
		if gotVer := actionVersionComment.MatchString(tc.pin.version); gotVer != tc.ver {
			t.Errorf("%s@%s: version comment %q match = %v, want %v", tc.pin.action, tc.pin.ref, tc.pin.version, gotVer, tc.ver)
		}
	}
}
