package cmd

import (
	"fmt"
	"os"
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

// githubYAMLFiles returns the repo-relative path of every workflow
// (.github/workflows/*.{yml,yaml}) and composite action
// (.github/actions/*/action.{yml,yaml}) under root. A composite action's
// steps run inside the calling job, so every rule a workflow step obeys binds
// an action step too — a sweep that reads only workflows would let a pin or an
// injection hide one directory over.
func githubYAMLFiles(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	for _, pattern := range []string{
		".github/workflows/*.yml", ".github/workflows/*.yaml",
		".github/actions/*/action.yml", ".github/actions/*/action.yaml",
	} {
		matches, err := filepath.Glob(filepath.Join(root, filepath.FromSlash(pattern)))
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range matches {
			rel, _ := filepath.Rel(root, m)
			out = append(out, filepath.ToSlash(rel))
		}
	}
	return out
}

// readTreeFile reads a repo-relative file under root.
func readTreeFile(t *testing.T, root, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(b)
}

// writeTreeFiles lays files out under a fresh temp dir and returns its path.
func writeTreeFiles(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, body := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// actionPinViolations sweeps every workflow and composite action under root
// and returns one message per unpinned `uses:`, plus how many `uses:` it saw.
// A local `./` action is admitted: it is this repo's own file at the checked-out
// commit, so there is no tag to move and no version to record.
func actionPinViolations(t *testing.T, root string) (violations []string, total int) {
	t.Helper()
	for _, rel := range githubYAMLFiles(t, root) {
		for _, p := range actionPins(readTreeFile(t, root, rel)) {
			total++
			if strings.HasPrefix(p.action, "./") {
				continue
			}
			if !actionSHA.MatchString(p.ref) {
				violations = append(violations, fmt.Sprintf("%s:%d `uses: %s@%s` is not pinned to a 40-hex commit SHA — a tag or short ref can be moved under us", rel, p.line, p.action, p.ref))
			}
			if !actionVersionComment.MatchString(p.version) {
				violations = append(violations, fmt.Sprintf("%s:%d `uses: %s@%s` has version comment %q; want a trailing `# vX.Y.Z` — without it a bot's SHA bump is an unreviewable diff line", rel, p.line, p.action, p.ref, p.version))
			}
		}
	}
	return violations, total
}

// TestWorkflowActionsArePinned sweeps every workflow and composite action: a
// mutable tag ref is a supply-chain hole, and a SHA without its version comment
// is an unreviewable bump.
func TestWorkflowActionsArePinned(t *testing.T) {
	violations, total := actionPinViolations(t, repoRootFromTest(t))
	for _, v := range violations {
		t.Error(v)
	}
	if total == 0 {
		t.Fatal("no `uses:` steps found in any workflow — the sweep cannot pass over an empty set")
	}

	// The sweep reaches composite actions: a tag-pinned remote action inside
	// one fails, a local ./ action beside it does not.
	t.Run("composite action fixture", func(t *testing.T) {
		root := writeTreeFiles(t, map[string]string{
			".github/actions/x/action.yml": `runs:
  using: composite
  steps:
    - uses: actions/x@v4  # v4.0.0
    - uses: ./.github/actions/y
`,
		})
		violations, total := actionPinViolations(t, root)
		if total != 2 {
			t.Fatalf("swept %d uses: in the fixture action, want 2", total)
		}
		if len(violations) != 1 || !strings.Contains(violations[0], "actions/x@v4") {
			t.Fatalf("want exactly one violation naming actions/x@v4, got %q", violations)
		}
	})
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

// compositeRunStepsWithoutShell returns the 1-based line of every step in a
// composite action that has a `run:` but no `shell:`. Line-based like the
// sweeps above: the steps list is the `- ` items under `steps:`, and a step's
// own keys sit two columns past its dash. Block-scalar content is skipped, so
// a `shell:` inside a script is not mistaken for the key.
func compositeRunStepsWithoutShell(action string) []int {
	var (
		missing          []int
		inSteps          bool
		stepsIndent      int
		itemIndent       = -1
		stepLine         int
		hasRun, hasShell bool
		blockDepth       = -1
	)
	flush := func() {
		if stepLine > 0 && hasRun && !hasShell {
			missing = append(missing, stepLine)
		}
		stepLine, hasRun, hasShell = 0, false, false
	}
	for i, raw := range strings.Split(action, "\n") {
		trimmed := strings.TrimSpace(raw)
		indent := len(raw) - len(strings.TrimLeft(raw, " "))
		if blockDepth >= 0 {
			if trimmed == "" || indent > blockDepth {
				continue
			}
			blockDepth = -1
		}
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if !inSteps {
			if trimmed == "steps:" {
				inSteps, stepsIndent, itemIndent = true, indent, -1
			}
			continue
		}
		item := strings.HasPrefix(trimmed, "- ")
		if itemIndent < 0 && item && indent >= stepsIndent {
			itemIndent = indent
		}
		switch {
		case item && indent == itemIndent:
			flush()
			stepLine = i + 1
		case indent <= itemIndent || itemIndent < 0:
			flush()
			inSteps = false
			continue
		}
		body := strings.TrimSpace(strings.TrimPrefix(trimmed, "- "))
		keyCol := indent + len(trimmed) - len(body)
		key, value, ok := strings.Cut(body, ":")
		if !ok {
			continue
		}
		value, _ = splitYAMLComment(value)
		if keyCol == itemIndent+2 {
			switch key {
			case "run":
				hasRun = true
			case "shell":
				hasShell = true
			}
		}
		if isBlockScalarIndicator(strings.TrimSpace(value)) {
			blockDepth = keyCol
		}
	}
	flush()
	return missing
}

// TestCompositeRunStepsDeclareShell sweeps every composite action. A composite
// `run:` step with no `shell:` is rejected when the action loads, at the start
// of whichever job uses it — nothing a local test or a PR's other checks would
// otherwise see before that job goes red.
func TestCompositeRunStepsDeclareShell(t *testing.T) {
	root := repoRootFromTest(t)
	actions := 0
	for _, rel := range githubYAMLFiles(t, root) {
		if !strings.HasPrefix(rel, ".github/actions/") {
			continue
		}
		actions++
		for _, line := range compositeRunStepsWithoutShell(readTreeFile(t, root, rel)) {
			t.Errorf("%s:%d composite run: step has no shell: — GitHub refuses to load the action", rel, line)
		}
	}
	if actions == 0 {
		t.Fatal("no .github/actions/*/action.yml found — nothing swept")
	}

	t.Run("fixture", func(t *testing.T) {
		const action = `runs:
  using: composite
  steps:
    - name: has shell
      shell: bash
      run: |
        echo "shell: is not a key in here"
    - name: no shell
      run: |
        echo "shell: bash"
    - uses: ./.github/actions/y
    - run: echo ok
      shell: bash
`
		if got := compositeRunStepsWithoutShell(action); len(got) != 1 || got[0] != 8 {
			t.Fatalf("steps without shell: got lines %v, want [8]", got)
		}
	})
}
