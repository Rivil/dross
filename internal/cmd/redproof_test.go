package cmd

import (
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/Rivil/dross/internal/changes"
)

// reachRepo builds a repo whose only origin ref is refs/remotes/origin/main,
// pointing at the returned SHA. update-ref rather than a real clone: what the
// classifier reads is the remote-tracking namespace, and writing it directly
// keeps each test's ref topology explicit instead of a side effect of fetch.
func reachRepo(t *testing.T) (dir, originSHA string) {
	t.Helper()
	dir = t.TempDir()
	gitInit(t, dir, "https://example.com/x.git")
	mustWrite(t, filepath.Join(dir, "a.txt"), "a\n")
	mustGit(t, dir, "add", "a.txt")
	mustGit(t, dir, "commit", "-q", "-m", "a")
	originSHA = mustGit(t, dir, "rev-parse", "HEAD")
	mustGit(t, dir, "update-ref", "refs/remotes/origin/main", originSHA)
	return dir, originSHA
}

// emptyCommit adds an empty commit on the current branch and returns its SHA.
func emptyCommit(t *testing.T, dir, msg string) string {
	t.Helper()
	mustGit(t, dir, "commit", "-q", "--allow-empty", "-m", msg)
	return mustGit(t, dir, "rev-parse", "HEAD")
}

func mustClassify(t *testing.T, dir, sha string) (reachability, string) {
	t.Helper()
	got, why, err := classifyReachability(dir, sha)
	if err != nil {
		t.Fatalf("classifyReachability(%s): %v", short(sha), err)
	}
	return got, why
}

// TestReachabilityRemoteBranch is the green path: a commit an origin ref
// contains is what a fresh clone would get.
func TestReachabilityRemoteBranch(t *testing.T) {
	dir, sha := reachRepo(t)

	if got, why := mustClassify(t, dir, sha); got != reachReachable {
		t.Errorf("commit contained in origin/main classified %q (%s), want %q", got, why, reachReachable)
	}
}

// TestReachabilityIgnoresLocalBranches is the locked reachability_scope
// decision. The c-5 pin passed on the author's machine precisely because a
// local branch still held it, and reddened for everyone else — a local ref is
// state a fresh clone does not have.
func TestReachabilityIgnoresLocalBranches(t *testing.T) {
	dir, _ := reachRepo(t)
	mustGit(t, dir, "checkout", "-q", "-b", "local-only")
	sha := emptyCommit(t, dir, "held by a local branch only")

	got, why := mustClassify(t, dir, sha)
	if got == reachReachable {
		t.Errorf("commit held only by local branch local-only classified reachable (%s) — local refs must not count", why)
	}
	if got != reachUnreachable {
		t.Errorf("classified %q (%s), want %q", got, why, reachUnreachable)
	}
}

// TestReachabilityLooseObject separates existence from reachability. rev-parse
// --verify answers "yes" for an unreferenced loose object right up until gc
// collects it, which is the false green the old fixture check was built on.
func TestReachabilityLooseObject(t *testing.T) {
	dir, base := reachRepo(t)
	mustGit(t, dir, "checkout", "-q", "-b", "doomed")
	sha := emptyCommit(t, dir, "about to lose its ref")
	mustGit(t, dir, "checkout", "-q", "main")
	mustGit(t, dir, "branch", "-qD", "doomed")

	// Precondition: the object is still in the database, so this test is
	// actually exercising the reachable-vs-exists distinction.
	if err := gitNoOut(dir, gitRefArgs("rev-parse", []string{"--verify", "--quiet"}, sha+"^{commit}")...); err != nil {
		t.Fatalf("setup: %s was already collected, so the loose-object case is not under test", short(sha))
	}
	if got, why := mustClassify(t, dir, sha); got != reachUnreachable {
		t.Errorf("loose object with no ref pointing at it classified %q (%s), want %q — `rev-parse --verify` succeeding is not reachability",
			got, why, reachUnreachable)
	}
	if got, _ := mustClassify(t, dir, base); got != reachReachable {
		t.Error("the control commit stopped classifying reachable — the fixture, not the case, is broken")
	}
}

// TestReachabilityMissingObject is the post-gc case c-2 is actually about: the
// pinned commit is gone from the database. git exits non-zero, and folding that
// into cannot-determine would turn the one finding that matters into a shrug.
func TestReachabilityMissingObject(t *testing.T) {
	dir, _ := reachRepo(t)
	const gone = "0123456789abcdef0123456789abcdef01234567"

	got, why := mustClassify(t, dir, gone)
	if got == reachIndeterminate {
		t.Errorf("a SHA absent from the object database classified %q (%s) — git's non-zero exit must not be folded into the indeterminate arm", got, why)
	}
	if got != reachUnreachable {
		t.Errorf("classified %q (%s), want %q", got, why, reachUnreachable)
	}
}

// TestReachabilityIndeterminate asserts the third outcome is distinct from the
// other two, for both of its causes: no origin refs to judge against, and a
// shallow clone whose history genuinely does not reach back that far.
func TestReachabilityIndeterminate(t *testing.T) {
	t.Run("no origin refs", func(t *testing.T) {
		dir := t.TempDir()
		gitInit(t, dir, "https://example.com/x.git")
		mustWrite(t, filepath.Join(dir, "a.txt"), "a\n")
		mustGit(t, dir, "add", "a.txt")
		mustGit(t, dir, "commit", "-q", "-m", "a")
		sha := mustGit(t, dir, "rev-parse", "HEAD")

		got, why := mustClassify(t, dir, sha)
		if got != reachIndeterminate {
			t.Errorf("repo with no %s* refs classified %q (%s), want %q", originRefGlob, got, why, reachIndeterminate)
		}
	})

	t.Run("shallow clone", func(t *testing.T) {
		src, oldSHA := reachRepo(t)
		emptyCommit(t, src, "newer")
		dst := filepath.Join(t.TempDir(), "shallow")
		clone := exec.Command("git", "clone", "-q", "--depth", "1", "file://"+src, dst)
		if out, err := clone.CombinedOutput(); err != nil {
			t.Fatalf("shallow clone: %v\n%s", err, out)
		}
		if shallow := mustGit(t, dst, "rev-parse", "--is-shallow-repository"); shallow != "true" {
			t.Fatalf("setup: clone is not shallow (%s)", shallow)
		}

		got, why := mustClassify(t, dst, oldSHA)
		if got == reachUnreachable {
			t.Errorf("shallow clone classified a truncated-away commit %q (%s) — an absence of history is not a verdict", got, why)
		}
		if got != reachIndeterminate {
			t.Errorf("classified %q (%s), want %q", got, why, reachIndeterminate)
		}
	})

	t.Run("outcomes are distinct", func(t *testing.T) {
		if reachReachable == reachUnreachable || reachUnreachable == reachIndeterminate || reachReachable == reachIndeterminate {
			t.Fatal("the three reachability outcomes are not distinct values")
		}
	})
}

// TestReachabilityRejectsEmptySHA pins that a record with no pin is a caller
// error surfaced here, not an empty ref handed to git two layers down.
func TestReachabilityRejectsEmptySHA(t *testing.T) {
	dir, _ := reachRepo(t)
	if got, _, err := classifyReachability(dir, "  "); err == nil {
		t.Errorf("empty SHA classified %q instead of erroring", got)
	}
}

// pinFixture writes a .dross root under a temp dir with one changes.json per
// entry: a nil pin means the phase recorded none, which is the shape of the ~30
// dirs that predate the field.
func pinFixture(t *testing.T, pins map[string]*changes.RedProof) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), ".dross")
	for phaseID, pin := range pins {
		path := changes.FilePath(root, phaseID)
		c := changes.New(phaseID)
		c.Base = "main"
		c.RedProof = pin
		if err := c.Save(path); err != nil {
			t.Fatalf("save changes for %s: %v", phaseID, err)
		}
	}
	return root
}

func pinPhases(pins []redProofPin) []string {
	out := make([]string, 0, len(pins))
	for _, p := range pins {
		out = append(out, p.Phase)
	}
	return out
}

// TestDiscoverRedProofPinsFindsLaterPhase is c-5: discovery is by convention,
// so a proof recorded by a phase written after this code is checked with no new
// code. A hardcoded fixtures/hostile-config-c5 path passes every test about the
// pin that exists today and silently ignores every one recorded after it.
func TestDiscoverRedProofPinsFindsLaterPhase(t *testing.T) {
	root := pinFixture(t, map[string]*changes.RedProof{
		"config-trust-hardening": {SHA: "a6ef729", Doc: "fixtures/hostile-config-c5/RUN.md"},
		"some-later-phase":       {SHA: "deadbee", Doc: "fixtures/later/RUN.md"},
	})

	pins, err := discoverRedProofPins(root)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	got := pinPhases(pins)
	if len(got) != 2 {
		t.Fatalf("discovered %v, want both phases' pins", got)
	}
	var found bool
	for _, p := range pins {
		if p.Phase == "some-later-phase" {
			found = true
			if p.SHA != "deadbee" || p.Doc != "fixtures/later/RUN.md" {
				t.Errorf("later phase's pin = %+v, want its own sha/doc", p)
			}
		}
	}
	if !found {
		t.Errorf("a pin recorded under a later phase dir is absent from %v — discovery is not by convention", got)
	}
}

// TestDiscoverRedProofPinsSkipsUnpinnedPhases: a phase with no red proof is the
// normal case, not an error. Erroring would make the check unusable in this
// repo, where ~30 dirs carry none.
func TestDiscoverRedProofPinsSkipsUnpinnedPhases(t *testing.T) {
	root := pinFixture(t, map[string]*changes.RedProof{
		"pinned":     {SHA: "a6ef729", Doc: "fixtures/hostile-config-c5/RUN.md"},
		"unpinned-a": nil,
		"unpinned-b": nil,
	})

	pins, err := discoverRedProofPins(root)
	if err != nil {
		t.Fatalf("discover errored on a phase with no red_proof entry: %v", err)
	}
	if got := pinPhases(pins); len(got) != 1 || got[0] != "pinned" {
		t.Errorf("discovered %v, want only [pinned]", got)
	}
}

// TestDiscoverRedProofPinsNoPhases covers the empty repo: no phases dir at all
// is nothing to check, not a failure.
func TestDiscoverRedProofPinsNoPhases(t *testing.T) {
	pins, err := discoverRedProofPins(filepath.Join(t.TempDir(), ".dross"))
	if err != nil {
		t.Fatalf("discover on a root with no phases dir: %v", err)
	}
	if len(pins) != 0 {
		t.Errorf("discovered %v from nothing", pinPhases(pins))
	}
}

// TestRedProofDocSHA pins the parser against the form the live doc actually
// carries — emphasised and backticked, because it is written for a human reader
// first. A parser that only handles the bare form reports "no pin" for a doc
// that plainly has one.
func TestRedProofDocSHA(t *testing.T) {
	const sha = "a6ef7295996db1d737be2ae2183f45e376ba3c19"
	for _, tc := range []struct {
		name, line, want string
	}{
		{"emphasised and backticked", "**base commit: `" + sha + "`** (`phase dross-repair`)", sha},
		{"bare", "base commit: " + sha, sha},
		{"capitalised", "Base commit: " + sha, sha},
		{"absent", "no pin here at all", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			mustWrite(t, filepath.Join(dir, "RUN.md"), "# proof\n\n"+tc.line+"\n")
			got, err := redProofDocSHA(dir, "RUN.md")
			if err != nil {
				t.Fatalf("redProofDocSHA: %v", err)
			}
			if got != tc.want {
				t.Errorf("parsed %q from %q, want %q", got, tc.line, tc.want)
			}
		})
	}
}

// TestRedProofDocSHAMissingFile: a pin naming a doc that is not there is an
// error the caller must see, not an empty SHA that reads as "no pin recorded".
func TestRedProofDocSHAMissingFile(t *testing.T) {
	if _, err := redProofDocSHA(t.TempDir(), "nope/RUN.md"); err == nil {
		t.Error("expected an error for a pin naming a doc that does not exist")
	}
}
