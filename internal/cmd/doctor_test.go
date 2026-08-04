package cmd

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/Rivil/dross/internal/configenum"
	"github.com/Rivil/dross/internal/project"
	"github.com/Rivil/dross/internal/state"
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
			"board.base_url":       "https://acme.youtrack.cloud",
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

// TestDoctorFlagsUnknownStateMapKey proves c-4's doctor half: a dead
// [board].state_map key already on disk is reported as an issue counting
// toward the non-zero exit, not as a warning (locked state_map_key_severity).
//
// The fixture is a complete, otherwise-clean [board] block — valid provider,
// auth_env exported, valid base_url, valid milestone_mode — so the valid-keys
// run reaches "✓ [board] is well-formed" and returns nil. Without that, the
// block errors on provider and auth_env whether the keys are good or bad, the
// negative control proves nothing, and the positive case cannot attribute its
// non-nil error to the state_map check.
func TestDoctorFlagsUnknownStateMapKey(t *testing.T) {
	const tokenEnv = "DROSS_TEST_STATEMAP_TOKEN"

	run := func(t *testing.T, stateMap string) (string, error) {
		t.Helper()
		dir := t.TempDir()
		gitInit(t, dir, "https://gitlab.com/Rivil/dross.git")
		chdir(t, dir)
		if err := runCmd(t, Init()); err != nil {
			t.Fatalf("init: %v", err)
		}
		for _, kv := range [][2]string{
			{"remote.auth_env", tokenEnv},
			{"board.provider", "youtrack"},
			{"board.base_url", "https://acme.youtrack.cloud"},
			{"board.auth_env", tokenEnv},
			{"board.project", "PROJ"},
			{"board.enabled", "true"},
			{"board.milestone_mode", "version"},
		} {
			if err := runCmd(t, Project(), "set", kv[0], kv[1]); err != nil {
				t.Fatalf("project set %s: %v", kv[0], err)
			}
		}
		t.Setenv(tokenEnv, "secret")
		// Hand-appended: `project set` now refuses to write a bad key, so a
		// hand edit is the only way one reaches disk — which is also how the
		// real ones got there, and why doctor has to see them at all.
		path := filepath.Join(dir, ".dross", "project.toml")
		mustWrite(t, path, mustRead(t, path)+"\n[board.state_map]\n"+stateMap+"\n")
		var out string
		err := runCmdCapturing(t, &out, Doctor())
		return out, err
	}

	clean, cleanErr := run(t, `verifying = "In Review"`)
	if cleanErr != nil {
		t.Fatalf("a [board] block with only valid state_map keys must pass; out:\n%s", clean)
	}
	if !strings.Contains(clean, "[board] is well-formed") {
		t.Fatalf("control fixture is not clean — the comparison below would be meaningless:\n%s", clean)
	}

	bad, badErr := run(t, `planning = "To Do"`)
	if badErr == nil {
		t.Errorf("a dead state_map key must count toward the non-zero exit; out:\n%s", bad)
	}
	if !strings.Contains(bad, "[board].state_map.planning") {
		t.Errorf("no ✗ line names the offending key (err=%v):\n%s", badErr, bad)
	}
	// Naming the fault is not enough — `project set` refuses to write such a
	// key, so doctor has to name the one call that removes it, or it reports a
	// fault with no visible repair.
	if !strings.Contains(bad, "dross project set --unset board.state_map.planning") {
		t.Errorf("no repair line for the dead key:\n%s", bad)
	}
	if got, want := strings.Count(bad, "✗"), strings.Count(clean, "✗")+1; got != want {
		t.Errorf("✗ lines: %d, want %d — the two runs must differ by exactly the state_map line\nclean:\n%s\nbad:\n%s", got, want, clean, bad)
	}
	if got, want := strings.Count(bad, "⚠"), strings.Count(clean, "⚠"); got != want {
		t.Errorf("warning tally changed (%d vs %d) — a dead key is an issue, not a warning", got, want)
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

// TestDoctorFlagsClobberedFile proves doctor's clobber section (c-6) surfaces
// a t-1 finding — an uncommitted content change to a tracked .dross/ file —
// as a ✗ line with a fix hint naming `dross repair`, and counts it as an issue.
func TestDoctorFlagsClobberedFile(t *testing.T) {
	dir := t.TempDir()
	gitInit(t, dir, "https://github.com/Rivil/dross.git")
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatalf("init: %v", err)
	}
	mustGit(t, dir, "add", ".")
	mustGit(t, dir, "commit", "-q", "-m", "chore: baseline")

	projPath := filepath.Join(dir, ".dross", "project.toml")
	// Append rather than replace: valid TOML that still parses, but whose
	// content diverges from HEAD's tracked blob (a clobbered file, not a
	// corrupt one — doctor's other sections still need to load project.toml).
	body := mustRead(t, projPath)
	mustWrite(t, projPath, body+"\n# clobbered\n")

	out := captureStdout(t, func() {
		_ = runCmd(t, Doctor())
	})
	if !strings.Contains(out, ".dross/project.toml") || !strings.Contains(out, "diverged from HEAD") {
		t.Errorf("expected a clobbered-file finding for project.toml, got:\n%s", out)
	}
	if !strings.Contains(out, "dross repair") {
		t.Errorf("expected a fix hint naming dross repair, got:\n%s", out)
	}
}

// TestDoctorClobberSectionCleanIsNotAnIssue proves the negative half of the
// contract: a healthy tree gets a ✓ line and doctor's existing issue count is
// unaffected (Doctor still exits 0 here, matching the other all-clean checks).
func TestDoctorClobberSectionCleanIsNotAnIssue(t *testing.T) {
	dir := t.TempDir()
	gitInit(t, dir, "https://github.com/Rivil/dross.git")
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatalf("init: %v", err)
	}
	mustGit(t, dir, "add", ".")
	mustGit(t, dir, "commit", "-q", "-m", "chore: baseline")

	out := captureStdout(t, func() {
		if err := runCmd(t, Doctor()); err != nil {
			t.Fatalf("doctor should exit clean on a healthy tree: %v", err)
		}
	})
	if !strings.Contains(out, "no clobbered or missing tracked .dross/ files") {
		t.Errorf("expected the clean-clobber ✓ line, got:\n%s", out)
	}
}

// TestDoctorFlagsMissingPhaseDir proves the other half of the clobber
// section (c-6): a phase dir origin/<mainBranch> knows about but the current
// working tree lacks is surfaced as a ✗ line and counted as an issue,
// exercising the missing-dir issues++ increment (doctor.go:370) and the
// missing-dir arm of the clean-vs-findings switch (doctor.go:357).
func TestDoctorFlagsMissingPhaseDir(t *testing.T) {
	remoteDir := t.TempDir()
	mustGit(t, remoteDir, "init", "-q", "--bare", "-b", "main")

	dir := t.TempDir()
	gitInit(t, dir, remoteDir)
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatalf("init: %v", err)
	}
	mustGit(t, dir, "add", ".")
	mustGit(t, dir, "commit", "-q", "-m", "chore: baseline")
	mustGit(t, dir, "push", "-q", "-u", "origin", "main")

	// y is pushed to origin/main from a second clone, so the working tree
	// here never had it — a phase dir known to origin but absent locally.
	otherDir := t.TempDir()
	mustGit(t, otherDir, "clone", "-q", remoteDir, ".")
	mustGit(t, otherDir, "config", "user.email", "test@example.com")
	mustGit(t, otherDir, "config", "user.name", "Test")
	mustWrite(t, filepath.Join(otherDir, ".dross", "phases", "y", "spec.toml"), "[phase]\nid = \"y\"\n")
	mustGit(t, otherDir, "add", ".")
	mustGit(t, otherDir, "commit", "-q", "-m", "chore: scaffold y")
	mustGit(t, otherDir, "push", "-q", "origin", "main")
	mustGit(t, dir, "fetch", "-q", "origin")

	out := captureStdout(t, func() {
		_ = runCmd(t, Doctor())
	})
	if !strings.Contains(out, ".dross/phases/y") || !strings.Contains(out, "phase dir known to origin but absent") {
		t.Errorf("expected a missing-phase-dir finding for y, got:\n%s", out)
	}
	if !strings.Contains(out, "dross repair") {
		t.Errorf("expected a fix hint naming dross repair, got:\n%s", out)
	}
}

// TestDoctorClobberScanErrorIsAdvisory covers doctor.go:353 — when
// detectModifiedOrMissingTracked itself can't run (here, .git is removed
// after init so `git ls-files` fails), the section prints an advisory ⚠
// scan-error line instead of silently reporting a clean or wrong result.
func TestDoctorClobberScanErrorIsAdvisory(t *testing.T) {
	dir := t.TempDir()
	gitInit(t, dir, "https://github.com/Rivil/dross.git")
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatalf("init: %v", err)
	}
	mustGit(t, dir, "add", ".")
	mustGit(t, dir, "commit", "-q", "-m", "chore: baseline")

	if err := os.RemoveAll(filepath.Join(dir, ".git")); err != nil {
		t.Fatal(err)
	}

	out := captureStdout(t, func() {
		_ = runCmd(t, Doctor())
	})
	if !strings.Contains(out, "couldn't scan tracked .dross/ files") {
		t.Errorf("expected the clobber-scan-error ⚠ line, got:\n%s", out)
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

// TestDoctorCover_RemoteSwitch exercises all three explicit arms of the
// gitURL/[remote].url switch (doctor.go:66/68/71) with distinguishing
// assertions, plus the exact issue count so the case-68 issues++ (line 70)
// is caught if flipped to a decrement.
func TestDoctorCover_RemoteSwitch(t *testing.T) {
	t.Run("greenfield: no origin and no [remote] passes", func(t *testing.T) {
		dir := t.TempDir()
		chdir(t, dir)
		t.Setenv("HOME", t.TempDir()) // isolate defaults so [remote] stays empty
		if err := runCmd(t, Init()); err != nil {
			t.Fatalf("init: %v", err)
		}
		var out string
		if err := runCmdCapturing(t, &out, Doctor()); err != nil {
			t.Fatalf("greenfield should pass, got %v\n%s", err, out)
		}
		if !strings.Contains(out, "no git origin and no [remote] configured") {
			t.Errorf("expected the greenfield ✓ line:\n%s", out)
		}
	})

	t.Run("[remote].url set but git has no origin is one issue", func(t *testing.T) {
		dir := t.TempDir()
		chdir(t, dir)
		t.Setenv("HOME", t.TempDir())
		if err := runCmd(t, Init()); err != nil { // no git → no origin
			t.Fatalf("init: %v", err)
		}
		if err := runCmd(t, Project(), "set", "remote.url", "https://github.com/Rivil/dross"); err != nil {
			t.Fatalf("project set: %v", err)
		}
		var out string
		err := runCmdCapturing(t, &out, Doctor())
		if err == nil || err.Error() != "1 project-level issue(s) found" {
			t.Fatalf("expected exactly one issue, got err=%v\n%s", err, out)
		}
		if !strings.Contains(out, "but git has no origin") {
			t.Errorf("expected the ⚠ url-without-origin line:\n%s", out)
		}
	})

	t.Run("git origin but no [remote] in toml is an issue", func(t *testing.T) {
		dir := t.TempDir()
		chdir(t, dir)
		t.Setenv("HOME", t.TempDir())
		if err := runCmd(t, Init()); err != nil { // init first, no git → toml has no [remote]
			t.Fatalf("init: %v", err)
		}
		gitInit(t, dir, "https://github.com/Rivil/dross.git") // add origin AFTER init
		var out string
		err := runCmdCapturing(t, &out, Doctor())
		if err == nil {
			t.Fatalf("expected an issue, got nil\n%s", out)
		}
		if !strings.Contains(out, "but project.toml has no [remote]") {
			t.Errorf("expected the ✗ origin-without-remote line:\n%s", out)
		}
	})
}

// TestDoctorCover_BoardAuthEnvUnset drives the config-level empty auth_env
// branch (doctor.go:121-123): board.auth_env is left unset while every other
// board field is well-formed, so boardIssues is exactly 1. The exact final
// count catches the line-123 boardIssues++ if flipped to a decrement.
func TestDoctorCover_BoardAuthEnvUnset(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	t.Setenv("HOME", t.TempDir())
	gitInit(t, dir, "https://gitlab.com/Rivil/dross.git")
	if err := runCmd(t, Init()); err != nil { // captures the gitlab remote → remote block clean
		t.Fatalf("init: %v", err)
	}
	for k, v := range map[string]string{
		"board.provider":       "youtrack",
		"board.base_url":       "https://acme.youtrack.cloud",
		"board.milestone_mode": "version",
		// board.auth_env deliberately left empty → line 122/123
	} {
		if err := runCmd(t, Project(), "set", k, v); err != nil {
			t.Fatalf("project set %s: %v", k, err)
		}
	}
	var out string
	err := runCmdCapturing(t, &out, Doctor())
	if err == nil || err.Error() != "1 project-level issue(s) found" {
		t.Fatalf("expected exactly one (board auth_env) issue, got err=%v\n%s", err, out)
	}
	if !strings.Contains(out, "[board].auth_env is not set") {
		t.Errorf("expected the config-level empty auth_env ✗ line:\n%s", out)
	}
}

// TestDoctorCover_GitattributesUnreadable makes .gitattributes a directory so
// os.ReadFile fails with a non-NotExist error, driving the "couldn't read"
// branch (doctor.go:158-160). The exact final count catches the line-160
// issues++ if flipped to a decrement.
func TestDoctorCover_GitattributesUnreadable(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	t.Setenv("HOME", t.TempDir())
	gitInit(t, dir, "https://github.com/Rivil/dross.git")
	if err := runCmd(t, Init()); err != nil {
		t.Fatalf("init: %v", err)
	}
	// Replace the .gitattributes file with a directory: os.ReadFile then
	// returns an EISDIR error (not fs.ErrNotExist) → the error branch.
	ga := filepath.Join(dir, ".gitattributes")
	if err := os.Remove(ga); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(ga, 0o755); err != nil {
		t.Fatal(err)
	}
	var out string
	err := runCmdCapturing(t, &out, Doctor())
	if err == nil || err.Error() != "1 project-level issue(s) found" {
		t.Fatalf("expected exactly one (.gitattributes) issue, got err=%v\n%s", err, out)
	}
	if !strings.Contains(out, "couldn't read .gitattributes") {
		t.Errorf("expected the unreadable-gitattributes ⚠ line:\n%s", out)
	}
}

// TestDoctorCover_PhaseHygieneBranches covers the default and error arms of the
// phase-hygiene switch (doctor.go:184/187). The clean-main default arm has
// len(leaked)==0 and must NOT add an issue, which pins both the >0 boundary and
// the negation on line 187 (either mutant would enter the leaked branch and add
// an issue). The error arm forces phaseCommitsOnMain to fail on a missing
// origin/main ref, and asserts it stays advisory (line 184).
func TestDoctorCover_PhaseHygieneBranches(t *testing.T) {
	t.Run("clean main takes the default ✓ arm", func(t *testing.T) {
		dir := t.TempDir()
		chdir(t, dir)
		t.Setenv("HOME", t.TempDir())
		gitInit(t, dir, "https://github.com/Rivil/dross.git")
		if err := runCmd(t, Init()); err != nil {
			t.Fatalf("init: %v", err)
		}
		var out string
		if err := runCmdCapturing(t, &out, Doctor()); err != nil {
			t.Fatalf("clean repo should pass (no phase issue), got %v\n%s", err, out)
		}
		if !strings.Contains(out, "no recorded phase commits on local main") {
			t.Errorf("expected the ✓ default phase-hygiene line:\n%s", out)
		}
	})

	t.Run("unreachable origin/main takes the advisory error arm", func(t *testing.T) {
		dir := t.TempDir()
		chdir(t, dir)
		t.Setenv("HOME", t.TempDir())
		gitInit(t, dir, "https://github.com/Rivil/dross.git") // fake, unfetched → no origin/main ref
		if err := runCmd(t, Init()); err != nil {
			t.Fatalf("init: %v", err)
		}
		// A recorded phase commit forces phaseCommitsOnMain past its
		// empty-recorded early return, so it runs `git rev-list
		// origin/main..main` and errors on the missing origin ref.
		mustWrite(t, filepath.Join(dir, ".dross", "phases", "pp", "changes.json"),
			`{"tasks":{"t1":{"commit":"deadbeefdeadbeefdeadbeefdeadbeefdeadbeef"}}}`)
		var out string
		err := runCmdCapturing(t, &out, Doctor())
		if !strings.Contains(out, "couldn't check phase commits on main") {
			t.Errorf("expected the advisory error ⚠ line:\n%s", out)
		}
		if err != nil {
			t.Errorf("the phase-hygiene error path is advisory and must not add an issue; got err=%v\n%s", err, out)
		}
	})
}

// --- configenum-backed enum checks (phase validator-truth) ---

// runDoctorEnum inits a repo with a well-formed youtrack [board] baseline,
// applies overrides, and runs doctor. An override with an empty value means
// "leave this field unset" rather than "set it to the empty string", so the
// optional-base_url cases can be expressed.
func runDoctorEnum(t *testing.T, overrides map[string]string) (string, error) {
	t.Helper()
	const tokenEnv = "DROSS_TEST_ENUM_TOKEN"
	dir := t.TempDir()
	gitInit(t, dir, "https://gitlab.com/Rivil/dross.git")
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatalf("init: %v", err)
	}
	fields := map[string]string{
		"remote.auth_env":      tokenEnv,
		"board.provider":       "youtrack",
		"board.base_url":       "https://acme.youtrack.cloud",
		"board.auth_env":       tokenEnv,
		"board.project":        "PROJ",
		"board.enabled":        "true",
		"board.milestone_mode": "version",
	}
	for k, v := range overrides {
		if v == "" {
			delete(fields, k)
			continue
		}
		fields[k] = v
	}
	for k, v := range fields {
		if err := runCmd(t, Project(), "set", k, v); err != nil {
			t.Fatalf("project set %s: %v", k, err)
		}
	}
	t.Setenv(tokenEnv, "secret")
	var out string
	err := runCmdCapturing(t, &out, Doctor())
	return out, err
}

// The c-1 regression: doctor rejected jira and github outright, so a board the
// CLI dispatches happily could not pass its own validator. Driving the loop off
// BoardProviders means a seventh backend added without teaching doctor fails
// here rather than in the user's terminal.
func TestDoctorAcceptsEveryDispatchableBoardProvider(t *testing.T) {
	for _, provider := range configenum.BoardProviders.Values() {
		t.Run(provider, func(t *testing.T) {
			out, err := runDoctorEnum(t, map[string]string{"board.provider": provider})
			if err != nil {
				t.Errorf("doctor rejects a dispatchable board provider %q:\n%s", provider, out)
			}
			if strings.Contains(out, "provider = ") && strings.Contains(out, "is invalid") {
				t.Errorf("provider %q reported invalid:\n%s", provider, out)
			}
		})
	}
}

func TestDoctorBoardBaseURLOptionalForGitHub(t *testing.T) {
	out, err := runDoctorEnum(t, map[string]string{
		"board.provider": "github",
		"board.base_url": "", // unset
	})
	if err != nil {
		t.Errorf("a github board needs no base_url (the backend defaults to api.github.com):\n%s", out)
	}

	// The relaxation must not leak: every other backend is self-hosted or
	// per-tenant and has no address to guess.
	out, err = runDoctorEnum(t, map[string]string{
		"board.provider": "youtrack",
		"board.base_url": "", // unset
	})
	if err == nil {
		t.Errorf("a youtrack board with no base_url must still fail:\n%s", out)
	}
	if !strings.Contains(out, "base_url") {
		t.Errorf("expected a base_url line:\n%s", out)
	}
}

// Optional is not unvalidated. Implementing the github relaxation by skipping
// the whole URL branch would silently accept a malformed value.
func TestDoctorGitHubMalformedBaseURLStillFails(t *testing.T) {
	out, err := runDoctorEnum(t, map[string]string{
		"board.provider": "github",
		"board.base_url": "not-a-url",
	})
	if err == nil {
		t.Errorf("a set-but-malformed github base_url must fail:\n%s", out)
	}
	if !strings.Contains(out, "base_url") {
		t.Errorf("expected a base_url line:\n%s", out)
	}
}

// BoardProviders carries no default, so Set.Has("") is false for it — the guard
// against a uniformly-true empty policy leaking in from AuthSchemes.
func TestDoctorRejectsEmptyBoardProvider(t *testing.T) {
	out, err := runDoctorEnum(t, map[string]string{"board.provider": ""})
	if err == nil {
		t.Errorf("an unset [board].provider dispatches nowhere and must fail:\n%s", out)
	}
	if !strings.Contains(out, "provider") || !strings.Contains(out, "invalid") {
		t.Errorf("expected an invalid-provider line:\n%s", out)
	}
}

// doctor must be exactly as forgiving as the consumers: forge lowercases and
// trims before matching, so a capitalised or padded mode is legal downstream.
func TestDoctorNormalisesMilestoneMode(t *testing.T) {
	for _, mode := range []string{"version", "Version", " version", "EPIC", "\tagile "} {
		out, err := runDoctorEnum(t, map[string]string{
			"board.milestone_mode": mode,
			// epic/agile are youtrack modes; the baseline provider is youtrack.
		})
		if err != nil {
			t.Errorf("milestone_mode %q rejected but accepted by the consumer:\n%s", mode, out)
		}
	}
	out, err := runDoctorEnum(t, map[string]string{"board.milestone_mode": "bogus"})
	if err == nil {
		t.Errorf("a genuinely unknown milestone_mode must still fail:\n%s", out)
	}
	if !strings.Contains(out, configenum.MilestoneModes.List()) {
		t.Errorf("message must be derived from MilestoneModes:\n%s", out)
	}
}

func TestDoctorNormalisesAuthScheme(t *testing.T) {
	for _, scheme := range []string{"private-token", "bearer", "basic", " Basic", "BEARER\t"} {
		out, err := runDoctorEnum(t, map[string]string{"remote.auth_scheme": scheme})
		if err != nil {
			t.Errorf("auth_scheme %q rejected:\n%s", scheme, out)
		}
	}
	out, err := runDoctorEnum(t, map[string]string{"remote.auth_scheme": "token"})
	if err == nil {
		t.Errorf("an unknown auth_scheme must still fail:\n%s", out)
	}
	if !strings.Contains(out, configenum.AuthSchemes.List()) {
		t.Errorf("message must list %q, got:\n%s", configenum.AuthSchemes.List(), out)
	}
}

// A partial migration — one switch left behind while the message is already
// derived — is the failure this pins. It forbids accept-set literals (the
// pipe-joined message strings and multi-provider case lines), not every mention
// of a provider name: single-provider cross-field checks are legitimate.
func TestDoctorCarriesNoProviderLiterals(t *testing.T) {
	path := filepath.Join(repoRootFromTest(t), "internal", "cmd", "doctor.go")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	src := string(raw)

	if !strings.Contains(src, "configenum.") {
		t.Fatal("doctor.go does not reference configenum at all")
	}
	for _, set := range []configenum.Set{
		configenum.BoardProviders,
		configenum.ShipProviders,
		configenum.AuthSchemes,
		configenum.MilestoneModes,
	} {
		if strings.Contains(src, set.List()) {
			t.Errorf("doctor.go hand-types the accept-set %q; derive it from configenum", set.List())
		}
	}
	// The switch shape the migration replaced.
	for _, lit := range []string{`case "forgejo"`, `case "youtrack"`, `case "", "version"`} {
		if strings.Contains(src, lit) {
			t.Errorf("doctor.go still carries a literal enum switch: %s", lit)
		}
	}
}

// --- cross-field combination warnings (phase validator-truth, c-5 + c-6) ---

// jira maps a milestone to a fixVersion and errors on anything else, so
// milestone_mode = epic is a guaranteed runtime failure that every per-field
// check passes: epic is a real mode, jira is a real provider.
func TestDoctorWarnsJiraEpicCombination(t *testing.T) {
	out, err := runDoctorEnum(t, map[string]string{
		"board.provider":       "jira",
		"board.auth_user":      "me@example.com",
		"board.milestone_mode": "epic",
	})
	if err != nil {
		t.Errorf("a combination warning must not change the exit code:\n%s", out)
	}
	if !strings.Contains(out, "milestone_mode") || !strings.Contains(out, "⚠") {
		t.Errorf("expected a milestone_mode combination warning:\n%s", out)
	}

	// youtrack accepts every mode, so the same value there is not a warning.
	// Warning on it would train the user to ignore the section.
	out, err = runDoctorEnum(t, map[string]string{
		"board.provider":       "youtrack",
		"board.milestone_mode": "epic",
	})
	if err != nil {
		t.Fatalf("youtrack + epic is a valid combination:\n%s", out)
	}
	if strings.Contains(out, "Combinations:") {
		t.Errorf("youtrack accepts epic — no warning expected:\n%s", out)
	}
}

// The regression guard for the locked new_check_severity decision. doctor's
// exit code gates CI and pre-push hooks; an `issues++` here would start
// breaking repos that work today.
func TestDoctorCombinationWarningKeepsExitZero(t *testing.T) {
	out, err := runDoctorEnum(t, map[string]string{
		"board.provider":       "jira",
		"board.milestone_mode": "epic",
	})
	if err != nil {
		t.Fatalf("combination warnings must keep exit 0, got err=%v\n%s", err, out)
	}
	if !strings.Contains(out, "Combinations:") {
		t.Fatalf("the warning did not fire, so this proves nothing:\n%s", out)
	}
	if !strings.Contains(out, "All project-level checks passed.") {
		t.Errorf("a warnings-only run must still report as passed:\n%s", out)
	}
}

// Jira authenticates as Basic email:token — auth_env alone authenticates
// nothing, and the 401 that follows reads as a bad token.
func TestDoctorWarnsJiraMissingAuthUser(t *testing.T) {
	out, err := runDoctorEnum(t, map[string]string{"board.provider": "jira"})
	if err != nil {
		t.Errorf("a missing board.auth_user is a warning, not a failure:\n%s", out)
	}
	if !strings.Contains(out, "[board].auth_user") {
		t.Errorf("expected a board.auth_user warning:\n%s", out)
	}

	out, _ = runDoctorEnum(t, map[string]string{
		"board.provider":  "jira",
		"board.auth_user": "me@example.com",
	})
	if strings.Contains(out, "[board].auth_user") {
		t.Errorf("a configured auth_user must silence the warning:\n%s", out)
	}
}

// c-6's writer side: the tooling writes [remote].provider from the git origin,
// so a host ship cannot dispatch is written happily and only surfaces at the
// PR step, at the end of a phase.
func TestDoctorWarnsUnshippableRemoteProvider(t *testing.T) {
	out, err := runDoctorEnum(t, map[string]string{"remote.provider": "sourcehut"})
	if err != nil {
		t.Errorf("an unshippable provider is a warning, not a failure:\n%s", out)
	}
	if !strings.Contains(out, "cannot open a PR") {
		t.Errorf("expected an unshippable-provider warning:\n%s", out)
	}

	// bitbucket is dispatchable now — warning on it would be the exact lie
	// this phase exists to remove.
	out, _ = runDoctorEnum(t, map[string]string{"remote.provider": "bitbucket"})
	if strings.Contains(out, "cannot open a PR") {
		t.Errorf("bitbucket is a dispatchable provider:\n%s", out)
	}

	// "none" is the no-remote sentinel, not a broken backend.
	out, _ = runDoctorEnum(t, map[string]string{"remote.provider": "none"})
	if strings.Contains(out, "Combinations:") {
		t.Errorf("the none sentinel must stay silent:\n%s", out)
	}

	// An unset provider likewise. runDoctorEnum cannot express "set to empty",
	// so the empty case is asserted against the check directly.
	if w := remoteCombinationWarnings("", "", ""); len(w) != 0 {
		t.Errorf("an unset remote provider must stay silent, got: %v", w)
	}
}

// Basic auth is user:token on the wire. Without the user half it sends
// base64(:token) and 401s on every ship — guaranteed, not merely suspicious.
func TestDoctorWarnsBasicAuthMissingAuthUser(t *testing.T) {
	out, err := runDoctorEnum(t, map[string]string{"remote.provider": "bitbucket"})
	if err != nil {
		t.Errorf("a missing remote.auth_user is a warning, not a failure:\n%s", out)
	}
	if !strings.Contains(out, "[remote].auth_user") {
		t.Errorf("bitbucket with no auth_user must warn:\n%s", out)
	}

	// The scheme reaches the same conclusion independently of the provider.
	out, _ = runDoctorEnum(t, map[string]string{"remote.auth_scheme": "basic"})
	if !strings.Contains(out, "[remote].auth_user") {
		t.Errorf("auth_scheme = basic with no auth_user must warn:\n%s", out)
	}

	out, _ = runDoctorEnum(t, map[string]string{
		"remote.provider":  "bitbucket",
		"remote.auth_user": "wsuser",
	})
	if strings.Contains(out, "[remote].auth_user") {
		t.Errorf("a configured auth_user must silence the warning:\n%s", out)
	}
}

// Only bitbucket dispatches Basic — gitlab falls through to PRIVATE-TOKEN and
// github ignores the scheme entirely. Today this pairing passes doctor clean
// and 401s on every ship.
func TestDoctorWarnsBasicOnNonBitbucketRemote(t *testing.T) {
	out, err := runDoctorEnum(t, map[string]string{
		"remote.auth_scheme": "basic",
		"remote.auth_user":   "someone", // isolate the scheme/provider pairing
	})
	if err != nil {
		t.Errorf("a no-op auth_scheme is a warning, not a failure:\n%s", out)
	}
	if !strings.Contains(out, "no Basic credential") {
		t.Errorf("basic on a gitlab remote must warn:\n%s", out)
	}

	out, _ = runDoctorEnum(t, map[string]string{
		"remote.provider":    "bitbucket",
		"remote.auth_scheme": "basic",
		"remote.auth_user":   "wsuser",
	})
	if strings.Contains(out, "no Basic credential") {
		t.Errorf("bitbucket is exactly where basic belongs:\n%s", out)
	}
}

// The soft class must not soften the hard one: a value no consumer can
// dispatch at all still exits non-zero, so CI and pre-push hooks keep working.
func TestDoctorHardFailureStillNonZero(t *testing.T) {
	cases := map[string]map[string]string{
		"invalid milestone_mode": {"board.milestone_mode": "bogus"},
		"invalid auth_scheme":    {"remote.auth_scheme": "token"},
		"invalid board provider": {"board.provider": "trello"},
	}
	for name, overrides := range cases {
		t.Run(name, func(t *testing.T) {
			out, err := runDoctorEnum(t, overrides)
			if err == nil {
				t.Errorf("a genuinely invalid value must exit non-zero:\n%s", out)
			}
			if !strings.Contains(out, "✗") {
				t.Errorf("expected a hard-failure line:\n%s", out)
			}
		})
	}
}

// incompleteRootBlock extracts doctor's incomplete-root block — the lines from
// its heading to the next blank line — and returns the file paths it lists.
// Scoping to the block matters: on a project.toml-only fixture the wider
// foundational-trio block also names rules.toml, which is deliberately not
// part of root completeness.
func incompleteRootBlock(t *testing.T, out string) []string {
	t.Helper()
	idx := strings.Index(out, incompleteRootHeading)
	if idx < 0 {
		t.Fatalf("doctor output has no incomplete-root block:\n%s", out)
	}
	var files []string
	for _, line := range strings.Split(out[idx:], "\n")[1:] {
		if strings.TrimSpace(line) == "" {
			break
		}
		if !strings.HasPrefix(line, "  ✗ ") {
			continue
		}
		files = append(files, strings.TrimSuffix(strings.TrimPrefix(line, "  ✗ "), " — missing"))
	}
	return files
}

// TestDoctorDiagnosesIncompleteRoot (c-5): an incomplete `.dross/` reaches the
// diagnosis instead of dying at root resolution. Leaving doctor on FindRoot
// prints nothing and fails both halves.
func TestDoctorDiagnosesIncompleteRoot(t *testing.T) {
	dir := realTempDir(t)
	mkRoot(t, dir, "state.json")
	chdir(t, dir)

	var out string
	err := runCmdCapturing(t, &out, Doctor())
	if err == nil {
		t.Fatal("doctor should report an issue on an incomplete root")
	}
	if !strings.Contains(out, "✗ .dross/project.toml — missing") {
		t.Errorf("output should name the missing file:\n%s", out)
	}
	if !strings.Contains(err.Error(), "project-level issue") {
		t.Errorf("verdict should name a project-level issue, got %v", err)
	}
}

// TestDoctorNamesEveryRootMiss: a short-circuit after the first miss fails the
// second needle.
func TestDoctorNamesEveryRootMiss(t *testing.T) {
	dir := realTempDir(t)
	mkRoot(t, dir)
	chdir(t, dir)

	var out string
	if err := runCmdCapturing(t, &out, Doctor()); err == nil {
		t.Fatal("doctor should report issues when both foundational files are missing")
	}
	for _, want := range []string{".dross/project.toml", ".dross/rules.toml"} {
		if !strings.Contains(out, want) {
			t.Errorf("output should name %s:\n%s", want, out)
		}
	}
}

// TestDoctorIncompleteRootVerdictIsDistinct: an ordinary repairable issue (a
// missing rules.toml) and an incomplete root must not collapse into the same
// verdict — the first is fixable in place, the second means this isn't a dross
// repo at all.
func TestDoctorIncompleteRootVerdictIsDistinct(t *testing.T) {
	repairable := realTempDir(t)
	chdir(t, repairable)
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(repairable, ".dross", "rules.toml")); err != nil {
		t.Fatal(err)
	}
	var repairableOut string
	repairableErr := runCmdCapturing(t, &repairableOut, Doctor())
	if repairableErr == nil {
		t.Fatal("doctor should report the missing rules.toml")
	}
	if strings.Contains(repairableOut, incompleteRootHeading) {
		t.Errorf("a missing rules.toml is not an incomplete root:\n%s", repairableOut)
	}

	incomplete := realTempDir(t)
	mkRoot(t, incomplete, "state.json")
	chdir(t, incomplete)
	incompleteErr := runCmdCapturing(t, new(string), Doctor())
	if incompleteErr == nil {
		t.Fatal("doctor should report the incomplete root")
	}

	if repairableErr.Error() == incompleteErr.Error() {
		t.Errorf("both verdicts read identically: %q", repairableErr)
	}
}

// TestDoctorMissingFileLineCarriesBothHints: doctor and root.go must not drift
// to two repair strings, and the pre-existing `dross ship recover` sentence has
// to survive alongside RepairHint on the same line — replacing rather than
// appending fails this.
func TestDoctorMissingFileLineCarriesBothHints(t *testing.T) {
	dir := realTempDir(t)
	mkRoot(t, dir, "project.toml")
	chdir(t, dir)

	var out string
	if err := runCmdCapturing(t, &out, Doctor()); err == nil {
		t.Fatal("doctor should report an issue on an incomplete root")
	}
	if !strings.Contains(out, RepairHint) {
		t.Errorf("output should contain RepairHint verbatim:\n%s", out)
	}
	var found bool
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "dross ship recover") && strings.Contains(line, RepairHint) {
			found = true
		}
	}
	if !found {
		t.Errorf("no missing-file line carries both the recover sentence and RepairHint:\n%s", out)
	}
}

// TestDoctorIncompleteRootBlockMatchesLocateRoot: the block lists exactly
// LocateRoot's Missing slice. The fixture is chosen so the two differ — the
// foundational trio also flags rules.toml — which is what makes the equality
// meaningful rather than accidental.
func TestDoctorIncompleteRootBlockMatchesLocateRoot(t *testing.T) {
	dir := realTempDir(t)
	mkRoot(t, dir, "state.json")
	chdir(t, dir)

	_, want, err := LocateRoot()
	if err != nil {
		t.Fatal(err)
	}

	var out string
	if err := runCmdCapturing(t, &out, Doctor()); err == nil {
		t.Fatal("doctor should report an issue on an incomplete root")
	}
	got := incompleteRootBlock(t, out)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("incomplete-root block lists %v, LocateRoot reports %v", got, want)
	}
	if strings.Contains(strings.Join(got, ","), "rules.toml") {
		t.Errorf("rules.toml is doctor's trio, not root completeness: %v", got)
	}
}

// doctorTokenEnv is the token the scaffold both configures and exports. The
// helper writes remote.auth_env explicitly instead of keeping whatever
// ~/.claude/dross/defaults.toml suggested — inheriting it is what made these
// tests red on any host that did not happen to export that particular token.
const doctorTokenEnv = "DROSS_TEST_DOCTOR_TOKEN"

// scaffoldDoctorRepo builds a git-backed dross repo whose remote matches, so
// doctor's other sections stay quiet and the task-status verdict is the only
// thing moving the exit code.
func scaffoldDoctorRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitInit(t, dir, "https://github.com/Rivil/dross.git")
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatalf("init: %v", err)
	}
	// Silence the unrelated baseline issues so the exit code tracks the
	// task-status verdict alone. auth_env is set, not inherited: the seeded
	// value comes from the global defaults, which this suite must not read.
	t.Setenv(doctorTokenEnv, "x")
	mustRunSet(t, "remote.auth_env", doctorTokenEnv)
	mustRunSet(t, "project.name", "x")
	mustRunSet(t, "runtime.mode", "native")
	if err := runCmd(t, Doctor()); err != nil {
		t.Fatalf("baseline repo is not doctor-clean: %v", err)
	}
	return dir
}

// writePlan drops a plan.toml (and the spec.toml beside it) into a phase dir.
func writeStatusPlan(t *testing.T, phaseID, body string) {
	t.Helper()
	dir := filepath.Join(".dross", "phases", phaseID)
	mustWrite(t, filepath.Join(dir, "spec.toml"), `[phase]
id = "`+phaseID+`"
title = "test"
[[criteria]]
id = "c-1"
text = "x"
`)
	mustWrite(t, filepath.Join(dir, "plan.toml"), body)
}

func planWithStatus(phaseID, status string) string {
	body := `[phase]
id = "` + phaseID + `"
[[task]]
id = "t-1"
wave = 1
title = "x"
files = ["a.go"]
covers = ["c-1"]
`
	if status != "" {
		body += "status = \"" + status + "\"\n"
	}
	return body
}

func TestDoctorFlagsUnrecognisedTaskStatus(t *testing.T) {
	// "Done" and "in-progress" are exactly the near-misses NextRunnable
	// skips in silence today.
	for _, bad := range []string{"Done", "in-progress"} {
		t.Run(bad, func(t *testing.T) {
			scaffoldDoctorRepo(t)
			writeStatusPlan(t, "01-test", planWithStatus("01-test", bad))

			var out string
			err := runCmdCapturing(t, &out, Doctor())
			if err == nil {
				t.Fatalf("doctor exited 0 with task status %q", bad)
			}
			var line string
			for _, l := range strings.Split(out, "\n") {
				if strings.Contains(l, bad) {
					line = l
					break
				}
			}
			if line == "" {
				t.Fatalf("no line mentions status %q:\n%s", bad, out)
			}
			if !strings.Contains(line, "01-test") || !strings.Contains(line, "t-1") {
				t.Errorf("line %q must name both the phase and the task id", line)
			}
			// The issue must gate the exit code, not sit in the advisory
			// warnings block.
			if !strings.Contains(err.Error(), "issue(s) found") {
				t.Errorf("error = %v, want it counted as a project-level issue", err)
			}
		})
	}
}

func TestDoctorAcceptsEveryRunnableTaskStatus(t *testing.T) {
	// "" means the status field is omitted — pending everywhere else in the
	// code, so it must not be reported here either.
	for _, ok := range []string{"", "pending", "in_progress", "done", "failed"} {
		t.Run("status="+ok, func(t *testing.T) {
			scaffoldDoctorRepo(t)
			writeStatusPlan(t, "01-test", planWithStatus("01-test", ok))

			var out string
			err := runCmdCapturing(t, &out, Doctor())
			if err != nil {
				t.Errorf("doctor errored on a valid status %q: %v\n%s", ok, err, out)
			}
			if !strings.Contains(out, "✓ every task status is") {
				t.Errorf("status %q did not get a clean verdict:\n%s", ok, out)
			}
		})
	}
}

func TestDoctorSkipsPhaseDirWithNoPlan(t *testing.T) {
	scaffoldDoctorRepo(t)
	// Spec'd but not yet planned — normal, and must not derail the section.
	mustWrite(t, filepath.Join(".dross", "phases", "01-unplanned", "spec.toml"), `[phase]
id = "01-unplanned"
title = "t"
[[criteria]]
id = "c-1"
text = "x"
`)
	var out string
	if err := runCmdCapturing(t, &out, Doctor()); err != nil {
		t.Errorf("doctor errored on a phase with no plan.toml: %v\n%s", err, out)
	}
	if !strings.Contains(out, "✓ every task status is") {
		t.Errorf("doctor did not reach the task-status verdict:\n%s", out)
	}
}

func TestDoctorReportsUnparseablePlan(t *testing.T) {
	scaffoldDoctorRepo(t)
	writeStatusPlan(t, "01-test", "[phase\nthis is not toml\n")

	var out string
	err := runCmdCapturing(t, &out, Doctor())
	if err == nil {
		t.Fatal("doctor exited 0 on an unparseable plan.toml")
	}
	if strings.Contains(out, "✓ every task status is") {
		t.Errorf("unparseable plan was swallowed into a clean verdict:\n%s", out)
	}
	if !strings.Contains(out, "01-test") {
		t.Errorf("the report does not name the phase:\n%s", out)
	}
}

// TestValidateIgnoresTaskStatus pins status_check_home: the enum check is
// doctor's, and validate — which runs in every slash command's wrap step —
// must stay structural-only.
func TestValidateIgnoresTaskStatus(t *testing.T) {
	scaffoldDoctorRepo(t)
	writeStatusPlan(t, "01-test", planWithStatus("01-test", "Done"))

	if err := runCmd(t, Validate()); err != nil {
		t.Errorf("dross validate rejected an unrecognised task status: %v", err)
	}
	if err := runCmd(t, Doctor()); err == nil {
		t.Error("doctor accepted it too — the check landed nowhere")
	}
}

// doctorRepo is an initialised dross repo with a git origin, on main, with a
// clean tree — the baseline the three checks below perturb one at a time.
func doctorRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	remote := t.TempDir()
	mustGit(t, remote, "init", "-q", "--bare", "-b", "main")
	gitInit(t, dir, remote)
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatalf("init: %v", err)
	}
	for _, set := range [][]string{
		{"set", "project.name", "p"},
		{"set", "project.version", "1.0.0.0"},
		{"set", "runtime.mode", "native"},
		{"set", "repo.git_main_branch", "main"},
		{"set", "remote.url", remote},
	} {
		if err := runCmd(t, Project(), set...); err != nil {
			t.Fatalf("project %v: %v", set, err)
		}
	}
	mustWrite(t, filepath.Join(dir, "README.md"), "base\n")
	mustGit(t, dir, "add", ".")
	mustGit(t, dir, "commit", "-q", "-m", "chore: baseline")
	mustGit(t, dir, "push", "-q", "-u", "origin", "main")
	return dir
}

// TestDoctorReportsTrackedStateJSON (c-6): a repo that still has state.json in
// the index is one squash away from the clobber. Doctor names it and prints the
// literal command, not a description of one.
func TestDoctorReportsTrackedStateJSON(t *testing.T) {
	dir := doctorRepo(t)
	mustGit(t, dir, "add", "-f", ".dross/"+state.File)
	mustGit(t, dir, "commit", "-q", "-m", "chore: track state.json")

	var out string
	err := runCmdCapturing(t, &out, Doctor())
	if err == nil {
		t.Fatal("a tracked state.json is an issue, not a clean bill of health")
	}
	if !strings.Contains(out, "git rm --cached .dross/state.json") {
		t.Errorf("doctor should print the literal fix command:\n%s", out)
	}
}

// TestDoctorCleanRepoPassesNewSections: with versions in parity, state
// untracked and no stale branch, none of the three sections may fire. An
// over-firing check makes doctor exit non-zero on a healthy repo forever.
func TestDoctorCleanRepoPassesNewSections(t *testing.T) {
	dir := doctorRepo(t)
	_ = dir

	var out string
	if err := runCmdCapturing(t, &out, Doctor()); err != nil {
		t.Fatalf("a clean repo should pass: %v\n%s", err, out)
	}
	for _, want := range []string{
		"✓ .dross/state.json is not tracked",
		"✓ project.toml and state.json agree on 1.0.0.0",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Stale milestone branches:") {
		t.Errorf("no milestone branches exist — the section must stay silent:\n%s", out)
	}
}

// TestDoctorReportsVersionDrift (c-6): the two homes must agree. The message
// carries both values on one line so the direction of the drift is readable.
func TestDoctorReportsVersionDrift(t *testing.T) {
	dir := doctorRepo(t)
	// Write project.toml behind state.json's back — the shape writeVersion
	// exists to prevent, and the one doctor has to catch when it happens anyway.
	p, err := project.Load(filepath.Join(dir, ".dross", project.File))
	if err != nil {
		t.Fatal(err)
	}
	p.Project.Version = "1.1.0.0"
	if err := p.Save(filepath.Join(dir, ".dross", project.File)); err != nil {
		t.Fatal(err)
	}

	var out string
	if err := runCmdCapturing(t, &out, Doctor()); err == nil {
		t.Fatal("version drift is an issue")
	}
	for _, want := range []string{"1.1.0.0", "1.0.0.0", "version drift"} {
		if !strings.Contains(out, want) {
			t.Errorf("drift line should contain %q:\n%s", want, out)
		}
	}
}

// TestDoctorReportsMissingProjectVersion (c-6): an absent version is its own
// issue naming project.toml, not an empty-string drift message.
func TestDoctorReportsMissingProjectVersion(t *testing.T) {
	dir := doctorRepo(t)
	mustWrite(t, filepath.Join(dir, ".dross", project.File),
		"[project]\n  name = \"p\"\n\n[runtime]\n  mode = \"native\"\n\n[repo]\n  git_main_branch = \"main\"\n")

	var out string
	if err := runCmdCapturing(t, &out, Doctor()); err == nil {
		t.Fatal("a missing [project].version is an issue")
	}
	if !strings.Contains(out, "no [project].version") {
		t.Errorf("should report the absent version distinctly:\n%s", out)
	}
	if strings.Contains(out, "version drift") {
		t.Errorf("an absent version is not a drift:\n%s", out)
	}
}

// TestDoctorSkipsDriftOnFreshClone (c-6): a checkout with project.toml and no
// state.json is the fresh-clone shape. Comparing against a value that would be
// seeded FROM project.toml is tautological, so the check must skip.
func TestDoctorSkipsDriftOnFreshClone(t *testing.T) {
	dir := doctorRepo(t)
	if err := os.Remove(filepath.Join(dir, ".dross", state.File)); err != nil {
		t.Fatal(err)
	}

	var out string
	err := runCmdCapturing(t, &out, Doctor())
	if strings.Contains(out, "version drift") {
		t.Errorf("a fresh clone has no state.json to drift from:\n%s", out)
	}
	if err != nil && strings.Contains(err.Error(), "state.json") {
		t.Errorf("a missing state.json is not a doctor issue: %v", err)
	}
	if strings.Contains(out, "✗ .dross/state.json — missing") {
		t.Errorf("state.json is not a foundational file any more:\n%s", out)
	}
}

// TestDoctorReportsStaleMilestoneBranchReadOnly (c-7, prune_surface): doctor
// names the branch and points at `dross milestone prune`. It must not delete
// anything itself, and a stale branch must move the exit code.
func TestDoctorReportsStaleMilestoneBranchReadOnly(t *testing.T) {
	dir := doctorRepo(t)
	mustGit(t, dir, "checkout", "-q", "-b", "milestone/v1.0", "main")
	commitOn(t, dir, "milestone/v1.0", "a.txt", "a\n", "feat: a")
	squashOnto(t, dir, "milestone/v1.0", "feat(squash): v1.0")

	var out string
	err := runCmdCapturing(t, &out, Doctor())
	if err == nil {
		t.Fatal("a stale milestone branch must move the exit code, not just advise")
	}
	if !strings.Contains(out, "milestone/v1.0") || !strings.Contains(out, "dross milestone prune") {
		t.Errorf("doctor should name the branch and the prune command:\n%s", out)
	}
	if !gitAllowFail(dir, "rev-parse", "--verify", "--quiet", "refs/heads/milestone/v1.0") {
		t.Error("doctor deleted the branch — it is a diagnostic, prune is the actor")
	}
}

// --- config-trust findings (c-6, c-7) ---

// TestDoctorReportsDashBranchName: a finding printed without moving the exit
// code is a finding nobody acts on — doctor's exit gates CI and pre-push hooks,
// and this class of config kills a command mid-branch-operation.
func TestDoctorReportsDashBranchName(t *testing.T) {
	dir := doctorRepo(t)
	ppath := filepath.Join(dir, ".dross", project.File)
	p, err := project.Load(ppath)
	if err != nil {
		t.Fatal(err)
	}
	p.Repo.GitMainBranch = "--output=/tmp/dross-pwned-doctor"
	if err := p.Save(ppath); err != nil {
		t.Fatal(err)
	}

	var out string
	err = runCmdCapturing(t, &out, Doctor())
	if err == nil {
		t.Error("a hostile git_main_branch must move doctor's exit code, not just print")
	}
	for _, want := range []string{"--output=/tmp/dross-pwned-doctor", "git_main_branch"} {
		if !strings.Contains(out, want) {
			t.Errorf("doctor output does not name %q:\n%s", want, out)
		}
	}
}

// TestDoctorReportsDashBranchPattern: branch_pattern is not a live vector today
// — nothing renders branch names from it — which is exactly why it needs
// reporting rather than refusing. It is broken config that becomes a vector the
// day something starts honouring it.
func TestDoctorReportsDashBranchPattern(t *testing.T) {
	dir := doctorRepo(t)
	ppath := filepath.Join(dir, ".dross", project.File)
	p, err := project.Load(ppath)
	if err != nil {
		t.Fatal(err)
	}
	p.Repo.BranchPattern = "-p/<id>"
	if err := p.Save(ppath); err != nil {
		t.Fatal(err)
	}

	var out string
	if err := runCmdCapturing(t, &out, Doctor()); err == nil {
		t.Error("a leading-dash branch_pattern must move doctor's exit code")
	}
	for _, want := range []string{"branch_pattern", "-p/"} {
		if !strings.Contains(out, want) {
			t.Errorf("doctor output does not name %q:\n%s", want, out)
		}
	}
}

// TestDoctorReportsOffAllowlistAPIBase asserts the escape hatch is named. A
// refusal with no way forward is where a legitimate self-hosted user gets stuck
// and starts editing the guard out — so the locked escape_hatch decision
// requires the literal command, not a description of one.
func TestDoctorReportsOffAllowlistAPIBase(t *testing.T) {
	dir := doctorRepo(t)
	ppath := filepath.Join(dir, ".dross", project.File)
	p, err := project.Load(ppath)
	if err != nil {
		t.Fatal(err)
	}
	p.Remote.URL = "https://github.com/Rivil/dross"
	p.Remote.APIBase = "https://attacker.example"
	if err := p.Save(ppath); err != nil {
		t.Fatal(err)
	}

	var out string
	if err := runCmdCapturing(t, &out, Doctor()); err == nil {
		t.Error("an off-allowlist api_base must move doctor's exit code")
	}
	if !strings.Contains(out, "attacker.example") {
		t.Errorf("doctor output does not name the refused host:\n%s", out)
	}
	if !strings.Contains(out, "dross local set allow_hosts attacker.example") {
		t.Errorf("doctor output does not carry the escape-hatch command verbatim:\n%s", out)
	}
}

// TestDoctorReportsUnignoredLocalToml: doctor is the ONLY command that runs
// against already-onboarded repos, which never re-run init or onboard. Without
// this finding those repos never gain the ignore line at all.
func TestDoctorReportsUnignoredLocalToml(t *testing.T) {
	dir := doctorRepo(t)
	// A .gitignore covering state.json but NOT local.toml — the exact shape a
	// repo onboarded before this phase has.
	mustWrite(t, filepath.Join(dir, ".gitignore"), drossStateIgnorePath+"\n")

	var out string
	if err := runCmdCapturing(t, &out, Doctor()); err == nil {
		t.Error("an unignored local.toml must move doctor's exit code")
	}
	if !strings.Contains(out, drossLocalIgnorePath) {
		t.Errorf("doctor output does not name the missing path:\n%s", out)
	}
}

// TestDoctorReportsOldGit exercises the version floor through the stub seam.
// CI's git is always new enough, so without the seam this check would only ever
// be observed passing — which is indistinguishable from it not working.
func TestDoctorReportsOldGit(t *testing.T) {
	for _, tc := range []struct {
		name    string
		version string
		wantHit bool
	}{
		{"below the floor", "git version 2.23.0\n", true},
		{"at the floor", "git version 2.24.0\n", false},
		{"well above", "git version 2.51.0 (Apple Git-999)\n", false},
		{"platform suffix below", "git version 2.23.1.windows.1\n", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doctorRepo(t)
			prev := gitVersionOutput
			t.Cleanup(func() { gitVersionOutput = prev })
			gitVersionOutput = func() (string, error) { return tc.version, nil }

			var out string
			err := runCmdCapturing(t, &out, Doctor())
			// The ✓ line names --end-of-options too, so match the finding's own
			// wording rather than the flag name.
			hit := strings.Contains(out, "is older than git")
			if hit != tc.wantHit {
				t.Errorf("finding present = %v, want %v, for %q:\n%s", hit, tc.wantHit, tc.version, out)
			}
			if tc.wantHit && err == nil {
				t.Error("an unusable git must move doctor's exit code")
			}
		})
	}
}

// TestDoctorCleanConfigHasNoNewFindings is the false-positive gate. A check
// satisfied by always reporting is worse than no check: it trains the user to
// ignore doctor's output. This repo's own shape — github.com / api.github.com /
// "main" — must produce the ✓ lines and add zero issues.
func TestDoctorCleanConfigHasNoNewFindings(t *testing.T) {
	dir := doctorRepo(t)
	ppath := filepath.Join(dir, ".dross", project.File)
	p, err := project.Load(ppath)
	if err != nil {
		t.Fatal(err)
	}
	p.Remote.URL = "https://github.com/Rivil/dross"
	p.Remote.APIBase = "https://api.github.com"
	p.Repo.GitMainBranch = "main"
	p.Repo.BranchPattern = "phase/<id>"
	if err := p.Save(ppath); err != nil {
		t.Fatal(err)
	}

	var out string
	_ = runCmdCapturing(t, &out, Doctor())

	for _, want := range []string{
		"✓ configured branch names are valid git refs",
		"✓ configured API hosts are within the allowlist derived from [remote].url",
		"✓ " + drossLocalIgnorePath + " is gitignored",
		"supports --end-of-options",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("clean config did not produce %q:\n%s", want, out)
		}
	}
	// And no config-trust finding fired.
	for _, unwanted := range []string{"unsafe git ref", "refusing to contact host", "is not gitignored"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("clean config produced a false finding %q:\n%s", unwanted, out)
		}
	}
}
