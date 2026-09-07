package cmd

import (
	"crypto/sha256"
	"encoding/hex"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
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

// plan builds the repoint plan for the fixture's pin, failing the test if it
// cannot — every caller below needs a plan, not an error.
func (f repointFixture) plan(t *testing.T) redProofRepointPlan {
	t.Helper()
	p, err := planRedProofRepoint(f.root, f.repoDir, f.pin(t), nil)
	if err != nil {
		t.Fatalf("planRedProofRepoint: %v", err)
	}
	return p
}

func (f repointFixture) pin(t *testing.T) redProofPin {
	t.Helper()
	pins, err := discoverRedProofPins(f.root, f.repoDir)
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

// --- t-8: an escaping red_proof.doc never reaches a write -------------------

// escapingDocFixture rewrites the fixture's pin to name a doc OUTSIDE the repo,
// seeds a real file there, and returns its path and hash.
//
// The file is real and carries a valid `base commit:` line for a sha the record
// does not pin. That is deliberate on two counts: an unguarded repoint would
// find it and rewrite it in place (so the hash is a genuine must-trip, not a
// no-op over a missing file), and a doctor run that ever READ it would emit a
// prose-disagrees-with-record line naming that sha — so the absence of that sha
// from doctor's output proves the read never happened.
func escapingDocFixture(t *testing.T, f repointFixture) (path, hash, docSHA string) {
	t.Helper()
	docSHA = "1111111111111111111111111111111111111111"
	path = filepath.Join(filepath.Dir(f.repoDir), "victim.md")
	mustWrite(t, path, "# victim\n\n**base commit: `"+docSHA+"`**\n")
	t.Cleanup(func() { os.Remove(path) })

	rec := changes.FilePath(f.root, f.phase)
	c, err := changes.Load(rec, f.phase)
	if err != nil {
		t.Fatal(err)
	}
	c.RedProof.Doc = "../victim.md"
	if err := c.Save(rec); err != nil {
		t.Fatal(err)
	}
	return path, hashFile(t, path), docSHA
}

// assertDocEscapeRefusal checks the refusal is diagnosable: it names the doc,
// the artifact it was loaded from, and the root it escaped (c-5).
func assertDocEscapeRefusal(t *testing.T, repoDir string, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("an escaping red_proof.doc was accepted")
	}
	for _, want := range []string{"../victim.md", "changes.json", repoDir} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal does not name %q:\n%s", want, err.Error())
		}
	}
}

// TestRepointRefusesAnEscapingDocAndWritesNothing is the c-2/c-3 end-to-end.
// The refusal has to land at DISCOVERY, before any plan is built — a guard
// placed after os.WriteFile would leave the outside file already rewritten and
// still pass a test that only checked for an error.
func TestRepointRefusesAnEscapingDocAndWritesNothing(t *testing.T) {
	for _, apply := range []bool{false, true} {
		name := "dry-run"
		if apply {
			name = "apply"
		}
		t.Run(name, func(t *testing.T) {
			f := newRepointFixture(t)
			f.pushPhaseBranch(t)
			f.squashMerge(t)
			victim, before, _ := escapingDocFixture(t, f)

			args := []string{"red-proof", "repoint", f.phase}
			if apply {
				args = append(args, "--apply")
			}
			chdir(t, f.repoDir)
			assertDocEscapeRefusal(t, f.repoDir, runCmd(t, Phase(), args...))

			// A plan that PRINTED an escaping doc as "would write" is a refusal
			// the operator never gets, so the dry run must refuse too.
			if after := hashFile(t, victim); after != before {
				t.Errorf("the file outside the repo was rewritten: %s", victim)
			}
		})
	}
}

// TestEscapingDocIsNeverRead proves the refusal precedes the doc read.
// discoverRedProofPins returns an error and no pins at all, so redProofDocSHA
// is never reached — and once it takes a Contained, no test could hand it an
// escaping doc to assert that directly.
func TestEscapingDocIsNeverRead(t *testing.T) {
	f := newRepointFixture(t)
	_, _, docSHA := escapingDocFixture(t, f)

	pins, err := discoverRedProofPins(f.root, f.repoDir)
	assertDocEscapeRefusal(t, f.repoDir, err)
	if pins != nil {
		t.Errorf("a refused discovery still returned pins: %+v", pins)
	}

	// Doctor's whole red-proof section, and nothing in it may quote the sha
	// that only the victim file carries — that sha can only appear if
	// something opened the file.
	lines, _ := redProofChecks(f.root, f.repoDir)
	for _, l := range lines {
		if strings.Contains(l.text, docSHA) {
			t.Errorf("the escaping doc was READ — its sha reached doctor output: %q", l.text)
		}
	}
}

// TestRepointPlanKeepsTheDocRepoRelative pins the two places a Contained could
// start leaking one machine's layout into operator-facing output: the Files
// list a dry run prints as repo-relative, and the error messages.
func TestRepointPlanKeepsTheDocRepoRelative(t *testing.T) {
	f := newRepointFixture(t)
	f.pushPhaseBranch(t)
	f.squashMerge(t)

	plan := f.plan(t)
	if plan.Doc.Rel() != f.doc {
		t.Errorf("plan.Doc.Rel() = %q, want %q", plan.Doc.Rel(), f.doc)
	}
	if !slices.Contains(plan.Files, f.doc) {
		t.Fatalf("plan.Files = %v, want it to carry the repo-relative %q", plan.Files, f.doc)
	}
	for _, p := range plan.Files {
		if filepath.IsAbs(p) || strings.Contains(p, f.repoDir) {
			t.Errorf("plan.Files carries an absolute path %q — the list is documented as repo-relative", p)
		}
	}
}

// TestApplyRepointErrorsPrintTheRelativeDoc drives the failure arm: the record
// write fails, so the rollback path formats the doc. A Contained formatted
// without Rel() would put the absolute path in front of the operator here.
func TestApplyRepointErrorsPrintTheRelativeDoc(t *testing.T) {
	f := newRepointFixture(t)
	f.pushPhaseBranch(t)
	f.squashMerge(t)
	plan := f.plan(t)

	// Make the RECORD FILE unwritable so the doc write succeeds and the record
	// write fails — the rollback arm. The file rather than its directory: the
	// record is rewritten in place, so a read-only dir does not stop it.
	rec := changes.FilePath(f.root, f.phase)
	if err := os.Chmod(rec, 0o400); err != nil {
		t.Fatalf("cannot make the record read-only here: %v", err)
	}
	t.Cleanup(func() { os.Chmod(rec, 0o644) })

	err := applyRedProofRepoint(plan)
	if err == nil {
		if os.Geteuid() == 0 {
			t.Skip("running as root: file permissions cannot produce a failed write")
		}
		t.Fatal("the record write succeeded despite a read-only file, so the rollback arm was never reached " +
			"and this assertion measured nothing")
	}
	if !strings.Contains(err.Error(), f.doc) {
		t.Errorf("the error does not name the repo-relative doc %q:\n%s", f.doc, err)
	}
	// The record path in this message is legitimately absolute and always was;
	// what must not appear is the DOC in joined form, which is what a bare %s
	// of the Contained would print.
	if abs := filepath.Join(f.repoDir, f.doc); strings.Contains(err.Error(), abs) {
		t.Errorf("the error prints the doc as %q — the Contained was formatted without Rel():\n%s", abs, err)
	}
}

// TestApplyRepointMakesNoDirectOSCall is the structural half. The five file
// operations on the doc go through the pathfence seam; the rollback write at
// the end is the one a partial adoption is most likely to leave behind, and it
// is indistinguishable from the guarded version in any behavioural test.
func TestApplyRepointMakesNoDirectOSCall(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "redproof_repoint.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var fn *ast.FuncDecl
	for _, d := range file.Decls {
		if f, ok := d.(*ast.FuncDecl); ok && f.Name.Name == "applyRedProofRepoint" {
			fn = f
		}
	}
	if fn == nil {
		t.Fatal("applyRedProofRepoint is gone — this guard is now vacuous")
	}
	seam := 0
	ast.Inspect(fn, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		pkg, _ := sel.X.(*ast.Ident)
		if pkg == nil {
			return true
		}
		switch pkg.Name {
		case "os":
			t.Errorf("%s:%d: os.%s on the doc — every file operation here goes through the pathfence seam",
				"redproof_repoint.go", fset.Position(call.Pos()).Line, sel.Sel.Name)
		case "pathfence":
			seam++
		case "filepath":
			if sel.Sel.Name == "Join" || sel.Sel.Name == "FromSlash" {
				t.Errorf("%s:%d: filepath.%s — the doc arrives already joined; re-deriving it undoes the check",
					"redproof_repoint.go", fset.Position(call.Pos()).Line, sel.Sel.Name)
			}
		}
		return true
	})
	// Stat, ReadFile, WriteFile and the rollback WriteFile.
	if seam != 4 {
		t.Errorf("applyRedProofRepoint makes %d pathfence seam calls, want 4 (Stat, ReadFile, WriteFile, rollback WriteFile)", seam)
	}
}
