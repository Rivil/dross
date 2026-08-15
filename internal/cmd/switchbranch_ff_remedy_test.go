package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ffRemedyRepo builds the shape the guard refuses on, with a real
// remote-tracking ref so the fast-forward question is answerable.
//
// Layout: `legacy` is a branch that still tracks .dross/state.json (the
// pre-migration vintage the guard exists for), and origin/legacy is whatever the
// caller's setup function makes it. The live state.json is present and
// gitignored on the branch the test sits on, which is what there is to lose.
func ffRemedyRepo(t *testing.T, setupRemote func(t *testing.T, dir, remoteDir string)) string {
	t.Helper()
	dir := t.TempDir()
	remoteDir := t.TempDir()

	// A real bare-ish origin so refs/remotes/origin/* exist for real rather
	// than being faked with update-ref — the guard reads them the way git does.
	mustGitBare(t, remoteDir)
	gitInit(t, dir, remoteDir)
	chdir(t, dir)

	mustWrite(t, filepath.Join(dir, "README.md"), "seed\n")
	mustGit(t, dir, "add", "README.md")
	mustGit(t, dir, "commit", "-qm", "seed")

	// The legacy branch: it TRACKS .dross/state.json.
	mustGit(t, dir, "checkout", "-q", "-b", "legacy")
	mustWrite(t, filepath.Join(dir, ".dross", "state.json"), `{"version":"9.9.9.9"}`+"\n")
	mustGit(t, dir, "add", "-f", ".dross/state.json")
	mustGit(t, dir, "commit", "-qm", "legacy tracks state.json")

	setupRemote(t, dir, remoteDir)

	// Back to main, with a live gitignored state.json to protect.
	mustGit(t, dir, "checkout", "-q", "main")
	mustWrite(t, filepath.Join(dir, ".gitignore"), ".dross/state.json\n")
	mustGit(t, dir, "add", ".gitignore")
	mustGit(t, dir, "commit", "-qm", "ignore state")
	mustWrite(t, filepath.Join(dir, ".dross", "state.json"), `{"version":"1.0.0.0"}`+"\n")

	mustGit(t, dir, "fetch", "-q", "origin")
	return dir
}

func mustGitBare(t *testing.T, dir string) {
	t.Helper()
	mustGit(t, dir, "init", "-q", "--bare", "-b", "main")
}

// pushCleanAhead makes origin/legacy one commit AHEAD of local legacy, with the
// tracked state.json removed — the exact shape the fast-forward fixes.
func pushCleanAhead(t *testing.T, dir, _ string) {
	t.Helper()
	mustGit(t, dir, "push", "-q", "origin", "legacy")
	mustGit(t, dir, "rm", "-q", "--cached", ".dross/state.json")
	mustGit(t, dir, "commit", "-qm", "untrack state.json")
	mustGit(t, dir, "push", "-q", "origin", "legacy")
	// Rewind local legacy so it is strictly behind origin/legacy.
	mustGit(t, dir, "reset", "-q", "--hard", "HEAD~1")
}

func guardRefusal(t *testing.T, dir, ref string) string {
	t.Helper()
	err := guardLiveState(dir, ref)
	if err == nil {
		t.Fatalf("guardLiveState(%q) did not refuse — the fixture is not the shape under test", ref)
	}
	return err.Error()
}

// TestRefusalLeadsWithFastForward is c-1. Leading is the criterion, not merely
// mentioning: the ff line must come BEFORE the move-aside block, or it is a
// footnote under the expensive remedy it is supposed to replace.
func TestRefusalLeadsWithFastForward(t *testing.T) {
	dir := ffRemedyRepo(t, pushCleanAhead)
	msg := guardRefusal(t, dir, "legacy")

	ff := strings.Index(msg, "git branch --force legacy origin/legacy")
	if ff < 0 {
		t.Fatalf("the refusal does not offer the fast-forward:\n%s", msg)
	}
	aside := strings.Index(msg, "move the live copy aside")
	if aside < 0 {
		t.Fatalf("the move-aside remedy vanished:\n%s", msg)
	}
	if ff > aside {
		t.Errorf("the fast-forward is offered AFTER the move-aside remedy — it is the cheapest fix and must lead:\n%s", msg)
	}
	if !strings.Contains(msg, "only BEHIND") {
		t.Errorf("the refusal does not say why the fast-forward is enough:\n%s", msg)
	}
}

// TestNoFastForwardWhenRemoteStillCarriesIt: the advice has to be CORRECT, not
// merely cheap. Fast-forwarding onto a remote that still tracks the file moves
// the refusal rather than clearing it — the user does the work, re-runs, and is
// refused again by the same guard for the same reason.
func TestNoFastForwardWhenRemoteStillCarriesIt(t *testing.T) {
	dir := ffRemedyRepo(t, func(t *testing.T, dir, _ string) {
		t.Helper()
		mustGit(t, dir, "push", "-q", "origin", "legacy")
		// Ahead, but the file is still tracked upstream.
		mustWrite(t, filepath.Join(dir, "other.txt"), "x\n")
		mustGit(t, dir, "add", "other.txt")
		mustGit(t, dir, "commit", "-qm", "unrelated")
		mustGit(t, dir, "push", "-q", "origin", "legacy")
		mustGit(t, dir, "reset", "-q", "--hard", "HEAD~1")
	})
	msg := guardRefusal(t, dir, "legacy")

	if strings.Contains(msg, "git branch --force") {
		t.Errorf("a fast-forward was offered onto a remote that still carries the file — it would move the refusal, not clear it:\n%s", msg)
	}
}

// TestNoFastForwardWithoutARemote: nothing to fast-forward from.
func TestNoFastForwardWithoutARemote(t *testing.T) {
	dir := ffRemedyRepo(t, func(t *testing.T, _, _ string) { t.Helper() })
	msg := guardRefusal(t, dir, "legacy")

	if strings.Contains(msg, "git branch --force") {
		t.Errorf("a fast-forward was offered for a branch with no remote-tracking ref:\n%s", msg)
	}
}

// TestNoFastForwardWhenDiverged: a diverged branch cannot fast-forward at all,
// so the lead line would be an instruction that errors — worse than no
// suggestion, because it looks authoritative.
func TestNoFastForwardWhenDiverged(t *testing.T) {
	dir := ffRemedyRepo(t, func(t *testing.T, dir, _ string) {
		t.Helper()
		mustGit(t, dir, "push", "-q", "origin", "legacy")
		mustGit(t, dir, "rm", "-q", "--cached", ".dross/state.json")
		mustGit(t, dir, "commit", "-qm", "untrack state.json")
		mustGit(t, dir, "push", "-q", "origin", "legacy")
		// Local gets its own commit instead of the remote's: diverged, not behind.
		mustGit(t, dir, "reset", "-q", "--hard", "HEAD~1")
		mustWrite(t, filepath.Join(dir, "local-only.txt"), "x\n")
		mustGit(t, dir, "add", "local-only.txt")
		mustGit(t, dir, "commit", "-qm", "local only")
	})
	msg := guardRefusal(t, dir, "legacy")

	if strings.Contains(msg, "git branch --force") {
		t.Errorf("a fast-forward was offered for a diverged branch:\n%s", msg)
	}
}

// TestBothRemediesSurvive: the fast-forward is an addition and a reordering,
// never a replacement. Every shape must still carry the two remedies that
// always work.
func TestBothRemediesSurvive(t *testing.T) {
	for name, setup := range map[string]func(t *testing.T, dir, remoteDir string){
		"behind and clean": pushCleanAhead,
		"no remote":        func(t *testing.T, _, _ string) { t.Helper() },
	} {
		t.Run(name, func(t *testing.T) {
			dir := ffRemedyRepo(t, setup)
			msg := guardRefusal(t, dir, "legacy")
			for _, want := range []string{"move the live copy aside", "git rm --cached", ".dross/state.json"} {
				if !strings.Contains(msg, want) {
					t.Errorf("the refusal lost %q:\n%s", want, msg)
				}
			}
		})
	}
}

// TestGuardStillPassesACleanRef is the control: the guard must stay silent for
// a ref that carries no copy, or every one of the assertions above is measuring
// a guard that refuses everything.
func TestGuardStillPassesACleanRef(t *testing.T) {
	dir := ffRemedyRepo(t, pushCleanAhead)
	if err := guardLiveState(dir, "main"); err != nil {
		t.Errorf("the guard refused a ref that carries no tracked state.json: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".dross", "state.json")); err != nil {
		t.Errorf("the live file went missing: %v", err)
	}
}
