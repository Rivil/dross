package cmd

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/changes"
)

// repointFixture builds a repo with an `origin` the tests control, a phase
// whose base is main, and a red proof pinned to a commit that lives only on the
// phase branch — the rot this whole file exists for.
//
// It returns the dross root, the repo dir, the phase id and the pinned SHA.
type repointFixture struct {
	root, repoDir, origin, phase, pinned, doc string
}

func newRepointFixture(t *testing.T) repointFixture {
	t.Helper()
	repoDir := initWithGit(t)
	root := filepath.Join(repoDir, ".dross")
	phaseID := "proofy"

	// A bare origin the fixture can push to and delete from, so reachability
	// is judged against real refs/remotes/origin/* refs rather than a stub.
	origin := filepath.Join(t.TempDir(), "origin.git")
	mustGit(t, repoDir, "init", "--bare", "-q", origin)
	// initWithGit already configures an `origin`, so point it here rather than
	// adding a second one.
	mustGit(t, repoDir, "remote", "set-url", "origin", origin)
	mustGit(t, repoDir, "push", "-q", "origin", "HEAD:refs/heads/main")

	// The phase branch, carrying the commit the proof is pinned to.
	mustGit(t, repoDir, "checkout", "-q", "-b", "phase/"+phaseID)
	doc := "fixtures/proof/RUN.md"
	pinnedPlaceholder := "0000000000000000000000000000000000000000"
	mustWrite(t, filepath.Join(repoDir, doc), "# proof\n\n**base commit: `"+pinnedPlaceholder+"`**\n\nBASE="+pinnedPlaceholder+"\n")
	mustGit(t, repoDir, "add", ".")
	mustGit(t, repoDir, "commit", "-q", "-m", "feat: the proof")
	pinned := mustGit(t, repoDir, "rev-parse", "HEAD")
	// Rewrite the doc so it actually pins the commit, and commit that too.
	mustWrite(t, filepath.Join(repoDir, doc), "# proof\n\n**base commit: `"+pinned+"`**\n\nBASE="+pinned+"\n")
	mustGit(t, repoDir, "add", ".")
	mustGit(t, repoDir, "commit", "-q", "-m", "chore: pin the doc")

	if err := os.MkdirAll(filepath.Join(root, "phases", phaseID), 0o755); err != nil {
		t.Fatal(err)
	}
	path := changes.FilePath(root, phaseID)
	c := changes.New(phaseID)
	c.Base = "main"
	// A recorded task commit, as every real phase has: once the phase branch
	// is deleted it is the only surviving evidence of where the phase worked,
	// and so the only thing the fork point can be merge-based against.
	c.Record("t-1", []string{doc}, pinned, "", nil)
	if err := c.Save(path); err != nil {
		t.Fatal(err)
	}
	if err := runCmd(t, Phase(), "red-proof", "set", phaseID, "--sha", pinned, "--doc", doc); err != nil {
		t.Fatalf("red-proof set: %v", err)
	}
	return repointFixture{root: root, repoDir: repoDir, origin: origin, phase: phaseID, pinned: pinned, doc: doc}
}

// pushPhaseBranch publishes the phase branch, making the pin reachable.
func (f repointFixture) pushPhaseBranch(t *testing.T) {
	t.Helper()
	mustGit(t, f.repoDir, "push", "-q", "origin", "phase/"+f.phase+":refs/heads/phase/"+f.phase)
	mustGit(t, f.repoDir, "fetch", "-q", "origin")
}

// squashMerge collapses the phase branch into main the way a merged PR does:
// main gains the work as one new commit and the phase branch disappears from
// origin, so the pinned commit is reachable from nothing.
func (f repointFixture) squashMerge(t *testing.T) {
	t.Helper()
	mustGit(t, f.repoDir, "checkout", "-q", "main")
	mustGit(t, f.repoDir, "merge", "-q", "--squash", "phase/"+f.phase)
	mustGit(t, f.repoDir, "commit", "-q", "-m", "phase proofy (#1)")
	mustGit(t, f.repoDir, "push", "-q", "origin", "main:refs/heads/main")
	// Deleted on BOTH sides: a local delete alone leaves
	// refs/remotes/origin/phase/<id> holding the commit up, which is exactly
	// the illusion that let the real c-5 pin look sound on the author's
	// machine while being gone for everyone else.
	mustGit(t, f.repoDir, "push", "-q", "origin", "--delete", "phase/"+f.phase)
	mustGit(t, f.repoDir, "branch", "-q", "-D", "phase/"+f.phase)
	mustGit(t, f.repoDir, "fetch", "-q", "--prune", "origin")
}

func (f repointFixture) pin(t *testing.T) redProofPin {
	t.Helper()
	pins, err := discoverRedProofPins(f.root)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	for _, p := range pins {
		if p.Phase == f.phase {
			return p
		}
	}
	t.Fatalf("phase %s has no pin among %v", f.phase, pins)
	return redProofPin{}
}

func hashFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// TestPlanSoundPinNothingToDo: a reachable pin is left alone (c-5), and it is
// left alone with NO target — proposing one invites a rewrite that trades a
// working pin for a different working pin.
func TestPlanSoundPinNothingToDo(t *testing.T) {
	f := newRepointFixture(t)
	f.pushPhaseBranch(t)

	recordPath := changes.FilePath(f.root, f.phase)
	docPath := filepath.Join(f.repoDir, f.doc)
	beforeRecord, beforeDoc := hashFile(t, recordPath), hashFile(t, docPath)

	plan, err := planRedProofRepoint(f.root, f.repoDir, f.pin(t), nil)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if plan.Verdict != repointNothingToDo {
		t.Errorf("verdict = %q, want %q", plan.Verdict, repointNothingToDo)
	}
	if plan.NewSHA != "" {
		t.Errorf("a sound pin was given a target: %q", plan.NewSHA)
	}
	if err := applyRedProofRepoint(plan); err != nil {
		t.Fatalf("apply of a nothing-to-do plan: %v", err)
	}
	if hashFile(t, recordPath) != beforeRecord {
		t.Error("apply rewrote changes.json for a sound pin")
	}
	if hashFile(t, docPath) != beforeDoc {
		t.Error("apply rewrote the doc for a sound pin")
	}
}

// TestPlanIndeterminateIsNotRotted: "I cannot tell" must never be rewritten as
// "it is gone". A shallow CI clone legitimately cannot see the pinned commit,
// and repointing there would destroy a perfectly sound pin.
func TestPlanIndeterminateIsNotRotted(t *testing.T) {
	t.Run("shallow clone", func(t *testing.T) {
		f := newRepointFixture(t)
		f.pushPhaseBranch(t)
		// Fake the shallow marker: `git rev-parse --is-shallow-repository`
		// reports true whenever .git/shallow exists.
		mustWrite(t, filepath.Join(f.repoDir, ".git", "shallow"), f.pinned+"\n")

		plan, err := planRedProofRepoint(f.root, f.repoDir, f.pin(t), nil)
		if err != nil {
			t.Fatalf("plan: %v", err)
		}
		if plan.Verdict != repointNothingToDo {
			t.Errorf("verdict = %q, want %q", plan.Verdict, repointNothingToDo)
		}
		if !strings.Contains(plan.Why, "shallow") {
			t.Errorf("why = %q, want it to name the shallow clone", plan.Why)
		}
	})

	t.Run("no origin refs", func(t *testing.T) {
		f := newRepointFixture(t)
		// A push updates the remote-tracking ref, so drop it: the case under
		// test is a repo that has never fetched, which has nothing to judge
		// containment against.
		mustGit(t, f.repoDir, "update-ref", "-d", "refs/remotes/origin/main")

		plan, err := planRedProofRepoint(f.root, f.repoDir, f.pin(t), nil)
		if err != nil {
			t.Fatalf("plan: %v", err)
		}
		if plan.Verdict != repointNothingToDo {
			t.Errorf("verdict = %q, want %q", plan.Verdict, repointNothingToDo)
		}
		if !strings.Contains(plan.Why, originRefGlob) {
			t.Errorf("why = %q, want it to name the missing origin refs", plan.Why)
		}
	})
}

// TestPlanRefusesUnreachableTarget: c-3. Moving a pin onto a second commit
// origin cannot see swaps one broken pin for another and calls it a repair.
func TestPlanRefusesUnreachableTarget(t *testing.T) {
	f := newRepointFixture(t)
	f.pushPhaseBranch(t)
	f.squashMerge(t)

	// Pin the fork point to a commit that exists locally but was never pushed,
	// so the proposed target itself is unreachable from origin.
	mustGit(t, f.repoDir, "checkout", "-q", "-b", "scratch")
	mustWrite(t, filepath.Join(f.repoDir, "scratch.txt"), "local only\n")
	mustGit(t, f.repoDir, "add", "scratch.txt")
	mustGit(t, f.repoDir, "commit", "-q", "-m", "chore: local only")
	unpushed := mustGit(t, f.repoDir, "rev-parse", "HEAD")

	path := changes.FilePath(f.root, f.phase)
	c, err := changes.Load(path, f.phase)
	if err != nil {
		t.Fatal(err)
	}
	c.BaseCommit = unpushed
	if err := c.Save(path); err != nil {
		t.Fatal(err)
	}
	beforeRecord := hashFile(t, path)
	docPath := filepath.Join(f.repoDir, f.doc)
	beforeDoc := hashFile(t, docPath)

	_, err = planRedProofRepoint(f.root, f.repoDir, f.pin(t), nil)
	if err == nil {
		t.Fatal("planned a repoint onto a commit origin cannot see")
	}
	if !strings.Contains(err.Error(), short(unpushed)) {
		t.Errorf("refusal does not name the proposed fork point: %v", err)
	}
	if !strings.Contains(err.Error(), "fresh clone") && !strings.Contains(err.Error(), originRefGlob) {
		t.Errorf("refusal does not carry the reachability reason: %v", err)
	}
	if hashFile(t, path) != beforeRecord || hashFile(t, docPath) != beforeDoc {
		t.Error("a refused plan still wrote a file")
	}
}

// TestPlanRefusesUnresolvableForkPoint: a phase with no base and no base_commit
// must error naming the phase, never degrade to a blank proposed SHA.
func TestPlanRefusesUnresolvableForkPoint(t *testing.T) {
	f := newRepointFixture(t)
	f.pushPhaseBranch(t)
	f.squashMerge(t)

	path := changes.FilePath(f.root, f.phase)
	c, err := changes.Load(path, f.phase)
	if err != nil {
		t.Fatal(err)
	}
	c.Base = ""
	c.BaseCommit = ""
	if err := c.Save(path); err != nil {
		t.Fatal(err)
	}

	plan, err := planRedProofRepoint(f.root, f.repoDir, f.pin(t), nil)
	if err == nil {
		t.Fatalf("planned a repoint with no fork point to propose: %+v", plan)
	}
	if !strings.Contains(err.Error(), f.phase) {
		t.Errorf("error does not name the phase: %v", err)
	}
	if plan.NewSHA != "" {
		t.Errorf("a failed plan still carried a target: %q", plan.NewSHA)
	}
}

// TestPlanDoesNotCacheForkPoint: planning is a read. phaseForkPoint writes the
// resolved value back into changes.json, so the plan path must not use it — a
// dry run that modified the record it was reporting on would be a write nobody
// asked for.
func TestPlanDoesNotCacheForkPoint(t *testing.T) {
	f := newRepointFixture(t)
	f.pushPhaseBranch(t)
	f.squashMerge(t)

	path := changes.FilePath(f.root, f.phase)
	c, err := changes.Load(path, f.phase)
	if err != nil {
		t.Fatal(err)
	}
	if c.BaseCommit != "" {
		c.BaseCommit = ""
		if err := c.Save(path); err != nil {
			t.Fatal(err)
		}
	}
	before := hashFile(t, path)

	if _, err := planRedProofRepoint(f.root, f.repoDir, f.pin(t), nil); err != nil {
		t.Fatalf("plan: %v", err)
	}
	if hashFile(t, path) != before {
		t.Error("planning cached the resolved fork point into changes.json")
	}
}

// TestPlanFilesExact: a dry run that understated what an apply touches would be
// a consent the operator did not actually give.
func TestPlanFilesExact(t *testing.T) {
	f := newRepointFixture(t)
	f.pushPhaseBranch(t)
	f.squashMerge(t)

	plan, err := planRedProofRepoint(f.root, f.repoDir, f.pin(t), nil)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if plan.Verdict != repointRepair {
		t.Fatalf("verdict = %q, want %q", plan.Verdict, repointRepair)
	}
	want := []string{".dross/phases/" + f.phase + "/" + changes.File, f.doc}
	got := append([]string(nil), plan.Files...)
	sort.Strings(got)
	sort.Strings(want)
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("files = %v, want %v", got, want)
	}
}

// TestApplyRollsBack: a half-repair must not survive. Either both files move or
// neither does — a doc that pins a commit the record does not is the exact
// disagreement c-2 exists to prevent.
func TestApplyRollsBack(t *testing.T) {
	t.Run("doc unwritable", func(t *testing.T) {
		f := newRepointFixture(t)
		f.pushPhaseBranch(t)
		f.squashMerge(t)
		docPath := filepath.Join(f.repoDir, f.doc)
		recordPath := changes.FilePath(f.root, f.phase)

		plan, err := planRedProofRepoint(f.root, f.repoDir, f.pin(t), nil)
		if err != nil {
			t.Fatalf("plan: %v", err)
		}
		if err := os.Chmod(docPath, 0o444); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(docPath, 0o644) })

		if err := applyRedProofRepoint(plan); err == nil {
			t.Fatal("apply succeeded with an unwritable doc")
		}
		c, err := changes.Load(recordPath, f.phase)
		if err != nil {
			t.Fatal(err)
		}
		if c.RedProof.SHA != f.pinned {
			t.Errorf("record pins %q after a failed apply, want the OLD %q", c.RedProof.SHA, f.pinned)
		}
	})

	t.Run("record unwritable", func(t *testing.T) {
		f := newRepointFixture(t)
		f.pushPhaseBranch(t)
		f.squashMerge(t)
		docPath := filepath.Join(f.repoDir, f.doc)
		recordPath := changes.FilePath(f.root, f.phase)

		plan, err := planRedProofRepoint(f.root, f.repoDir, f.pin(t), nil)
		if err != nil {
			t.Fatalf("plan: %v", err)
		}
		beforeDoc, err := os.ReadFile(docPath)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(recordPath, 0o444); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(recordPath, 0o644) })

		err = applyRedProofRepoint(plan)
		if err == nil {
			t.Fatal("apply succeeded with an unwritable record")
		}
		if !strings.Contains(err.Error(), changes.File) || !strings.Contains(err.Error(), f.doc) {
			t.Errorf("error does not name both files: %v", err)
		}
		afterDoc, err := os.ReadFile(docPath)
		if err != nil {
			t.Fatal(err)
		}
		if string(afterDoc) != string(beforeDoc) {
			t.Errorf("the doc was not restored after the record write failed:\n%s", afterDoc)
		}
	})
}

// TestPlanExcludedRef: the ship hook's question. A pin held up only by the ref
// that is about to be deleted is already doomed, and the excluded-ref plan is
// what lets ship see that before the deletion rather than after.
func TestPlanExcludedRef(t *testing.T) {
	f := newRepointFixture(t)
	f.pushPhaseBranch(t)
	doomedRef := originRefGlob + "phase/" + f.phase

	withExclusion, err := planRedProofRepoint(f.root, f.repoDir, f.pin(t), []string{doomedRef})
	if err != nil {
		t.Fatalf("plan with exclusion: %v", err)
	}
	if withExclusion.Verdict != repointRepair {
		t.Errorf("with %s excluded, verdict = %q, want %q", doomedRef, withExclusion.Verdict, repointRepair)
	}

	without, err := planRedProofRepoint(f.root, f.repoDir, f.pin(t), nil)
	if err != nil {
		t.Fatalf("plan without exclusion: %v", err)
	}
	if without.Verdict != repointNothingToDo {
		t.Errorf("with no exclusion, verdict = %q, want %q — the pin is still reachable today", without.Verdict, repointNothingToDo)
	}
}
