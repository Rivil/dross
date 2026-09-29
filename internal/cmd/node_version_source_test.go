package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Node's version is pinned in exactly one place: a version file that every
// actions/setup-node step reads through `node-version-file:`. It lives outside
// .github/workflows/ on purpose (locked decision bump_reach): the weekly
// pin-currency job bumps it with a GITHUB_TOKEN, and GitHub rejects a
// GITHUB_TOKEN push that changes a workflow file — an inline `node-version:`
// would be a pin the bot could see stale but never move. Phase
// run-block-pin-currency, criterion c-10. Line-based like
// toolchain_source_test.go: the repo carries no YAML dependency.

// exactNodeVersion is the only content a Node version file may hold: one
// X.Y.Z. `24`, `24.x`, `lts/*` or `node` let the runner pick the version.
var exactNodeVersion = regexp.MustCompile(`^\d+\.\d+\.\d+$`)

// setupNodeStep is one `uses: actions/setup-node@<sha>` step and the Node
// version keys found in the `with:` block that follows it.
type setupNodeStep struct {
	line            int
	nodeVersion     string
	hasNodeVersion  bool
	nodeVersionFile string
}

// setupNodeSteps walks a workflow by line, collecting every actions/setup-node
// step and its node-version / node-version-file keys. A step's keys end at the
// next line indented at or above its `- uses:` dash; comments are dropped
// before keys are read.
func setupNodeSteps(workflow string) []setupNodeStep {
	var (
		steps []setupNodeStep
		cur   *setupNodeStep
		depth = -1
	)
	for i, raw := range strings.Split(workflow, "\n") {
		line := stripYAMLComment(raw)
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " "))
		if cur != nil && indent <= depth {
			steps = append(steps, *cur)
			cur = nil
		}
		if strings.HasPrefix(trimmed, "- uses: actions/setup-node@") {
			cur = &setupNodeStep{line: i + 1}
			depth = indent
			continue
		}
		if cur == nil {
			continue
		}
		if v, ok := strings.CutPrefix(trimmed, "node-version-file:"); ok {
			cur.nodeVersionFile = strings.Trim(strings.TrimSpace(v), `'"`)
		} else if v, ok := strings.CutPrefix(trimmed, "node-version:"); ok {
			cur.nodeVersion, cur.hasNodeVersion = strings.Trim(strings.TrimSpace(v), `'"`), true
		}
	}
	if cur != nil {
		steps = append(steps, *cur)
	}
	return steps
}

// nodeVersionFileProblem reports why the version file at rel under root is not
// one exact X.Y.Z line, or "" when it is.
func nodeVersionFileProblem(root, rel string) string {
	b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		return fmt.Sprintf("node-version-file %s cannot be read: %v", rel, err)
	}
	var lines []string
	for _, l := range strings.Split(string(b), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			lines = append(lines, l)
		}
	}
	if len(lines) != 1 || !exactNodeVersion.MatchString(lines[0]) {
		return fmt.Sprintf("node-version-file %s holds %q; want exactly one X.Y.Z line — anything looser lets the runner pick Node", rel, lines)
	}
	return ""
}

// setupNodeStepProblems reports every way one setup-node step breaks the
// single-source wiring against the tree at root.
func setupNodeStepProblems(root string, s setupNodeStep) []string {
	var problems []string
	if s.hasNodeVersion {
		problems = append(problems, fmt.Sprintf("actions/setup-node carries `node-version: %s` — an inline pin in a workflow file the pin-currency bot cannot push; read it from a file via node-version-file:", s.nodeVersion))
	}
	if s.nodeVersionFile == "" {
		return append(problems, "actions/setup-node has no node-version-file: — the Node version has no single source")
	}
	if p := nodeVersionFileProblem(root, s.nodeVersionFile); p != "" {
		problems = append(problems, p)
	}
	return problems
}

func TestNodeVersionSingleSource(t *testing.T) {
	root := repoRootFromTest(t)
	steps := 0
	for _, rel := range githubYAMLFiles(t, root) {
		if !strings.HasPrefix(rel, ".github/workflows/") {
			continue
		}
		for _, s := range setupNodeSteps(readTreeFile(t, root, rel)) {
			steps++
			for _, p := range setupNodeStepProblems(root, s) {
				t.Errorf("%s:%d %s", rel, s.line, p)
			}
		}
	}
	if steps == 0 {
		t.Fatal("no actions/setup-node steps found in any workflow — nothing to check")
	}

	const good = ".node-version"
	for _, tc := range []struct {
		name, step, file string
		want             string // substring of an expected problem; "" = passes
	}{
		{"file with one exact version", "node-version-file: .node-version", "24.19.0\n", ""},
		{"inline node-version", "node-version: '24.19.0'", "24.19.0\n", "inline pin"},
		{"inline beside a file", "node-version-file: .node-version\n          node-version: '24.19.0'", "24.19.0\n", "inline pin"},
		{"major only", "node-version-file: .node-version", "24\n", "exactly one X.Y.Z"},
		{"x range", "node-version-file: .node-version", "24.x\n", "exactly one X.Y.Z"},
		{"lts alias", "node-version-file: .node-version", "lts/*\n", "exactly one X.Y.Z"},
		{"two lines", "node-version-file: .node-version", "24.19.0\n24.20.0\n", "exactly one X.Y.Z"},
		{"missing file", "node-version-file: .nvmrc", "24.19.0\n", "cannot be read"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wf := "jobs:\n  a:\n    steps:\n      - uses: actions/setup-node@49933ea5288caeca8642d1e84afbd3f7d6820020  # v4.4.0\n        with:\n          " + tc.step + "\n      - run: npm ci --ignore-scripts\n"
			fixture := writeTreeFiles(t, map[string]string{good: tc.file})
			got := setupNodeSteps(wf)
			if len(got) != 1 {
				t.Fatalf("scanned %d setup-node steps, want 1: %+v", len(got), got)
			}
			problems := setupNodeStepProblems(fixture, got[0])
			switch {
			case tc.want == "" && len(problems) != 0:
				t.Fatalf("want no problems, got %q", problems)
			case tc.want != "" && !strings.Contains(strings.Join(problems, "\n"), tc.want):
				t.Fatalf("want a problem containing %q, got %q", tc.want, problems)
			}
		})
	}
}
