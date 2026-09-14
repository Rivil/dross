package cmd

import (
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

// setupGoMinSHA is the oldest actions/setup-go commit that honours the
// `toolchain` directive under `go-version-file` — the v7.0.0 tag. v5.x read only
// the `go` directive, so the declared toolchain arrived later via GOTOOLCHAIN=auto
// and the action's installed version was a fiction. Bump this deliberately (and
// the workflow pins with it) when adopting a newer tag; a workflow pinned to any
// other SHA fails the test.
const setupGoMinSHA = "b7ad1dad31e06c5925ef5d2fc7ad053ef454303e" // v7.0.0

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
			if s.sha != setupGoMinSHA {
				t.Errorf("%s:%d actions/setup-go pinned to %q; want %s (v7.0.0 — the first tag that reads go.mod's toolchain directive)", rel, s.line, s.sha, setupGoMinSHA)
			}
			if s.goVersion != "" {
				t.Errorf("%s:%d actions/setup-go carries `go-version: %s` — a second toolchain source; go.mod's toolchain directive is the only one", rel, s.line, s.goVersion)
			}
			if s.goVersionFile != "go.mod" {
				t.Errorf("%s:%d actions/setup-go has `go-version-file: %q`; want go.mod", rel, s.line, s.goVersionFile)
			}
		}
	}
	if steps == 0 {
		t.Fatal("no actions/setup-go steps found in any workflow — nothing to check")
	}
}

// setupGoStep is one `uses: actions/setup-go@<sha>` step and the toolchain
// keys found in its `with:` block.
type setupGoStep struct {
	line          int
	sha           string
	goVersion     string
	goVersionFile string
}

// setupGoSteps walks a workflow by line, collecting every actions/setup-go step
// and the `go-version` / `go-version-file` keys in the `with:` block that
// follows it. Line-based on purpose: the repo carries no YAML dependency, and a
// step's `with:` block ends at the next line indented at or above the step's
// `- uses:` dash. Comment lines and trailing `# ...` comments are dropped
// first so a comment mentioning `go-version:` is not read as the key.
func setupGoSteps(workflow string) []setupGoStep {
	var (
		steps []setupGoStep
		cur   *setupGoStep
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
		if rest, ok := strings.CutPrefix(trimmed, "- uses: actions/setup-go@"); ok {
			cur = &setupGoStep{line: i + 1, sha: strings.TrimSpace(rest)}
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
	if strings.HasPrefix(strings.TrimSpace(line), "#") {
		return ""
	}
	if i := strings.Index(line, " #"); i >= 0 {
		return line[:i]
	}
	return line
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
		{line: 5, sha: "1111", goVersionFile: "go.mod"},
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
