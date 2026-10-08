package cmd

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/project"
)

// End-to-end proof that every project.toml writer dross ships is lossless:
// each subtest runs the real cobra command in-process against a seeded
// project.toml and asserts that the set of changed lines is exactly the
// targeted key(s), and that every comment and unmodeled key survived.
//
// The seams above this — the patcher, the differ, Save — each carry their
// own tests in internal/project; this file is the one that reddens if a
// command sidesteps Save, or if a future writer round-trips the struct.

// losslessFixture is a hand-edited project.toml: a leading comment, a
// mid-table comment, an inline `# why`, hand-added notes under two
// array-of-tables elements, an unmodeled [custom] table and an unmodeled
// key in [remote], a multi-line array, three lanes (one commented).
const losslessFixture = `# project.toml — hand-edited; every comment and note here must survive

[project]
  name = "fixture"
  version = "1.7.5.0"  # why: bumped by the phase workflow
  # what the project is for
  description = "a fixture project"
  created = "2026-06-19"

[stack]
  languages = ["go"]

  [[stack.locked]]
    choice = "Go, distributed as a single static binary CLI"
    why = "No runtime dependencies for users; one install artifact."
    locked_at = "2026-06-19"
    note = "hand-added under locked"

[runtime]
  mode = "native"
  # the suite is slow on the laptop; prefer the remote runner
  test_command = "go test ./..."

  [[runtime.test_lane]]
    name = "cmd"
    match = ["internal/cmd/**"]
    command = "go test ./internal/cmd/..."

  [[runtime.test_lane]]
    # the project package is the fast lane
    name = "project"
    match = ["internal/project/**"]
    command = "go test ./internal/project/..."

  [[runtime.test_lane]]
    name = "ship"
    match = ["internal/ship/**"]
    command = "go test ./internal/ship/..."

[repo]
  layout = "single"
  git_main_branch = "main"
  commit_convention = "conventional"
  squash_merge = true

[remote]
  url = "https://example.invalid/rivil/fixture"
  provider = "github"
  mirror = "https://example.invalid/mirror/fixture"

[board]
  provider = "youtrack"
  base_url = "https://issues.example.invalid"
  auth_env = "YOUTRACK_TOKEN"
  project = "FIX"

[paths]

[env]

[goals]
  core_value = "A low-overhead loop for AI-assisted development."
  non_goals = [
    "Not a general-purpose tool", # marketing is out of scope
    "No feature PRs that don't match the author's workflow",
  ]

[custom]
  anything = 1

[[competition]]
  name = "one"
  url = "https://one.invalid"

[[competition]]
  name = "two"
  note = "hand-added"
`

// mustSurvive is what c-2 demands after every write: the two hand-added
// notes, the unmodeled table, the unmodeled key, and every comment.
var mustSurvive = []string{
	`    note = "hand-added under locked"`,
	`  note = "hand-added"`,
	`[custom]`,
	`  anything = 1`,
	`  mirror = "https://example.invalid/mirror/fixture"`,
	`# why: bumped by the phase workflow`,
	`# project.toml — hand-edited; every comment and note here must survive`,
	`  # what the project is for`,
	`  # the suite is slow on the laptop; prefer the remote runner`,
	`    # the project package is the fast lane`,
	`    "Not a general-purpose tool", # marketing is out of scope`,
}

// seedLossless lays out a repo root with the fixture project.toml, a
// state.json (state set version needs one) and a go.mod so stack apply
// resolves the go profile. It chdirs into it and returns the project.toml
// path.
func seedLossless(t *testing.T, src string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/fixture\n\ngo 1.25\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(dir, ".dross")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, project.File)
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "state.json"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	chdir(t, dir)
	return path
}

func lineSet(s string) []string {
	s = strings.TrimSuffix(s, "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// changedLineSets is a multiset diff of before vs after, position-blind:
// a one-line edit is one removed and one added; identical text is neither.
func changedLineSets(before, after string) (added, removed []string) {
	count := map[string]int{}
	for _, l := range lineSet(before) {
		count[l]--
	}
	for _, l := range lineSet(after) {
		count[l]++
	}
	for _, l := range lineSet(after) {
		if count[l] > 0 {
			added = append(added, l)
			count[l]--
		}
	}
	for _, l := range lineSet(before) {
		if count[l] < 0 {
			removed = append(removed, l)
			count[l]++
		}
	}
	return added, removed
}

// onlyLinesChanged asserts the added/removed line sets are exactly the
// targeted ones.
func onlyLinesChanged(t *testing.T, before, after string, wantAdded, wantRemoved []string) {
	t.Helper()
	added, removed := changedLineSets(before, after)
	if !reflect.DeepEqual(added, wantAdded) {
		t.Errorf("added lines:\n got %q\nwant %q", added, wantAdded)
	}
	if !reflect.DeepEqual(removed, wantRemoved) {
		t.Errorf("removed lines:\n got %q\nwant %q", removed, wantRemoved)
	}
}

func assertSurvivors(t *testing.T, step, text string) {
	t.Helper()
	for _, want := range mustSurvive {
		if !strings.Contains(text, want) {
			t.Errorf("after %s: %q is gone", step, want)
		}
	}
}

func TestLosslessStateSetVersion(t *testing.T) {
	path := seedLossless(t, losslessFixture)
	if err := runCmd(t, State(), "set", "version", "1.7.11.0"); err != nil {
		t.Fatal(err)
	}
	after := mustRead(t, path)
	onlyLinesChanged(t, losslessFixture, after,
		[]string{`  version = "1.7.11.0"  # why: bumped by the phase workflow`},
		[]string{`  version = "1.7.5.0"  # why: bumped by the phase workflow`})
	assertSurvivors(t, "state set version", after)
}

// TestLosslessUnmodeledKeysSurviveEveryWriter runs the whole writer matrix
// on one file, in sequence, and greps for every survivor after each step.
// A writer that round-trips the struct drops the notes and the [custom]
// table on its first write, so this is the assertion that reddens.
func TestLosslessUnmodeledKeysSurviveEveryWriter(t *testing.T) {
	path := seedLossless(t, losslessFixture)
	steps := []struct {
		name  string
		run   func() error
		drops string // a survivor this step legitimately removes, from here on
	}{
		{"state set version", func() error { return runCmd(t, State(), "set", "version", "1.7.11.0") }, ""},
		{"project set scalar", func() error { return runCmd(t, Project(), "set", "project.name", "renamed") }, ""},
		{"project set stack.languages", func() error { return runCmd(t, Project(), "set", "stack.languages", "go,toml") }, ""},
		{"project set remote.reviewers", func() error { return runCmd(t, Project(), "set", "remote.reviewers", "a,b") }, ""},
		{"project set board.state_map.planned", func() error { return runCmd(t, Project(), "set", "board.state_map.planned", "Open") }, ""},
		{"project set board.fields.state", func() error { return runCmd(t, Project(), "set", "board.fields.state", "Status") }, ""},
		{"project set --unset project.description", func() error {
			return runCmd(t, Project(), "set", "--unset", "project.description")
		}, ""},
		{"project set --unset last state_map key", func() error {
			return runCmd(t, Project(), "set", "--unset", "board.state_map.planned")
		}, ""},
		{"issue enable", func() error { return runCmd(t, Issue(), "enable") }, ""},
		{"issue disable", func() error { return runCmd(t, Issue(), "disable") }, ""},
		{"test lane add", func() error {
			return runCmd(t, Test(), "lane", "add", "verify", "--match", "internal/verify/**", "--command", "go test ./internal/verify/...")
		}, ""},
		{"test lane edit --command", func() error {
			return runCmd(t, Test(), "lane", "edit", "project", "--command", "go test -race ./internal/project/...")
		}, ""},
		// The lane's comment travels with the lane it belongs to: once the
		// lane is removed it is legitimately gone, and must not linger.
		{"test lane remove (middle)", func() error { return runCmd(t, Test(), "lane", "remove", "project") },
			`    # the project package is the fast lane`},
		{"stack apply", func() error { return runCmd(t, Stack(), "apply") }, ""},
	}
	var gone []string
	for _, s := range steps {
		if err := s.run(); err != nil {
			t.Fatalf("%s: %v", s.name, err)
		}
		if s.drops != "" {
			gone = append(gone, s.drops)
		}
		after := mustRead(t, path)
		for _, want := range mustSurvive {
			has := strings.Contains(after, want)
			switch {
			case slices.Contains(gone, want) && has:
				t.Errorf("after %s: %q lingered though its owner was removed", s.name, want)
			case !slices.Contains(gone, want) && !has:
				t.Errorf("after %s: %q is gone", s.name, want)
			}
		}
		if _, err := project.Load(path); err != nil {
			t.Fatalf("after %s the file no longer loads: %v", s.name, err)
		}
	}
}

func TestLosslessArrayWriteIsOneLine(t *testing.T) {
	path := seedLossless(t, losslessFixture)
	if err := runCmd(t, Project(), "set", "stack.languages", "go,toml"); err != nil {
		t.Fatal(err)
	}
	after := mustRead(t, path)
	onlyLinesChanged(t, losslessFixture, after,
		[]string{`  languages = ["go", "toml"]`},
		[]string{`  languages = ["go"]`})
	if !strings.Contains(after, "  non_goals = [\n    \"Not a general-purpose tool\", # marketing is out of scope\n") {
		t.Error("the multi-line non_goals array was rewritten")
	}
}

func TestLosslessAbsentKeyInsertsInsideTable(t *testing.T) {
	path := seedLossless(t, losslessFixture)
	if err := runCmd(t, Project(), "set", "remote.reviewers", "a,b"); err != nil {
		t.Fatal(err)
	}
	after := mustRead(t, path)
	onlyLinesChanged(t, losslessFixture, after, []string{`  reviewers = ["a", "b"]`}, nil)
	if !strings.Contains(after, "  mirror = \"https://example.invalid/mirror/fixture\"\n  reviewers = [\"a\", \"b\"]\n\n[board]\n") {
		t.Errorf("reviewers did not land inside [remote]:\n%s", after)
	}
}

func TestLosslessSubtableCreatedOnFirstWrite(t *testing.T) {
	for _, tc := range []struct {
		name, key, value, header, line string
	}{
		{"state_map", "board.state_map.planned", "Open", `  [board.state_map]`, `    planned = "Open"`},
		{"fields", "board.fields.state", "Status", `  [board.fields]`, `    state = "Status"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := seedLossless(t, losslessFixture)
			if err := runCmd(t, Project(), "set", tc.key, tc.value); err != nil {
				t.Fatal(err)
			}
			after := mustRead(t, path)
			onlyLinesChanged(t, losslessFixture, after, []string{``, tc.header, tc.line}, nil)
			want := "  project = \"FIX\"\n\n" + tc.header + "\n" + tc.line + "\n\n[paths]\n"
			if !strings.Contains(after, want) {
				t.Errorf("sub-table placement:\n%s", after)
			}
		})
	}
}

func TestLosslessUnsetIsOneLine(t *testing.T) {
	path := seedLossless(t, losslessFixture)
	if err := runCmd(t, Project(), "set", "--unset", "project.description"); err != nil {
		t.Fatal(err)
	}
	after := mustRead(t, path)
	onlyLinesChanged(t, losslessFixture, after, nil, []string{`  description = "a fixture project"`})
	if !strings.Contains(after, "  # what the project is for\n  created = ") {
		t.Error("the comment beside description was removed with it")
	}

	// Set the only state_map key, then unset it: the header goes with it
	// and the file is byte-identical to where it started.
	path = seedLossless(t, losslessFixture)
	if err := runCmd(t, Project(), "set", "board.state_map.planned", "Open"); err != nil {
		t.Fatal(err)
	}
	if err := runCmd(t, Project(), "set", "--unset", "board.state_map.planned"); err != nil {
		t.Fatal(err)
	}
	if after := mustRead(t, path); after != losslessFixture {
		added, removed := changedLineSets(losslessFixture, after)
		t.Errorf("set+unset of the only state_map key left residue: added %q removed %q", added, removed)
	}
}

func TestLosslessIssueToggle(t *testing.T) {
	path := seedLossless(t, losslessFixture)
	if err := runCmd(t, Issue(), "enable"); err != nil {
		t.Fatal(err)
	}
	enabled := mustRead(t, path)
	onlyLinesChanged(t, losslessFixture, enabled, []string{`  enabled = true`}, nil)
	if err := runCmd(t, Issue(), "disable"); err != nil {
		t.Fatal(err)
	}
	disabled := mustRead(t, path)
	onlyLinesChanged(t, enabled, disabled, nil, []string{`  enabled = true`})
	if disabled != losslessFixture {
		t.Error("enable+disable did not restore the original bytes")
	}
}

func TestLosslessLaneRemoveLeavesSiblings(t *testing.T) {
	path := seedLossless(t, losslessFixture)
	if err := runCmd(t, Test(), "lane", "remove", "project"); err != nil {
		t.Fatal(err)
	}
	after := mustRead(t, path)
	onlyLinesChanged(t, losslessFixture, after, nil, []string{
		``,
		`  [[runtime.test_lane]]`,
		`    # the project package is the fast lane`,
		`    name = "project"`,
		`    match = ["internal/project/**"]`,
		`    command = "go test ./internal/project/..."`,
	})
	if !strings.Contains(after, "    command = \"go test ./internal/cmd/...\"\n\n  [[runtime.test_lane]]\n    name = \"ship\"\n") {
		t.Errorf("lanes 1 and 3 are not intact neighbours:\n%s", after)
	}
}

func TestLosslessLaneAddEditOneBlock(t *testing.T) {
	path := seedLossless(t, losslessFixture)
	if err := runCmd(t, Test(), "lane", "edit", "project", "--command", "go test -race ./internal/project/..."); err != nil {
		t.Fatal(err)
	}
	edited := mustRead(t, path)
	onlyLinesChanged(t, losslessFixture, edited,
		[]string{`    command = "go test -race ./internal/project/..."`},
		[]string{`    command = "go test ./internal/project/..."`})
	if !strings.Contains(edited, "  [[runtime.test_lane]]\n    # the project package is the fast lane\n    name = \"project\"\n") {
		t.Error("lane edit dropped the block's comment")
	}

	if err := runCmd(t, Test(), "lane", "add", "verify", "--match", "internal/verify/**", "--command", "go test ./internal/verify/..."); err != nil {
		t.Fatal(err)
	}
	added := mustRead(t, path)
	onlyLinesChanged(t, edited, added, []string{
		``,
		`  [[runtime.test_lane]]`,
		`    name = "verify"`,
		`    match = ["internal/verify/**"]`,
		`    command = "go test ./internal/verify/..."`,
	}, nil)
	if !strings.Contains(added, "    command = \"go test ./internal/ship/...\"\n\n  [[runtime.test_lane]]\n    name = \"verify\"\n") {
		t.Errorf("new lane did not land after the last:\n%s", added)
	}
}

func TestLosslessStackApply(t *testing.T) {
	path := seedLossless(t, losslessFixture)
	if err := runCmd(t, Stack(), "apply"); err != nil {
		t.Fatal(err)
	}
	after := mustRead(t, path)
	onlyLinesChanged(t, losslessFixture, after,
		[]string{
			`  profile = "go"`,
			`  test_command = "go test -count=1 ./..."`,
			`  typecheck_command = "go vet ./..."`,
			`  format_command = "gofmt -l ."`,
			`  build_command = "make build"`,
		},
		[]string{`  test_command = "go test ./..."`})
	assertSurvivors(t, "stack apply", after)
}

// TestLosslessRealProjectTomlOneLineDiff bumps the version on a copy of the
// repo's own tracked project.toml — the file whose indented `[[stack.locked]]`
// blocks and empty [paths]/[env] headers a round-trip would re-render.
func TestLosslessRealProjectTomlOneLineDiff(t *testing.T) {
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 6; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			break
		}
		dir = filepath.Dir(dir)
	}
	real := filepath.Join(dir, ".dross", project.File)
	if _, err := os.Stat(real); err != nil {
		t.Skipf("no repo project.toml at %s", real)
	}
	src := mustRead(t, real)
	path := seedLossless(t, src)
	if err := runCmd(t, State(), "set", "version", "99.0.0.0"); err != nil {
		t.Fatal(err)
	}
	after := mustRead(t, path)
	added, removed := changedLineSets(src, after)
	if len(added) != 1 || len(removed) != 1 {
		t.Fatalf("version bump changed more than one line:\n added %q\n removed %q", added, removed)
	}
	if added[0] != `  version = "99.0.0.0"` || !strings.HasPrefix(removed[0], `  version = "`) {
		t.Errorf("changed the wrong line: added %q removed %q", added[0], removed[0])
	}
}
