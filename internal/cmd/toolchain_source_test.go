package cmd

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/mod/modfile"
	"golang.org/x/mod/semver"
)

// The Go toolchain is pinned in exactly one place: go.mod's `toolchain`
// directive. ci.yml and release.yml resolve actions/setup-go from it via
// `go-version-file: go.mod`, so the version govulncheck scans in CI, the version
// goreleaser builds with, and the version go.mod declares are one value by
// construction rather than three strings kept in sync by hand. These tests pin
// that wiring (phase supply-chain-currency, criterion c-2): YAML has no mutation
// adapter, so a content guard is the reproducible regression check.

// setupGoMinVersion is the oldest actions/setup-go release that honours the
// `toolchain` directive under `go-version-file` — v7.0.0. v5.x read only the
// `go` directive, so the declared toolchain arrived later via GOTOOLCHAIN=auto
// and the action's installed version was a fiction.
//
// The floor is compared by semver against each pin's trailing `# vX.Y.Z`
// comment rather than against one exact SHA (phase
// dependency-update-automation, criterion c-3): a Dependabot bump rewrites both
// the SHA and the comment, and an exact-SHA constant here would turn every such
// PR red until someone hand-edited this line. The comment is trustworthy
// because action_pins_test.go requires one on every pin, in that shape.
const setupGoMinVersion = "v7.0.0"

func TestToolchainSingleSource(t *testing.T) {
	root := repoRootFromTest(t)
	mod, err := modfile.Parse("go.mod", []byte(readRepoFile(t, "go.mod")), nil)
	if err != nil {
		t.Fatalf("parse go.mod: %v", err)
	}
	if mod.Go == nil || mod.Go.Version == "" {
		t.Fatal("go.mod has no `go` directive")
	}
	if mod.Toolchain == nil || mod.Toolchain.Name == "" {
		t.Fatal("go.mod has no `toolchain` directive — the workflows read the toolchain from it, so without one setup-go falls back to the `go` directive and the pin is a fiction")
	}
	goVer := "v" + mod.Go.Version
	tcVer := "v" + strings.TrimPrefix(mod.Toolchain.Name, "go")
	if !semver.IsValid(goVer) || !semver.IsValid(tcVer) {
		t.Fatalf("go.mod versions are not comparable: go %q toolchain %q", mod.Go.Version, mod.Toolchain.Name)
	}
	if semver.Compare(tcVer, goVer) < 0 {
		t.Fatalf("go.mod toolchain %s is below its go directive %s — the `go` line is the minimum, the toolchain must satisfy it", mod.Toolchain.Name, mod.Go.Version)
	}

	workflows, err := filepath.Glob(filepath.Join(root, ".github", "workflows", "*.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(workflows) == 0 {
		t.Fatal("no .github/workflows/*.yml found")
	}
	steps := 0
	for _, wf := range workflows {
		rel, _ := filepath.Rel(root, wf)
		for _, s := range setupGoSteps(readRepoFile(t, rel)) {
			steps++
			for _, p := range setupGoStepProblems(s) {
				t.Errorf("%s:%d %s", rel, s.line, p)
			}
		}
	}
	if steps == 0 {
		t.Fatal("no actions/setup-go steps found in any workflow — nothing to check")
	}
}

// setupGoStepProblems reports every way one setup-go step breaks the
// single-source wiring. It is split out of the sweep so the rejection branches
// can be driven by synthetic steps: the real workflows all pass, and their pins
// sit exactly on setupGoMinVersion, so the sweep alone never sees a failure.
func setupGoStepProblems(s setupGoStep) []string {
	var problems []string
	switch {
	case !semver.IsValid(s.version):
		problems = append(problems, fmt.Sprintf("actions/setup-go@%s carries version comment %q — a pin's tag must be recorded as a trailing `# vX.Y.Z` comment, which is what makes the floor below comparable", s.sha, s.version))
	case semver.Compare(s.version, setupGoMinVersion) < 0:
		problems = append(problems, fmt.Sprintf("actions/setup-go pinned to %s (%s); want >= %s — the first release that reads go.mod's toolchain directive", s.sha, s.version, setupGoMinVersion))
	}
	if s.goVersion != "" {
		problems = append(problems, fmt.Sprintf("actions/setup-go carries `go-version: %s` — a second toolchain source; go.mod's toolchain directive is the only one", s.goVersion))
	}
	if s.goVersionFile != "go.mod" {
		problems = append(problems, fmt.Sprintf("actions/setup-go has `go-version-file: %q`; want go.mod", s.goVersionFile))
	}
	return problems
}

// setupGoStep is one `uses: actions/setup-go@<sha>` step, the version recorded
// in its trailing comment, and the toolchain keys found in its `with:` block.
type setupGoStep struct {
	line          int
	sha           string
	version       string
	goVersion     string
	goVersionFile string
}

// setupGoSteps walks a workflow by line, collecting every actions/setup-go step
// and the `go-version` / `go-version-file` keys in the `with:` block that
// follows it. Line-based on purpose: the repo carries no YAML dependency, and a
// step's `with:` block ends at the next line indented at or above the step's
// `- uses:` dash. Comment lines and trailing `# ...` comments are dropped
// before keys are read, so a comment mentioning `go-version:` is not read as
// the key — but the `uses:` line's own comment is retained, since that is where
// the pinned version lives.
func setupGoSteps(workflow string) []setupGoStep {
	var (
		steps []setupGoStep
		cur   *setupGoStep
		depth = -1
	)
	for i, raw := range strings.Split(workflow, "\n") {
		line, comment := splitYAMLComment(raw)
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " "))
		if cur != nil && indent <= depth {
			steps = append(steps, *cur)
			cur = nil
		}
		if rest, ok := strings.CutPrefix(trimmed, "- uses: actions/setup-go@"); ok {
			cur = &setupGoStep{line: i + 1, sha: strings.TrimSpace(rest), version: comment}
			depth = indent
			continue
		}
		if cur == nil {
			continue
		}
		if v, ok := strings.CutPrefix(trimmed, "go-version-file:"); ok {
			cur.goVersionFile = strings.Trim(strings.TrimSpace(v), `'"`)
		} else if v, ok := strings.CutPrefix(trimmed, "go-version:"); ok {
			cur.goVersion = strings.Trim(strings.TrimSpace(v), `'"`)
		}
	}
	if cur != nil {
		steps = append(steps, *cur)
	}
	return steps
}

func stripYAMLComment(line string) string {
	value, _ := splitYAMLComment(line)
	return value
}

// TestSetupGoStepScanner pins the line scanner against inline fixtures so the
// sweep above cannot go green by parsing nothing.
func TestSetupGoStepScanner(t *testing.T) {
	const wf = `jobs:
  a:
    steps:
      - uses: actions/checkout@aaaa  # v4
      - uses: actions/setup-go@1111  # v7.0.0
        with:
          # go-version: 'comment, not a key'
          go-version-file: go.mod
          cache: true
      - name: go version
        run: go version
  b:
    steps:
      - uses: actions/setup-go@2222
        if: x != ''
        with:
          go-version: '1.25.13'
          cache: true
      - uses: actions/setup-go@3333
`
	got := setupGoSteps(wf)
	want := []setupGoStep{
		{line: 5, sha: "1111", version: "v7.0.0", goVersionFile: "go.mod"},
		{line: 14, sha: "2222", goVersion: "1.25.13"},
		{line: 19, sha: "3333"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d steps %+v, want %d", len(got), got, len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("step %d: got %+v, want %+v", i, got[i], want[i])
		}
	}
}

// TestSetupGoStepProblems drives each rejection branch of setupGoStepProblems
// with a step that must fail, and pins the floor as inclusive and
// pin-agnostic with steps that must pass. Each want entry is a substring that
// identifies one problem, in the order the checks run; the count must match
// exactly, so a dropped check and a spurious extra one both fail.
func TestSetupGoStepProblems(t *testing.T) {
	const (
		invalidComment = "a pin's tag must be recorded"
		belowFloor     = "want >= " + setupGoMinVersion
		goVersionKey   = "a second toolchain source"
		versionFile    = "want go.mod"
	)
	ok := func(version string) setupGoStep {
		return setupGoStep{line: 1, sha: "1111", version: version, goVersionFile: "go.mod"}
	}
	for _, tc := range []struct {
		name string
		step setupGoStep
		want []string
	}{
		{"floor is inclusive", ok("v7.0.0"), nil},
		{"bot bump above the floor", ok("v7.1.0"), nil},
		{"below the floor", ok("v6.0.0"), []string{belowFloor}},
		{"no version comment", ok(""), []string{invalidComment}},
		{"comment without the v prefix", ok("7.0.0"), []string{invalidComment}},
		{"go-version key", setupGoStep{line: 1, sha: "1111", version: "v7.0.0", goVersion: "1.25.13", goVersionFile: "go.mod"}, []string{goVersionKey}},
		{"no go-version-file", setupGoStep{line: 1, sha: "1111", version: "v7.0.0"}, []string{versionFile}},
		{"go-version-file not go.mod", setupGoStep{line: 1, sha: "1111", version: "v7.0.0", goVersionFile: "go.work"}, []string{versionFile}},
		{"every fault at once", setupGoStep{line: 1, sha: "1111", version: "v6.0.0", goVersion: "1.25.13"}, []string{belowFloor, goVersionKey, versionFile}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := setupGoStepProblems(tc.step)
			if len(got) != len(tc.want) {
				t.Fatalf("got %d problems %q, want %d matching %q", len(got), got, len(tc.want), tc.want)
			}
			for i, w := range tc.want {
				if !strings.Contains(got[i], w) {
					t.Errorf("problem %d = %q, want it to contain %q", i, got[i], w)
				}
			}
		})
	}
}
