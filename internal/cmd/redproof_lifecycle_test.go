package cmd

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/changes"
)

// doomedShipFixture is shipFixture plus the thing c-7 is about: a red proof
// pinned to a commit that lives ONLY on phase/x, with phase/x already pushed so
// origin's copy is the single ref holding it up. That is the state the real c-5
// pin was in the moment before its PR was squash-merged.
//
// It returns the repo dir, the bare remote, the pinned SHA and the doc path.
func doomedShipFixture(t *testing.T) (dir, remoteDir, pinned, doc string) {
	t.Helper()
	dir = shipFixture(t, "https://forge.example/me/p.git")
	remoteDir = t.TempDir()
	mustGit(t, remoteDir, "init", "-q", "--bare")
	mustGit(t, dir, "remote", "set-url", "origin", remoteDir)
	// origin/main must exist, or reachability is indeterminate rather than a
	// verdict — and every plan would be a no-op for the wrong reason.
	mustGit(t, dir, "push", "-q", "origin", "main:refs/heads/main")

	doc = "fixtures/proof/RUN.md"
	placeholder := "0000000000000000000000000000000000000000"
	mustWrite(t, filepath.Join(dir, doc), "# proof\n\n**base commit: `"+placeholder+"`**\n\nBASE="+placeholder+"\n")
	mustGit(t, dir, "add", ".")
	mustGit(t, dir, "commit", "-q", "-m", "docs: red proof")
	pinned = mustGit(t, dir, "rev-parse", "HEAD")
	mustWrite(t, filepath.Join(dir, doc), "# proof\n\n**base commit: `"+pinned+"`**\n\nBASE="+pinned+"\n")
	mustGit(t, dir, "add", ".")
	mustGit(t, dir, "commit", "-q", "-m", "docs: pin the red proof")

	if err := runCmd(t, Phase(), "red-proof", "set", "x", "--sha", pinned, "--doc", doc); err != nil {
		t.Fatalf("red-proof set: %v", err)
	}
	mustGit(t, dir, "add", ".dross")
	mustGit(t, dir, "commit", "-q", "-m", "chore(dross): pin the red proof")

	// Published, so origin/phase/x is what holds the pinned commit up — and so
	// deleting it is what makes the pin rot.
	mustGit(t, dir, "push", "-q", "origin", "phase/x:refs/heads/phase/x")
	mustGit(t, dir, "fetch", "-q", "origin")
	return dir, remoteDir, pinned, doc
}

// stubForgeForShip points ship's PR-open at a local mock and authorizes the
// host machine-locally, mirroring the pattern the other full-push ship tests
// use.
func stubForgeForShip(t *testing.T, dir string) {
	t.Helper()
	t.Setenv("MOCK_FORGEJO_TOKEN", "secret")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/pulls") && r.Method == "POST" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"number":99,"html_url":"https://forge.example/me/p/pulls/99"}`))
			return
		}
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(server.Close)

	if err := runCmd(t, Local(), "set", "allow_hosts", server.Listener.Addr().String()); err != nil {
		t.Fatal(err)
	}
	if err := runCmd(t, Project(), "set", "remote.api_base", server.URL); err != nil {
		t.Fatal(err)
	}
	mustGit(t, dir, "add", ".")
	mustGit(t, dir, "commit", "-q", "-m", "test: point api_base at mock")
}

// simulateSquashMerge does what the provider does to the phase branch: the work
// lands on main as one new commit and phase/x disappears from both sides.
func simulatePhaseXSquashMerge(t *testing.T, dir string) {
	t.Helper()
	mustGit(t, dir, "checkout", "-q", "main")
	mustGit(t, dir, "merge", "-q", "--squash", "phase/x")
	mustGit(t, dir, "commit", "-q", "-m", "phase x (#99)")
	mustGit(t, dir, "push", "-q", "origin", "main:refs/heads/main")
	mustGit(t, dir, "push", "-q", "origin", "--delete", "phase/x")
	mustGit(t, dir, "branch", "-q", "-D", "phase/x")
	mustGit(t, dir, "fetch", "-q", "--prune", "origin")
}

// TestShipRepointsDoomedPin is c-7 end to end: the pin survives its own
// branch's death, with nobody running repoint by hand.
func TestShipRepointsDoomedPin(t *testing.T) {
	dir, _, pinned, doc := doomedShipFixture(t)
	stubForgeForShip(t, dir)

	if err := runCmd(t, Ship()); err != nil {
		t.Fatalf("ship: %v", err)
	}
	simulatePhaseXSquashMerge(t, dir)

	root := filepath.Join(dir, ".dross")
	c, err := changes.Load(changes.FilePath(root, "x"), "x")
	if err != nil {
		t.Fatal(err)
	}
	if c.RedProof == nil {
		t.Fatal("the pin vanished")
	}
	if c.RedProof.SHA == pinned {
		t.Fatal("the pin still names the doomed commit after ship + merge")
	}
	verdict, why, err := classifyReachability(dir, c.RedProof.SHA)
	if err != nil {
		t.Fatal(err)
	}
	if verdict != reachReachable {
		t.Errorf("the repointed pin is %s after the merge — %s", verdict, why)
	}
	lines, present := redProofChecks(root, dir)
	if !present {
		t.Fatal("doctor sees no pin at all")
	}
	if hasDoctorIssue(lines, "x") {
		t.Errorf("doctor still reports an issue after the merge: %+v", lines)
	}
	docSHA, err := redProofDocSHA(dir, doc)
	if err != nil {
		t.Fatal(err)
	}
	if docSHA != c.RedProof.SHA {
		t.Errorf("doc pins %q, record pins %q", docSHA, c.RedProof.SHA)
	}
}

// TestDoomedPinIsNotRotted keeps the two predicates apart. Collapsing them
// would make every sound pin on a live branch look broken (c-5), and the
// tension is asserted at the decision layer so this test needs no verb.
func TestDoomedPinIsNotRotted(t *testing.T) {
	dir, _, _, _ := doomedShipFixture(t)
	root := filepath.Join(dir, ".dross")

	pins, err := discoverRedProofPins(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(pins) != 1 {
		t.Fatalf("want exactly one pin, got %v", pins)
	}

	plain, err := planRedProofRepoint(root, dir, pins[0], nil)
	if err != nil {
		t.Fatalf("plan without exclusion: %v", err)
	}
	if plain.Verdict != repointNothingToDo {
		t.Errorf("a pin held by a LIVE branch reports %q — it is not rotted yet (c-5)", plain.Verdict)
	}

	doomed, err := doomedRedProofPlans(root, dir, "x")
	if err != nil {
		t.Fatalf("doomed plans: %v", err)
	}
	if len(doomed) != 1 || doomed[0].Verdict != repointRepair {
		t.Errorf("the ship hook does not see the doomed pin: %+v", doomed)
	}
}

// TestShipRepointCommitScope: the hook's own commit must be complete (so ship's
// clean-tree gate passes) and narrow (so it carries nothing else).
func TestShipRepointCommitScope(t *testing.T) {
	dir, _, _, doc := doomedShipFixture(t)
	stubForgeForShip(t, dir)

	if err := runCmd(t, Ship()); err != nil {
		t.Fatalf("ship: %v", err)
	}

	sha := mustGit(t, dir, "log", "--format=%H", "--grep", "repoint red proof for x", "-1", "phase/x")
	if sha == "" {
		t.Fatal("the hook left no commit on phase/x")
	}
	got := strings.Split(mustGit(t, dir, "diff-tree", "--no-commit-id", "--name-only", "-r", sha), "\n")
	want := map[string]bool{".dross/phases/x/" + changes.File: true, doc: true}
	if len(got) != len(want) {
		t.Fatalf("the repoint commit touched %v, want exactly %v", got, want)
	}
	for _, f := range got {
		if !want[strings.TrimSpace(f)] {
			t.Errorf("the repoint commit touched %q, which is outside its scope", f)
		}
	}

	// The gate the hook has to satisfy: nothing left dirty behind it.
	if status := mustGit(t, dir, "status", "--porcelain"); status != "" {
		t.Errorf("the tree is dirty after ship: %q", status)
	}
}

// TestShipRepointLeavesBaseClean: the hook writes on the phase branch, not on
// the base. A doomed-pin fixture is used deliberately — with a sound pin the
// hook never fires and this test would pass for a hook writing all over main.
func TestShipRepointLeavesBaseClean(t *testing.T) {
	dir, _, _, _ := doomedShipFixture(t)
	stubForgeForShip(t, dir)
	mainBefore := mustGit(t, dir, "rev-parse", "main")

	if err := runCmd(t, Ship()); err != nil {
		t.Fatalf("ship: %v", err)
	}

	sha := mustGit(t, dir, "log", "--format=%H", "--grep", "repoint red proof for x", "-1", "phase/x")
	if sha == "" {
		t.Fatal("the hook left no commit on phase/x — this test would be vacuous")
	}
	if mustGit(t, dir, "rev-parse", "main") != mainBefore {
		t.Error("the hook moved main")
	}
	if err := gitNoOut(dir, "merge-base", "--is-ancestor", sha, "main"); err == nil {
		t.Error("the repoint commit is on main; it belongs on the phase branch")
	}
	if _, err := pushBaseIfAheadDrossOnly(dir, "main"); err != nil {
		t.Errorf("the base safety net refused after the hook ran: %v", err)
	}
}

// TestShipRepointsUnverified: with no replay recorded the repair still happens
// — c-1's promise does not depend on a replay existing — and the output says
// plainly that nothing checked it.
func TestShipRepointsUnverified(t *testing.T) {
	dir, _, pinned, _ := doomedShipFixture(t)
	stubForgeForShip(t, dir)

	var err error
	out := captureStdout(t, func() { err = runCmd(t, Ship()) })
	if err != nil {
		t.Fatalf("ship: %v\n%s", err, out)
	}
	if !strings.Contains(out, "unverified") {
		t.Errorf("ship did not report the repair as unverified:\n%s", out)
	}
	c, lerr := changes.Load(changes.FilePath(filepath.Join(dir, ".dross"), "x"), "x")
	if lerr != nil {
		t.Fatal(lerr)
	}
	if c.RedProof.SHA == pinned {
		t.Error("the pin was not repaired")
	}
}

// TestCompleteWarnsAndFinishes: complete names the phase and the repair verb,
// then finishes anyway. Refusing there would leave a merged phase
// uncompletable, which is strictly worse than a rotted pin.
func TestCompleteWarnsAndFinishes(t *testing.T) {
	dir, phaseID := completeFixture(t)
	stubPRMerged(t, true)
	root := filepath.Join(dir, ".dross")

	// Publish the phase branch and pin the proof to a commit on it — the
	// doomed state complete is being asked to notice.
	doc := "fixtures/proof/RUN.md"
	pinned := mustGit(t, dir, "rev-parse", "HEAD")
	mustWrite(t, filepath.Join(dir, doc), "**base commit: `"+pinned+"`**\n")
	mustGit(t, dir, "add", ".")
	mustGit(t, dir, "commit", "-q", "-m", "docs: red proof")
	pinned = mustGit(t, dir, "rev-parse", "HEAD~1")
	if err := runCmd(t, Phase(), "red-proof", "set", phaseID, "--sha", pinned, "--doc", doc); err != nil {
		t.Fatalf("red-proof set: %v", err)
	}
	mustGit(t, dir, "add", ".dross")
	mustGit(t, dir, "commit", "-q", "-m", "chore(dross): pin the red proof")
	mustGit(t, dir, "push", "-q", "origin", "phase/"+phaseID+":refs/heads/phase/"+phaseID)
	mustGit(t, dir, "fetch", "-q", "origin")

	// Sanity: the fixture really is doomed, or the warning below proves nothing.
	plans, err := doomedRedProofPlans(root, dir, phaseID)
	if err != nil || len(plans) != 1 {
		t.Fatalf("fixture is not doomed (%v): %+v", err, plans)
	}

	var cerr error
	out := captureStdout(t, func() { cerr = runCmd(t, Phase(), "complete") })
	if cerr != nil {
		t.Fatalf("complete refused over a doomed pin: %v\n%s", cerr, out)
	}
	if !strings.Contains(out, "red-proof repoint "+phaseID) {
		t.Errorf("the warning does not name the repair verb:\n%s", out)
	}
	if !strings.Contains(out, phaseID) {
		t.Errorf("the warning does not name the phase:\n%s", out)
	}
	if branches := mustGit(t, dir, "branch", "--list", "phase/*"); branches != "" {
		t.Errorf("complete did not finish: phase branch survives (%q)", branches)
	}
	if _, err := os.Stat(filepath.Join(dir, ".dross", "phases", phaseID)); err != nil {
		t.Fatalf("phase dir vanished: %v", err)
	}
}
