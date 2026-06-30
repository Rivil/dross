package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestSameRemoteURL(t *testing.T) {
	tests := []struct {
		a, b string
		want bool
	}{
		{"https://github.com/Rivil/dross", "https://github.com/Rivil/dross.git", true},
		{"git@github.com:Rivil/dross.git", "https://github.com/Rivil/dross", true},
		{"ssh://git@github.com/Rivil/dross.git", "https://github.com/Rivil/dross", true},
		{"https://github.com/Rivil/dross", "https://github.com/other/dross", false},
		{"https://github.com/Rivil/dross", "https://gitlab.com/Rivil/dross", false},
		{"", "", true}, // both empty are equal — caller handles "missing" before calling
	}
	for _, tc := range tests {
		got := sameRemoteURL(tc.a, tc.b)
		if got != tc.want {
			t.Errorf("sameRemoteURL(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestDoctorWarnsOnMissingRemote(t *testing.T) {
	dir := t.TempDir()
	gitInit(t, dir, "https://github.com/Rivil/dross.git")
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatalf("init: %v", err)
	}
	// init now writes [remote] from git origin → doctor should pass.
	out := captureStdout(t, func() {
		_ = runCmd(t, Doctor())
	})
	if !strings.Contains(out, "git origin matches [remote].url") {
		t.Errorf("expected match line in healthy doctor output, got:\n%s", out)
	}
}

func TestDoctorFlagsMismatchedRemote(t *testing.T) {
	dir := t.TempDir()
	gitInit(t, dir, "https://github.com/Rivil/dross.git")
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatalf("init: %v", err)
	}
	// Tamper with project.toml [remote].url
	if err := runCmd(t, Project(), "set", "remote.url", "https://github.com/other/repo"); err != nil {
		t.Fatalf("project set: %v", err)
	}
	out := captureStdout(t, func() {
		_ = runCmd(t, Doctor())
	})
	if !strings.Contains(out, "does not match") {
		t.Errorf("expected mismatch warning, got:\n%s", out)
	}
}

func TestDoctorFlagsMissingAuthEnv(t *testing.T) {
	dir := t.TempDir()
	gitInit(t, dir, "https://github.com/Rivil/dross.git")
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatalf("init: %v", err)
	}
	if err := runCmd(t, Project(), "set", "remote.auth_env", "DROSS_TEST_TOKEN_DEFINITELY_UNSET"); err != nil {
		t.Fatalf("project set: %v", err)
	}
	t.Setenv("DROSS_TEST_TOKEN_DEFINITELY_UNSET", "") // explicit absence
	out := captureStdout(t, func() {
		_ = runCmd(t, Doctor())
	})
	if !strings.Contains(out, "is not set in this shell") {
		t.Errorf("expected auth_env warning, got:\n%s", out)
	}
}

func TestDoctorAcceptsGitLabRemote(t *testing.T) {
	dir := t.TempDir()
	gitInit(t, dir, "https://gitlab.com/Rivil/dross.git")
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatalf("init: %v", err)
	}
	// c-1: a GitLab remote with auth_env set is validated, not rejected.
	if err := runCmd(t, Project(), "set", "remote.auth_env", "DROSS_TEST_GITLAB_TOKEN"); err != nil {
		t.Fatalf("project set: %v", err)
	}
	t.Setenv("DROSS_TEST_GITLAB_TOKEN", "secret")

	var out string
	err := runCmdCapturing(t, &out, Doctor())
	if err != nil {
		t.Fatalf("doctor should accept a well-formed GitLab remote, got error; out:\n%s", out)
	}
	if !strings.Contains(out, "git origin matches [remote].url") {
		t.Errorf("expected origin-match line for the gitlab remote:\n%s", out)
	}
	if !strings.Contains(out, "$DROSS_TEST_GITLAB_TOKEN is set") {
		t.Errorf("expected auth_env-set line for the gitlab remote:\n%s", out)
	}
}

// TestDoctorValidatesBoardBlock proves c-1: doctor validates a configured
// [board] independently of [remote] — flagging an unset $auth_env, an
// unrecognised provider, a malformed base_url, and an invalid milestone_mode,
// while passing a well-formed youtrack board with a ✓ line.
func TestDoctorValidatesBoardBlock(t *testing.T) {
	const tokenEnv = "DROSS_TEST_BOARD_TOKEN"

	// runWithBoard inits a repo with a well-formed youtrack [board] as the
	// baseline, applies the caller's overrides, optionally exports the token,
	// then runs doctor and returns its captured output + error.
	runWithBoard := func(t *testing.T, overrides map[string]string, exportToken bool) (string, error) {
		t.Helper()
		dir := t.TempDir()
		gitInit(t, dir, "https://gitlab.com/Rivil/dross.git")
		chdir(t, dir)
		if err := runCmd(t, Init()); err != nil {
			t.Fatalf("init: %v", err)
		}
		fields := map[string]string{
			// Point [remote].auth_env at the same token so the [remote] block
			// stays clean and only the [board] block decides the verdict.
			"remote.auth_env":      tokenEnv,
			"board.provider":       "youtrack",
			"board.base_url":       "https://yt.example.com",
			"board.auth_env":       tokenEnv,
			"board.project":        "PROJ",
			"board.enabled":        "true",
			"board.milestone_mode": "version",
		}
		for k, v := range overrides {
			fields[k] = v
		}
		for k, v := range fields {
			if err := runCmd(t, Project(), "set", k, v); err != nil {
				t.Fatalf("project set %s: %v", k, err)
			}
		}
		if exportToken {
			t.Setenv(tokenEnv, "secret")
		} else {
			t.Setenv(tokenEnv, "") // explicit absence
		}
		var out string
		err := runCmdCapturing(t, &out, Doctor())
		return out, err
	}

	t.Run("well-formed youtrack board", func(t *testing.T) {
		out, err := runWithBoard(t, nil, true)
		if err != nil {
			t.Fatalf("doctor should accept a well-formed board, got error; out:\n%s", out)
		}
		if !strings.Contains(out, "[board] is well-formed") {
			t.Errorf("expected ✓ board line:\n%s", out)
		}
	})

	bad := []struct {
		name      string
		overrides map[string]string
		export    bool
		want      string
	}{
		{"unset auth_env", nil, false, "auth_env"},
		{"bogus provider", map[string]string{"board.provider": "bogus"}, true, "provider"},
		{"bad base_url", map[string]string{"board.base_url": "not a url"}, true, "base_url"},
		{"invalid milestone_mode", map[string]string{"board.milestone_mode": "bogus"}, true, "milestone_mode"},
	}
	for _, tc := range bad {
		t.Run(tc.name, func(t *testing.T) {
			out, err := runWithBoard(t, tc.overrides, tc.export)
			if err == nil {
				t.Errorf("expected non-nil error for %s; out:\n%s", tc.name, out)
			}
			if !strings.Contains(out, "✗") || !strings.Contains(out, tc.want) {
				t.Errorf("expected ✗ %s line, got:\n%s", tc.want, out)
			}
		})
	}
}

func TestDoctorFlagsInvalidAuthScheme(t *testing.T) {
	dir := t.TempDir()
	gitInit(t, dir, "https://gitlab.com/Rivil/dross.git")
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatalf("init: %v", err)
	}
	if err := runCmd(t, Project(), "set", "remote.auth_scheme", "token"); err != nil {
		t.Fatalf("project set: %v", err)
	}
	var out string
	err := runCmdCapturing(t, &out, Doctor())
	if err == nil {
		t.Error("expected non-nil error for an invalid remote.auth_scheme")
	}
	if !strings.Contains(out, "auth_scheme") || !strings.Contains(out, "invalid") {
		t.Errorf("expected invalid auth_scheme warning, got:\n%s", out)
	}
}

func TestDoctorReturnsErrorOnIssues(t *testing.T) {
	dir := t.TempDir()
	gitInit(t, dir, "https://github.com/Rivil/dross.git")
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatalf("init: %v", err)
	}
	if err := runCmd(t, Project(), "set", "remote.url", "https://example.com/wrong"); err != nil {
		t.Fatalf("project set: %v", err)
	}
	if err := runCmd(t, Doctor()); err == nil {
		t.Error("expected non-nil error from Doctor when issues present")
	}
}

func TestDoctorFlagsMissingFoundationalFile(t *testing.T) {
	dir := t.TempDir()
	gitInit(t, dir, "https://github.com/Rivil/dross.git")
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatalf("init: %v", err)
	}

	// Simulate a botched recovery: project.toml went missing.
	if err := os.Remove(filepath.Join(dir, ".dross", "project.toml")); err != nil {
		t.Fatal(err)
	}

	var doctorOut string
	doctorErr := runCmdCapturing(t, &doctorOut, Doctor())
	if doctorErr == nil {
		t.Fatal("expected error when project.toml is missing")
	}
	if !strings.Contains(doctorOut, ".dross/project.toml") || !strings.Contains(doctorOut, "missing") {
		t.Errorf("output should call out the missing file:\n%s", doctorOut)
	}
	if !strings.Contains(doctorOut, "dross ship recover") {
		t.Errorf("output should hint at recovery remediation:\n%s", doctorOut)
	}
}

func TestDoctorFlagsPhaseCommitsOnMain(t *testing.T) {
	// Build a repo where main has a commit recorded as a phase task —
	// the legacy state we want users to migrate away from.
	dir := t.TempDir()
	remoteDir := t.TempDir()
	mustGit(t, remoteDir, "init", "-q", "--bare", "-b", "main")
	gitInit(t, dir, remoteDir)
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatalf("init: %v", err)
	}
	mustWrite(t, filepath.Join(dir, "README.md"), "base\n")
	mustGit(t, dir, "add", ".")
	mustGit(t, dir, "commit", "-q", "-m", "chore: baseline")
	mustGit(t, dir, "push", "-q", "-u", "origin", "main")

	// Make a phase commit *on main* — the legacy pattern.
	mustWrite(t, filepath.Join(dir, "src/leak.ts"), "x\n")
	mustGit(t, dir, "add", ".")
	mustGit(t, dir, "commit", "-q", "-m", "feat: leak")
	leakSHA := mustGit(t, dir, "rev-parse", "HEAD")

	// Record that commit in a phase's changes.json so doctor can match.
	phaseDir := filepath.Join(dir, ".dross", "phases", "01-leak")
	if err := os.MkdirAll(phaseDir, 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(phaseDir, "changes.json"),
		`{"phase":"01-leak","tasks":{"t1":{"commit":"`+leakSHA+`"}}}`)

	out := captureStdout(t, func() {
		_ = runCmd(t, Doctor())
	})
	if !strings.Contains(out, "phase commit") || !strings.Contains(out, "ahead of origin/main") {
		t.Errorf("output should flag phase commits on main:\n%s", out)
	}
	if !strings.Contains(out, leakSHA[:7]) {
		t.Errorf("output should name the leaked SHA prefix:\n%s", out)
	}
	if !strings.Contains(out, "git branch phase/<id>") {
		t.Errorf("output should suggest the branch+reset fix:\n%s", out)
	}
}

func TestDoctorFlagsMissingGitattributes(t *testing.T) {
	dir := t.TempDir()
	gitInit(t, dir, "https://github.com/Rivil/dross.git")
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatalf("init: %v", err)
	}

	// Simulate a legacy dross project: .gitattributes never had the
	// linguist-generated line added.
	if err := os.Remove(filepath.Join(dir, ".gitattributes")); err != nil {
		t.Fatal(err)
	}

	out := captureStdout(t, func() {
		_ = runCmd(t, Doctor())
	})
	if !strings.Contains(out, "not marked linguist-generated") {
		t.Errorf("expected linguist-generated warning, got:\n%s", out)
	}
	if !strings.Contains(out, drossGitattributesLine) {
		t.Errorf("output should include the line to add:\n%s", out)
	}
}

// TestArchitectureLinkWarnings (c-3) proves the resolver-backed detection: only
// Moved and Unresolved bullets warn — an OK link, a Skipped (unindexable) link,
// and a no-line link stay silent — and a repo with no ARCHITECTURE.md yields no
// section (present=false).
func TestArchitectureLinkWarnings(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "foo.go"), "package foo\n\nfunc Bar() {}\n") // Bar at line 3
	doc := "### Feature\n\none line.\n\n" +
		"- `Bar` — `foo.go:99`\n" + // Moved (lives at 3) → warn
		"- `Gone` — `foo.go:3`\n" + // Unresolved (no such symbol) → warn
		"- `Bar` — `foo.go:3`\n" + // OK → silent
		"- `Doc` — `notes.md:1`\n" + // Skipped (codex can't index .md) → silent
		"\n_x_\n"
	mustWrite(t, filepath.Join(dir, "ARCHITECTURE.md"), doc)

	warnings, present := architectureLinkWarnings(dir)
	if !present {
		t.Fatal("expected present=true when ARCHITECTURE.md exists")
	}
	if len(warnings) != 2 {
		t.Fatalf("expected exactly 2 warnings (Moved+Unresolved), got %d: %v", len(warnings), warnings)
	}

	// No ARCHITECTURE.md → no section at all.
	if err := os.Remove(filepath.Join(dir, "ARCHITECTURE.md")); err != nil {
		t.Fatal(err)
	}
	if _, present := architectureLinkWarnings(dir); present {
		t.Error("expected present=false when ARCHITECTURE.md is absent")
	}
}

// TestDoctorStaleLinksNeverBlock (c-3) proves the advisory-only contract by the
// only falsifiable measure: stale ARCHITECTURE.md links must not change doctor's
// issue verdict. Running doctor on the same repo with a clean vs a stale doc must
// yield the identical return error (issue count unchanged) — while the ⚠ advisory
// still appears in the output.
func TestDoctorStaleLinksNeverBlock(t *testing.T) {
	dir := t.TempDir()
	gitInit(t, dir, "https://github.com/Rivil/dross.git")
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatalf("init: %v", err)
	}
	mustWrite(t, filepath.Join(dir, "foo.go"), "package foo\n\nfunc Bar() {}\n") // Bar at line 3

	errString := func(e error) string {
		if e == nil {
			return ""
		}
		return e.Error()
	}

	// Baseline: the seeded skeleton (no real entries → no stale links).
	var baseOut string
	baseErr := runCmdCapturing(t, &baseOut, Doctor())

	// Now plant stale links and re-run on the otherwise-identical repo.
	stale := "### Feature\n\none line.\n\n- `Bar` — `foo.go:99`\n- `Gone` — `foo.go:3`\n\n_x_\n"
	mustWrite(t, filepath.Join(dir, "ARCHITECTURE.md"), stale)
	var staleOut string
	staleErr := runCmdCapturing(t, &staleOut, Doctor())

	if errString(baseErr) != errString(staleErr) {
		t.Errorf("stale links changed the doctor verdict (must be advisory):\n base=%q\n stale=%q", errString(baseErr), errString(staleErr))
	}
	if !strings.Contains(staleOut, "Architecture links:") || !strings.Contains(staleOut, "⚠") {
		t.Errorf("expected the advisory stale-link section with ⚠, got:\n%s", staleOut)
	}
}

// plantInteractionFixture writes a minimal command/prompt/audit tree into dir so
// doctor's interaction-coverage lint has a dross-source-tree to classify:
//   - foo: interactive (AskUserQuestion shim) + audit section → covered
//   - baz: non-interactive + Exempt entry → covered
//   - bar (only if includeBar): non-interactive, NOT exempt → the unclassified probe
func plantInteractionFixture(t *testing.T, dir string, includeBar bool) {
	t.Helper()
	mustWrite(t, filepath.Join(dir, "assets/commands/dross-foo.md"), "allowed-tools: AskUserQuestion\n")
	mustWrite(t, filepath.Join(dir, "assets/prompts/foo.md"), "# foo\n")
	mustWrite(t, filepath.Join(dir, "assets/commands/dross-baz.md"), "allowed-tools: Read\n")
	mustWrite(t, filepath.Join(dir, "assets/prompts/baz.md"), "# baz\n")
	if includeBar {
		mustWrite(t, filepath.Join(dir, "assets/commands/dross-bar.md"), "allowed-tools: Read\n")
		mustWrite(t, filepath.Join(dir, "assets/prompts/bar.md"), "# bar\n")
	}
	mustWrite(t, filepath.Join(dir, "docs/interaction-audit.md"),
		"# Interaction audit\n\n### dross-foo\n\n| Decision point | Conforms |\n|---|---|\n| pick | yes |\n\n"+
			"## Exempt\n\n| Command | Reason |\n|---|---|\n| baz | read-only fixture |\n")
}

// TestInteractionCoverageWarnings (c-5) proves the present-gating: a dir with no
// docs/interaction-audit.md yields no section (present=false), while a planted
// dross source tree returns present=true and one warning per unclassified prompt.
func TestInteractionCoverageWarnings(t *testing.T) {
	// Absent source tree → no section.
	if _, present := interactionCoverageWarnings(t.TempDir()); present {
		t.Error("expected present=false when docs/interaction-audit.md is absent")
	}

	// Planted tree with an unclassified prompt → present, warning names it.
	dir := t.TempDir()
	plantInteractionFixture(t, dir, true)
	warnings, present := interactionCoverageWarnings(dir)
	if !present {
		t.Fatal("expected present=true for a dross source tree")
	}
	joined := strings.Join(warnings, "\n")
	if !strings.Contains(joined, "bar") {
		t.Errorf("expected a warning naming the unclassified 'bar'; got: %v", warnings)
	}
	if strings.Contains(joined, "foo ") || strings.Contains(joined, "baz ") {
		t.Errorf("covered prompts foo/baz should not warn; got: %v", warnings)
	}
}

// TestDoctorInteractionCoverage (c-5) proves the doctor lint end to end. The
// assertions are differential (baseline vs. with-fixture, in one repo) so they
// isolate the coverage check from unrelated baseline issues in the test shell
// (e.g. an unset $auth_env): an unclassified prompt prints a ✗ line naming it and
// *changes the verdict* (adds an issue); a fully classified tree prints the ✓ line
// and leaves the verdict unchanged (adds no issue); and a repo with no dross
// source tree emits no section at all.
func TestDoctorInteractionCoverage(t *testing.T) {
	newRepo := func(t *testing.T) string {
		t.Helper()
		dir := t.TempDir()
		gitInit(t, dir, "https://github.com/Rivil/dross.git")
		chdir(t, dir)
		if err := runCmd(t, Init()); err != nil {
			t.Fatalf("init: %v", err)
		}
		return dir
	}
	errString := func(e error) string {
		if e == nil {
			return ""
		}
		return e.Error()
	}

	t.Run("classified tree adds no issue and shows ✓", func(t *testing.T) {
		dir := newRepo(t)
		var baseOut, classOut string
		baseErr := runCmdCapturing(t, &baseOut, Doctor()) // no source tree yet → no section
		if strings.Contains(baseOut, "Interaction coverage:") {
			t.Fatalf("baseline should have no coverage section:\n%s", baseOut)
		}
		plantInteractionFixture(t, dir, false) // only covered prompts
		classErr := runCmdCapturing(t, &classOut, Doctor())
		if errString(baseErr) != errString(classErr) {
			t.Errorf("a fully-classified tree changed the doctor verdict (must add no issue):\n base=%q\n class=%q",
				errString(baseErr), errString(classErr))
		}
		if !strings.Contains(classOut, "every command-backed prompt is sectioned or exempt") {
			t.Errorf("expected the ✓ coverage line, got:\n%s", classOut)
		}
	})

	t.Run("unclassified prompt adds an issue and a ✗ line", func(t *testing.T) {
		dir := newRepo(t)
		var baseOut, uncOut string
		baseErr := runCmdCapturing(t, &baseOut, Doctor())
		plantInteractionFixture(t, dir, true) // bar is unclassified
		uncErr := runCmdCapturing(t, &uncOut, Doctor())
		if errString(baseErr) == errString(uncErr) {
			t.Errorf("an unclassified prompt must change the verdict (add an issue); both=%q", errString(uncErr))
		}
		if !strings.Contains(uncOut, "Interaction coverage:") || !strings.Contains(uncOut, "✗") || !strings.Contains(uncOut, "bar") {
			t.Errorf("expected a ✗ coverage line naming 'bar', got:\n%s", uncOut)
		}
	})

	t.Run("no source tree, no section", func(t *testing.T) {
		newRepo(t) // plain repo: no docs/interaction-audit.md, no assets/
		var out string
		_ = runCmdCapturing(t, &out, Doctor())
		if strings.Contains(out, "Interaction coverage:") {
			t.Errorf("expected no coverage section outside the dross source tree, got:\n%s", out)
		}
	})
}

// runCmdCapturing runs cmd with args while capturing stdout into *out.
// Use when both the error and the output text matter to the assertion.
func runCmdCapturing(t *testing.T, out *string, cmd *cobra.Command, args ...string) error {
	t.Helper()
	var err error
	*out = captureStdout(t, func() {
		err = runCmd(t, cmd, args...)
	})
	return err
}
