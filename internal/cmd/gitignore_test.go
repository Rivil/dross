package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// checkIgnored reports whether git considers path ignored in repoDir.
func checkIgnored(t *testing.T, repoDir, path string) bool {
	t.Helper()
	err := exec.Command("git", "-C", repoDir, "check-ignore", "-q", path).Run()
	return err == nil
}

// TestStateJSONNotTracked is the migration itself, asserted against this repo:
// once `git rm --cached` has run, no commit carries state.json, so no checkout
// can replace the live one. Re-adding it puts the incident back.
func TestStateJSONNotTracked(t *testing.T) {
	root := repoRootFromTest(t)
	out, err := exec.Command("git", "-C", root, "ls-files", drossStateIgnorePath).Output()
	if err != nil {
		t.Fatalf("git ls-files: %v", err)
	}
	if got := strings.TrimSpace(string(out)); got != "" {
		t.Errorf("%s is still tracked (%q) — run `git rm --cached %s`",
			drossStateIgnorePath, got, drossStateIgnorePath)
	}
}

// TestRepoGitignoreCoversState: the working copy has to be ignored as well as
// untracked, or the next `git add .dross` puts it straight back.
func TestRepoGitignoreCoversState(t *testing.T) {
	root := repoRootFromTest(t)
	if !checkIgnored(t, root, drossStateIgnorePath) {
		t.Errorf("%s is not ignored in this repo's .gitignore", drossStateIgnorePath)
	}
	// Anchoring: with the pattern wrong (or absent), the live file shows up as
	// untracked noise in every `git status` from here on. A staged deletion is
	// fine — that is the untrack commit itself, mid-flight.
	out, err := exec.Command("git", "-C", root, "status", "--porcelain", "--", drossStateIgnorePath).Output()
	if err != nil {
		t.Fatalf("git status: %v", err)
	}
	if got := strings.TrimSpace(string(out)); strings.HasPrefix(got, "??") {
		t.Errorf("git status reports %s as untracked: %q — the ignore pattern is not anchored to the repo root",
			drossStateIgnorePath, got)
	}
}

// TestInitScaffoldsStateIgnore / TestOnboardScaffoldsStateIgnore: both scaffolds
// have to ship the ignore, or a fresh repo re-acquires the bug on its first ship.
func TestInitScaffoldsStateIgnore(t *testing.T) {
	dir := t.TempDir()
	gitInit(t, dir, "")
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatalf("init: %v", err)
	}
	if !checkIgnored(t, dir, drossStateIgnorePath) {
		t.Errorf("init did not scaffold the state.json ignore:\n%s", readIfExists(t, filepath.Join(dir, ".gitignore")))
	}
}

func TestOnboardScaffoldsStateIgnore(t *testing.T) {
	dir := t.TempDir()
	gitInit(t, dir, "")
	chdir(t, dir)
	if err := runCmd(t, Onboard()); err != nil {
		t.Fatalf("onboard: %v", err)
	}
	if !checkIgnored(t, dir, drossStateIgnorePath) {
		t.Errorf("onboard did not scaffold the state.json ignore:\n%s", readIfExists(t, filepath.Join(dir, ".gitignore")))
	}
}

// TestGitignoredStateSurvivesAdd is the property the ignore buys: the directory
// form every dross prompt uses stages the phase artefacts and skips the live
// state. An explicit path would hard-fail here, which is why none remain.
func TestGitignoredStateSurvivesAdd(t *testing.T) {
	dir := t.TempDir()
	gitInit(t, dir, "")
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatalf("init: %v", err)
	}
	mustWrite(t, filepath.Join(dir, ".dross", "phases", "auth", "spec.toml"), "id = \"auth\"\n")

	mustGit(t, dir, "add", ".dross")
	staged := mustGit(t, dir, "diff", "--cached", "--name-only")
	if strings.Contains(staged, drossStateIgnorePath) {
		t.Errorf("`git add .dross` staged the ignored state.json:\n%s", staged)
	}
	if !strings.Contains(staged, ".dross/phases/auth/spec.toml") {
		t.Errorf("`git add .dross` should still stage the phase artefacts:\n%s", staged)
	}
}

// TestEnsureDrossGitignoreIsIdempotent: init and onboard both call it, and a
// repo that runs onboard twice must not accumulate the block.
func TestEnsureDrossGitignoreIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".gitignore")
	for i := 0; i < 3; i++ {
		if err := ensureDrossGitignore(dir); err != nil {
			t.Fatalf("call %d: %v", i+1, err)
		}
	}
	body := readIfExists(t, path)
	if n := strings.Count(body, drossStateIgnorePath+"\n"); n != 1 {
		t.Errorf("pattern appears %d times, want 1:\n%s", n, body)
	}
}

// TestEnsureDrossGitignoreRespectsBroaderPattern: a repo that already ignores
// the file some other way is left byte-for-byte alone. Appending anyway is
// harmless to git and noisy to the human reading the diff.
func TestEnsureDrossGitignoreRespectsBroaderPattern(t *testing.T) {
	for _, existing := range []string{
		".dross/*.json\n",
		".dross/\n",
		"# comment\n/.dross/state.json\n",
		"node_modules/\n.dross/state.json\n",
	} {
		t.Run(strings.TrimSpace(strings.ReplaceAll(existing, "\n", " ")), func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, ".gitignore")
			if err := os.WriteFile(path, []byte(existing), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := ensureDrossGitignore(dir); err != nil {
				t.Fatal(err)
			}
			if got := readIfExists(t, path); got != existing {
				t.Errorf("an already-covering .gitignore was rewritten:\nwant %q\ngot  %q", existing, got)
			}
		})
	}
}

// TestEnsureDrossGitignoreAppendsToExisting: a pre-existing .gitignore keeps
// everything it had, and the block lands after it rather than overwriting.
func TestEnsureDrossGitignoreAppendsToExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".gitignore")
	const existing = "node_modules/\ndist/\n"
	if err := os.WriteFile(path, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ensureDrossGitignore(dir); err != nil {
		t.Fatal(err)
	}
	body := readIfExists(t, path)
	if !strings.HasPrefix(body, existing) {
		t.Errorf("existing entries were not preserved:\n%s", body)
	}
	if !strings.Contains(body, drossStateIgnorePath) {
		t.Errorf("the block was not appended:\n%s", body)
	}
	// A negation is a deliberate choice, and re-appending would silently undo it.
	dir2 := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir2, ".gitignore"), []byte("!.dross/state.json\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ensureDrossGitignore(dir2); err != nil {
		t.Fatal(err)
	}
	if body := readIfExists(t, filepath.Join(dir2, ".gitignore")); !strings.Contains(body, drossStateIgnorePath+"\n") {
		t.Errorf("a negation must not read as coverage:\n%s", body)
	}
}

// readIfExists returns a file's contents, or "" when it is absent.
func readIfExists(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(b)
}

// TestDocsDoNotClaimStateIsTracked (c-1): a doc that still says state.json is
// committed sends the reader looking for a file no commit has, and a prompt
// that does hard-fails the slash command it belongs to. Scanned rather than
// eyeballed so the claim cannot creep back in a later edit.
func TestDocsDoNotClaimStateIsTracked(t *testing.T) {
	root := repoRootFromTest(t)
	// Phrases that assert the file rides history. Matched case-insensitively on
	// lines that also mention state.json, so unrelated prose is untouched.
	claims := []string{
		"state.json is committed",
		"state.json is tracked",
		"state.json rides",
		"state.json does ride",
		"commits state.json",
	}
	for _, rel := range []string{
		"README.md",
		"docs/dross.1",
		"assets/prompts/init.md",
		"assets/prompts/pause.md",
		"internal/cmd/local.go",
		".gitignore",
	} {
		b, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		lower := strings.ToLower(string(b))
		for _, claim := range claims {
			if strings.Contains(lower, claim) {
				t.Errorf("%s still asserts %q — state.json is machine-local and gitignored", rel, claim)
			}
		}
	}
}

// TestDoctorFoundationalDocMatchesBehaviour (c-1/c-6): README's doctor row lists
// doctor's foundational files. It listed state.json, which t-9 removed from the
// set — a doc that contradicts the command it documents is worse than none.
func TestDoctorFoundationalDocMatchesBehaviour(t *testing.T) {
	b, err := os.ReadFile(filepath.Join(repoRootFromTest(t), "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	var row string
	for _, line := range strings.Split(string(b), "\n") {
		if strings.Contains(line, "`dross doctor`") && strings.Contains(line, "foundational files") {
			row = line
		}
	}
	if row == "" {
		t.Fatal("README has no `dross doctor` row naming its foundational files")
	}
	head, _, _ := strings.Cut(row, "`[remote]`")
	if strings.Contains(head, "state.json`)") {
		t.Errorf("README lists state.json among doctor's foundational files:\n%s", row)
	}
	for _, want := range []string{"`project.toml`", "`rules.toml`"} {
		if !strings.Contains(head, want) {
			t.Errorf("README's foundational list should still name %s:\n%s", want, row)
		}
	}
}
