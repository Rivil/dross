package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// output_golden_test.go pins the exact stdout bytes of every command whose
// output crosses encoding/json or BurntSushi/toml, as goldens under
// testdata/cli_surface/output/. The cmd-exec-baseline-drain phase moves those
// codecs out of internal/cmd (c-8) and routes CLI output through one rendering
// package; these goldens, minted from the pre-move tree, are what "no
// user-visible change" (c-7) means for the bytes. A structural assertion —
// parses, carries key X — passes a MarshalIndent→Marshal swap or a dropped
// trailing newline; only a byte-for-byte golden catches either.
//
// The fixture is hand-written with fixed timestamps rather than built through
// `dross init`, whose project.toml carries today's date. Temp paths are
// normalised to <ROOT> and <HOME>, so two runs — and two machines — mint the
// same bytes. The stack cases read a user-dir profile the fixture owns, not an
// embedded one, so editing a shipped profile never churns these goldens.

// outputGoldenDir is the goldens' subdirectory under cliSurfaceDir.
const outputGoldenDir = "output"

// outputFixtureFiles is the fixture repo, keyed by path relative to the repo
// dir ("~/" for the isolated HOME). The deferred text and the plan title carry
// "<&>" so json.Marshal's HTML escaping is part of what the goldens pin.
var outputFixtureFiles = map[string]string{
	".dross/project.toml": `[project]
  name = "golden"
  version = "1.2.3.0"
  description = "fixture for the output goldens"
  created = "2026-01-02"

[stack]
  languages = ["go"]

[runtime]
  mode = "native"
  test_command = "go test ./..."

[repo]
  git_main_branch = "main"
  commit_convention = "conventional"
`,
	".dross/state.json": `{
  "version": "1.2.3.0",
  "current_milestone": "v1.1",
  "current_phase": "p1",
  "current_phase_status": "planned",
  "last_activity": "2026-01-02T03:04:05Z",
  "last_action": "plan locked: p1",
  "history": [
    {"at": "2026-01-02T03:04:05Z", "action": "plan locked: p1"}
  ]
}
`,
	".dross/milestones/v1.1.toml": `phases = ["p1"]

[milestone]
version = "v1.1"
title = "Friction pass"
status = "active"

[scope]
success_criteria = ["the logs stop repeating themselves"]
non_goals = ["a rewrite"]
`,
	".dross/phases/p1/spec.toml": `[phase]
id = "p1"
title = "First phase"
milestone = "v1.1"

[[criteria]]
id = "c-1"
text = "it works"

[[deferred]]
id = "0123456789abcdef"
text = "a <routed> & escaped idea"
why = "later"
target = "p2"

[[deferred]]
id = "fedcba9876543210"
text = "a someday idea"
why = "no home yet"
`,
	".dross/phases/p1/plan.toml": `[phase]
id = "p1"

[[task]]
id = "t-1"
wave = 1
title = "Do the <first> & thing"
files = ["a.go", "a_test.go"]
description = "desc"
covers = ["c-1"]
test_contract = ["fails if broken"]
status = "done"

[[task]]
id = "t-2"
wave = 2
title = "Do the second thing"
files = ["b.go"]
covers = ["c-1"]
depends_on = ["t-1"]
`,
	".dross/phases/p1/changes.json": `{
  "phase": "p1",
  "base": "main",
  "tasks": {
    "t-1": {
      "files": ["a.go", "a_test.go"],
      "commit": "abc1234",
      "completed_at": "2026-01-02T03:04:05Z",
      "landmarks": [{"feature": "golden", "symbol": "A", "loc": "a.go:1", "what": "does <a> & b"}]
    }
  }
}
`,
	".dross/profile.toml": `[dimensions.pace]
rating = "fast"
confidence = "medium"
directive = "move quickly"
`,
	"~/.claude/dross/profile.toml": `generated = "2026-01-02"
source = "fixture"

[dimensions.verbosity]
rating = "terse"
confidence = "high"
directive = "keep it short"
`,
	"~/.claude/dross/defaults.toml": `[remote_defaults]
provider = "github"
`,
	"~/.claude/dross/profiles/golden.toml": `id    = "golden"
title = "Golden"

[signals]
  files    = ["golden.mod"]
  priority = 1

[runtime.test]
  run = "golden test"

[[tools]]
  name    = "goldlint"
  kind    = "analyzer"
  core    = true
  install = "go install example.com/goldlint@latest"
[[tools]]
  name    = "goldscan"
  kind    = "scanner"
  install = "go install example.com/goldscan@latest"

[loadout]
  mcp_tools   = ["golden-mcp — look things up"]
  guardrails  = ["Check the <signature> & the docs."]
  conventions = ["One command per file."]
`,
}

// outputFixture writes outputFixtureFiles plus a tests.json carrying
// provenanceFixture, isolates HOME, chdirs into the repo, and returns the
// normaliser that maps its temp paths to <ROOT> and <HOME>.
func outputFixture(t *testing.T) func(string) string {
	t.Helper()
	dir := t.TempDir()
	home := t.TempDir()
	for rel, body := range outputFixtureFiles {
		base := dir
		if strings.HasPrefix(rel, "~/") {
			base, rel = home, strings.TrimPrefix(rel, "~/")
		}
		mustWrite(t, filepath.Join(base, filepath.FromSlash(rel)), body)
	}
	seedTests(t, filepath.Join(dir, ".dross"), "p1", provenanceFixture())
	t.Setenv("HOME", home)
	chdir(t, dir)
	return pathNormaliser(map[string]string{dir: "<ROOT>", home: "<HOME>"})
}

// pathNormaliser replaces each temp dir — and its symlink-resolved form, which
// FindRoot may report on macOS where /var is /private/var — with its token.
// Resolved forms are replaced first so the shorter spelling never splits one.
func pathNormaliser(tokens map[string]string) func(string) string {
	var olds, news []string
	for path, token := range tokens {
		if real, err := filepath.EvalSymlinks(path); err == nil && real != path {
			olds, news = append(olds, real), append(news, token)
		}
	}
	for path, token := range tokens {
		olds, news = append(olds, path), append(news, token)
	}
	return func(s string) string {
		for i := range olds {
			s = strings.ReplaceAll(s, olds[i], news[i])
		}
		return s
	}
}

// outputGoldenCases are the commands whose stdout crosses a codec, each with
// the argv that reaches it. Names are the golden file stems.
var outputGoldenCases = []struct {
	name  string
	build func() *cobra.Command
	args  []string
}{
	{"project_show", Project, []string{"show"}},
	{"project_show_json", Project, []string{"show", "--json"}},
	{"project_get_multi", Project, []string{"get", "runtime.test_command", "project.name"}},
	{"milestone_show", Milestone, []string{"show", "v1.1"}},
	{"milestone_show_json", Milestone, []string{"show", "v1.1", "--json"}},
	{"defaults_show", Defaults, []string{"show"}},
	{"defaults_show_json", Defaults, []string{"show", "--json"}},
	{"profile_show", Profile, []string{"show"}},
	{"profile_show_json", Profile, []string{"show", "--json"}},
	{"stack_show", Stack, []string{"show", "golden"}},
	{"stack_show_json", Stack, []string{"show", "golden", "--json"}},
	{"state_show", State, []string{"show"}},
	// Argument order, not sorted key order: "version" sorts after
	// "current_phase", so a map-marshalled object would flip them.
	{"state_get_multi", State, []string{"get", "version", "current_phase"}},
	{"changes_show_json", Changes, []string{"show", "p1", "--json"}},
	{"verify_scope_json", Verify, []string{"scope", "p1", "--json"}},
	{"task_list_json", Task, []string{"list", "p1", "--json"}},
	{"task_show_json", Task, []string{"show", "p1", "t-2", "--json"}},
	{"deferred_list_json", Deferred, []string{"list", "--json"}},
	{"watch_json", Watch, []string{"--json"}},
	{"reentry", Reentry, nil},
}

// TestOutputGoldens: every codec-crossing command prints exactly its pre-move
// bytes over the fixed fixture.
func TestOutputGoldens(t *testing.T) {
	pkgDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range outputGoldenCases {
		t.Run(tc.name, func(t *testing.T) {
			norm := outputFixture(t)
			var out string
			if err := runCmdCapturing(t, &out, tc.build(), tc.args...); err != nil {
				t.Fatalf("%s %v: %v", tc.name, tc.args, err)
			}
			checkOutputGolden(t, pkgDir, tc.name, norm(out))
		})
	}
}

// TestStackLoadoutGolden pins `stack loadout` with an empty PATH, so every
// tool renders missing on a laptop and on the remote runner alike — the
// baseline the phase's stack.LookPath move is measured against.
func TestStackLoadoutGolden(t *testing.T) {
	pkgDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	norm := outputFixture(t)
	t.Setenv("PATH", "")
	var out string
	if err := runCmdCapturing(t, &out, Stack(), "loadout", "golden"); err != nil {
		t.Fatalf("stack loadout: %v", err)
	}
	checkOutputGolden(t, pkgDir, "stack_loadout", norm(out))
}

// TestShipJSONGolden pins `ship --json` over the mock forge flow. It needs a
// real repo, branch and forge, so it runs on shipFixture rather than the
// shared output fixture; the mock's url and number are fixed.
func TestShipJSONGolden(t *testing.T) {
	pkgDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	dir := shipFixture(t, "https://forge.example/me/p.git")
	shipMockFlow(t, dir)
	var out string
	if err := runCmdCapturing(t, &out, Ship(), "--json"); err != nil {
		t.Fatalf("ship --json: %v", err)
	}
	checkOutputGolden(t, pkgDir, "ship_json", out)
}

// checkOutputGolden checks got against output/<name>.txt. The golden path is
// anchored at the package dir captured before the fixture chdir'd away.
func checkOutputGolden(t *testing.T, pkgDir, name, got string) {
	t.Helper()
	path := filepath.Join(pkgDir, cliSurfaceDir, outputGoldenDir, name+".txt")
	if err := goldenCheck(path, got, os.Getenv(goldenUpdateEnv) == "1"); err != nil {
		t.Fatal(err)
	}
}

// TestOutputFixtureNormalisesPaths: a normalised output carries neither temp
// dir, in either spelling — otherwise two runs could not mint equal bytes.
func TestOutputFixtureNormalisesPaths(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	norm := pathNormaliser(map[string]string{a: "<ROOT>", b: "<HOME>"})
	in := "# " + a + "/.dross/project.toml\n" + b + "/.claude\n"
	if real, err := filepath.EvalSymlinks(a); err == nil {
		in += real + "/x\n"
	}
	got := norm(in)
	if strings.Contains(got, a) || strings.Contains(got, b) {
		t.Fatalf("temp path survived normalisation:\n%s", got)
	}
	if !strings.HasPrefix(got, "# <ROOT>/.dross/project.toml\n<HOME>/.claude\n") {
		t.Fatalf("normalised output = %q", got)
	}
}
