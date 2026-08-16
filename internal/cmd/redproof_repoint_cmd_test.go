package cmd

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/changes"
)

// addRepointPhase adds a second (third, …) phase to an existing fixture: its
// own branch, its own replay doc, its own pin. rotted squash-merges the branch
// away afterwards, which is what makes the pin unreachable.
//
// The blanket-scan tests need several phases in one repo, which is the whole
// point of the bare `repoint` form — a scan that only ever sees one pin proves
// nothing about scoping.
func addRepointPhase(t *testing.T, f repointFixture, phaseID string, rotted bool) (doc, pinned string) {
	t.Helper()
	mustGit(t, f.repoDir, "checkout", "-q", "main")
	mustGit(t, f.repoDir, "checkout", "-q", "-b", "phase/"+phaseID)

	doc = "fixtures/proof/" + phaseID + ".md"
	placeholder := "0000000000000000000000000000000000000000"
	mustWrite(t, filepath.Join(f.repoDir, doc), "# "+phaseID+"\n\n**base commit: `"+placeholder+"`**\n")
	mustGit(t, f.repoDir, "add", ".")
	mustGit(t, f.repoDir, "commit", "-q", "-m", "feat: "+phaseID+" proof")
	pinned = mustGit(t, f.repoDir, "rev-parse", "HEAD")
	mustWrite(t, filepath.Join(f.repoDir, doc), "# "+phaseID+"\n\n**base commit: `"+pinned+"`**\n")
	mustGit(t, f.repoDir, "add", ".")
	mustGit(t, f.repoDir, "commit", "-q", "-m", "chore: pin "+phaseID)
	mustGit(t, f.repoDir, "push", "-q", "origin", "phase/"+phaseID+":refs/heads/phase/"+phaseID)
	mustGit(t, f.repoDir, "fetch", "-q", "origin")

	if err := os.MkdirAll(filepath.Join(f.root, "phases", phaseID), 0o755); err != nil {
		t.Fatal(err)
	}
	path := changes.FilePath(f.root, phaseID)
	c := changes.New(phaseID)
	c.Base = "main"
	c.Record("t-1", []string{doc}, pinned, "", nil)
	if err := c.Save(path); err != nil {
		t.Fatal(err)
	}
	if err := runCmd(t, Phase(), "red-proof", "set", phaseID, "--sha", pinned, "--doc", doc); err != nil {
		t.Fatalf("red-proof set %s: %v", phaseID, err)
	}

	if rotted {
		mustGit(t, f.repoDir, "checkout", "-q", "main")
		mustGit(t, f.repoDir, "merge", "-q", "--squash", "phase/"+phaseID)
		mustGit(t, f.repoDir, "commit", "-q", "-m", "phase "+phaseID+" (#2)")
		mustGit(t, f.repoDir, "push", "-q", "origin", "main:refs/heads/main")
		mustGit(t, f.repoDir, "push", "-q", "origin", "--delete", "phase/"+phaseID)
		mustGit(t, f.repoDir, "branch", "-q", "-D", "phase/"+phaseID)
		mustGit(t, f.repoDir, "fetch", "-q", "--prune", "origin")
	}
	return doc, pinned
}

// rottedFixture is the common setup: one phase whose pin names a commit that
// lived only on a branch the squash-merge deleted.
func rottedFixture(t *testing.T) repointFixture {
	t.Helper()
	f := newRepointFixture(t)
	f.pushPhaseBranch(t)
	f.squashMerge(t)
	return f
}

// recordReplay attaches a replay command to an existing pin.
func recordReplay(t *testing.T, f repointFixture, phaseID, line string) {
	t.Helper()
	path := changes.FilePath(f.root, phaseID)
	c, err := changes.Load(path, phaseID)
	if err != nil {
		t.Fatal(err)
	}
	if c.RedProof == nil {
		t.Fatalf("phase %s has no pin to attach a replay to", phaseID)
	}
	c.RedProof.Replay = line
	if err := c.Save(path); err != nil {
		t.Fatal(err)
	}
}

func runRepoint(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var err error
	out := captureStdout(t, func() { err = runCmd(t, Phase(), append([]string{"red-proof", "repoint"}, args...)...) })
	return out, err
}

// TestRepointDryRunWritesNothing: c-4. The dry run must state the whole
// proposal — old, new, and every file — and leave the tree exactly as it found
// it, including the fork-point cache a naive read would have written.
func TestRepointDryRunWritesNothing(t *testing.T) {
	f := rottedFixture(t)
	recordPath := changes.FilePath(f.root, f.phase)
	docPath := filepath.Join(f.repoDir, f.doc)
	beforeRecord, beforeDoc := hashFile(t, recordPath), hashFile(t, docPath)

	fork := mustGit(t, f.repoDir, "merge-base", "main", f.pinned)
	out, err := runRepoint(t)
	if err != nil {
		t.Fatalf("repoint: %v\n%s", err, out)
	}

	for _, want := range []string{f.pinned, fork, ".dross/phases/" + f.phase + "/" + changes.File, f.doc} {
		if !strings.Contains(out, want) {
			t.Errorf("dry run did not name %q:\n%s", want, out)
		}
	}
	if hashFile(t, recordPath) != beforeRecord {
		t.Error("the dry run rewrote changes.json")
	}
	if hashFile(t, docPath) != beforeDoc {
		t.Error("the dry run rewrote the doc")
	}
}

// TestRepointClearsDoctor is c-1 end to end: one command, and the thing that
// reported the rot stops reporting it. No hand-edit anywhere.
func TestRepointClearsDoctor(t *testing.T) {
	f := rottedFixture(t)

	before, present := redProofChecks(f.root, f.repoDir)
	if !present {
		t.Fatal("doctor sees no pin at all — the fixture is not exercising the check")
	}
	if !hasDoctorIssue(before, f.phase) {
		t.Fatalf("doctor reports no issue for the rotted pin: %+v", before)
	}

	if out, err := runRepoint(t, "--apply"); err != nil {
		t.Fatalf("repoint --apply: %v\n%s", err, out)
	}

	after, _ := redProofChecks(f.root, f.repoDir)
	if hasDoctorIssue(after, f.phase) {
		t.Errorf("doctor still reports an issue after the repair: %+v", after)
	}
}

func hasDoctorIssue(lines []doctorLine, phaseID string) bool {
	for _, l := range lines {
		if l.level == doctorIssue && strings.Contains(l.text, phaseID) {
			return true
		}
	}
	return false
}

// TestRepointUpdatesDoc: c-2. A repair that fixed the record and left the doc
// behind would trade an unreachable-pin issue for a prose-disagrees issue.
func TestRepointUpdatesDoc(t *testing.T) {
	f := rottedFixture(t)

	if out, err := runRepoint(t, "--apply"); err != nil {
		t.Fatalf("repoint --apply: %v\n%s", err, out)
	}

	c, err := changes.Load(changes.FilePath(f.root, f.phase), f.phase)
	if err != nil {
		t.Fatal(err)
	}
	docSHA, err := redProofDocSHA(f.repoDir, f.doc)
	if err != nil {
		t.Fatal(err)
	}
	if docSHA != c.RedProof.SHA {
		t.Errorf("doc pins %q, record pins %q — they disagree after a repair", docSHA, c.RedProof.SHA)
	}
	if c.RedProof.SHA == f.pinned {
		t.Error("the record still pins the rotted commit")
	}
}

// TestRepointRefusesUnreachable: c-3. A target origin cannot see is not a
// repair, and refusing it must cost nothing.
func TestRepointRefusesUnreachable(t *testing.T) {
	f := rottedFixture(t)

	// Point the fork point at a commit that never left this machine.
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

	out, err := runRepoint(t, "--apply")
	if err == nil {
		t.Fatalf("repoint accepted an unreachable target:\n%s", out)
	}
	if !strings.Contains(out, "refused") {
		t.Errorf("output does not say it refused:\n%s", out)
	}
	if hashFile(t, path) != beforeRecord || hashFile(t, docPath) != beforeDoc {
		t.Error("a refused repoint still wrote a file")
	}
}

// TestRepointSoundPinNoOp: c-5. A sound pin is left alone even under --apply,
// and that is a success, not a refusal.
func TestRepointSoundPinNoOp(t *testing.T) {
	f := newRepointFixture(t)
	f.pushPhaseBranch(t)
	recordPath := changes.FilePath(f.root, f.phase)
	docPath := filepath.Join(f.repoDir, f.doc)
	beforeRecord, beforeDoc := hashFile(t, recordPath), hashFile(t, docPath)

	out, err := runRepoint(t, "--apply")
	if err != nil {
		t.Fatalf("repoint over a sound pin: %v\n%s", err, out)
	}
	if !strings.Contains(out, "nothing to do") {
		t.Errorf("output does not report nothing-to-do:\n%s", out)
	}
	if hashFile(t, recordPath) != beforeRecord {
		t.Error("a sound pin's changes.json was rewritten")
	}
	if hashFile(t, docPath) != beforeDoc {
		t.Error("a sound pin's doc was rewritten")
	}
}

// TestRepointBlanketScopes: the bare form repairs what is broken and touches
// nothing else. Scoping by verdict rather than by argument is what makes the
// blanket run safe (locked repoint_target_selection).
func TestRepointBlanketScopes(t *testing.T) {
	f := rottedFixture(t)
	soundDoc, soundPinned := addRepointPhase(t, f, "sound-phase", false)
	soundRecord := changes.FilePath(f.root, "sound-phase")
	beforeSoundRecord := hashFile(t, soundRecord)
	beforeSoundDoc := hashFile(t, filepath.Join(f.repoDir, soundDoc))

	if out, err := runRepoint(t, "--apply"); err != nil {
		t.Fatalf("blanket repoint: %v\n%s", err, out)
	}

	if hashFile(t, soundRecord) != beforeSoundRecord {
		t.Error("the sound phase's changes.json was rewritten")
	}
	if hashFile(t, filepath.Join(f.repoDir, soundDoc)) != beforeSoundDoc {
		t.Error("the sound phase's doc was rewritten")
	}
	sound, err := changes.Load(soundRecord, "sound-phase")
	if err != nil {
		t.Fatal(err)
	}
	if sound.RedProof.SHA != soundPinned {
		t.Errorf("the sound pin moved: %q, want %q", sound.RedProof.SHA, soundPinned)
	}
	rotted, err := changes.Load(changes.FilePath(f.root, f.phase), f.phase)
	if err != nil {
		t.Fatal(err)
	}
	if rotted.RedProof.SHA == f.pinned {
		t.Error("the rotted pin was not repaired")
	}
}

// TestRepointBlanketContinuesPastRefusal: one bad pin must not cost the others
// their repair, and the run must still exit non-zero so the refusal is not lost.
func TestRepointBlanketContinuesPastRefusal(t *testing.T) {
	f := rottedFixture(t)
	// Three rotted pins; the middle one (by sorted phase order) is made
	// unrepairable by removing everything its fork point could resolve from.
	_, aPinned := addRepointPhase(t, f, "aaa-phase", true)
	_, mPinned := addRepointPhase(t, f, "mmm-phase", true)

	mPath := changes.FilePath(f.root, "mmm-phase")
	m, err := changes.Load(mPath, "mmm-phase")
	if err != nil {
		t.Fatal(err)
	}
	m.Base = ""
	m.BaseCommit = ""
	if err := m.Save(mPath); err != nil {
		t.Fatal(err)
	}

	out, err := runRepoint(t, "--apply")
	if err == nil {
		t.Fatalf("a run containing a refusal exited 0:\n%s", out)
	}
	if !strings.Contains(out, "mmm-phase: refused") {
		t.Errorf("the refusal was not reported:\n%s", out)
	}

	for phaseID, was := range map[string]string{"aaa-phase": aPinned, f.phase: f.pinned} {
		c, lerr := changes.Load(changes.FilePath(f.root, phaseID), phaseID)
		if lerr != nil {
			t.Fatal(lerr)
		}
		if c.RedProof.SHA == was {
			t.Errorf("%s was not repaired despite another pin refusing:\n%s", phaseID, out)
		}
	}
	stillRotted, err := changes.Load(mPath, "mmm-phase")
	if err != nil {
		t.Fatal(err)
	}
	if stillRotted.RedProof.SHA != mPinned {
		t.Error("the refused pin was rewritten anyway")
	}
}

// TestRepointRefusesGreenReplay: a proof that no longer reproduces at the
// proposed commit is not the proof being moved. Accepting it would record a
// red proof that is green.
func TestRepointRefusesGreenReplay(t *testing.T) {
	f := rottedFixture(t)
	const line = "true"
	recordReplay(t, f, f.phase, line)
	if err := GrantReplayConsent(f.root, line); err != nil {
		t.Fatal(err)
	}
	recordPath := changes.FilePath(f.root, f.phase)
	docPath := filepath.Join(f.repoDir, f.doc)
	beforeRecord, beforeDoc := hashFile(t, recordPath), hashFile(t, docPath)

	out, err := runRepoint(t, "--apply")
	if err == nil {
		t.Fatalf("a green replay was accepted as a repair:\n%s", out)
	}
	if !strings.Contains(out, "did NOT go red") {
		t.Errorf("output does not say the proof failed to go red:\n%s", out)
	}
	if hashFile(t, recordPath) != beforeRecord || hashFile(t, docPath) != beforeDoc {
		t.Error("a refused repoint still wrote a file")
	}
}

// TestRepointUnverifiedWording: with no replay recorded the repair proceeds but
// must not imply anything was checked. c-8's honesty half.
func TestRepointUnverifiedWording(t *testing.T) {
	f := rottedFixture(t)

	out, err := runRepoint(t, "--apply")
	if err != nil {
		t.Fatalf("repoint --apply: %v\n%s", err, out)
	}
	if !strings.Contains(out, "unverified") {
		t.Errorf("output does not report the repair as unverified:\n%s", out)
	}
	if strings.Contains(out, "went red") {
		t.Errorf("output claims a replay was checked when none is recorded:\n%s", out)
	}
	c, err := changes.Load(changes.FilePath(f.root, f.phase), f.phase)
	if err != nil {
		t.Fatal(err)
	}
	if c.RedProof.SHA == f.pinned {
		t.Error("the pin was not repaired")
	}
}

// TestRepointRefusesUnrunnableReplay: an error is not a red proof. Each way a
// replay can fail to RUN must refuse the repair, which is what distinguishes it
// from the merely-unconsented case below.
func TestRepointRefusesUnrunnableReplay(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(t *testing.T, f repointFixture)
	}{
		{
			name: "worktree add fails",
			setup: func(t *testing.T, f repointFixture) {
				// A FILE where git needs a directory: `worktree add` cannot
				// create its admin dir and fails before anything is spawned.
				mustWrite(t, filepath.Join(f.repoDir, ".git", "worktrees"), "not a directory\n")
			},
		},
		{
			name: "spawn error",
			setup: func(t *testing.T, f repointFixture) {
				orig := spawnLocalCtx
				spawnLocalCtx = func(context.Context, string, string, io.Writer, io.Writer) error {
					return errors.New("exec: \"sh\": executable file not found in $PATH")
				}
				t.Cleanup(func() { spawnLocalCtx = orig })
			},
		},
		{
			name: "timeout",
			setup: func(t *testing.T, f repointFixture) {
				orig := spawnLocalCtx
				spawnLocalCtx = func(context.Context, string, string, io.Writer, io.Writer) error {
					return context.DeadlineExceeded
				}
				t.Cleanup(func() { spawnLocalCtx = orig })
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := rottedFixture(t)
			const line = "exit 3"
			recordReplay(t, f, f.phase, line)
			if err := GrantReplayConsent(f.root, line); err != nil {
				t.Fatal(err)
			}
			recordPath := changes.FilePath(f.root, f.phase)
			docPath := filepath.Join(f.repoDir, f.doc)
			beforeRecord, beforeDoc := hashFile(t, recordPath), hashFile(t, docPath)
			tc.setup(t, f)

			out, err := runRepoint(t, "--apply")
			if err == nil {
				t.Fatalf("a replay that could not be run was treated as evidence:\n%s", out)
			}
			if !strings.Contains(out, "could not be run") {
				t.Errorf("output does not name the reason:\n%s", out)
			}
			if hashFile(t, recordPath) != beforeRecord || hashFile(t, docPath) != beforeDoc {
				t.Error("a refused repoint still wrote a file")
			}
		})
	}
}

// TestRepointUnconsentedReplayRepoints: absent consent is NOT a failed check.
// Refusing here would make a rotted pin unrepairable on a fresh clone, which
// would make c-1's single-command promise false exactly where it matters.
func TestRepointUnconsentedReplayRepoints(t *testing.T) {
	f := rottedFixture(t)
	const line = "exit 3"
	recordReplay(t, f, f.phase, line)
	// Deliberately NOT granted.

	spawn := &countingSpawn{}
	spawn.install(t)

	out, err := runRepoint(t, "--apply")
	if err != nil {
		t.Fatalf("an unconsented replay refused the repair: %v\n%s", err, out)
	}
	if !strings.Contains(out, "unverified") {
		t.Errorf("output does not report the repair as unverified:\n%s", out)
	}
	if !strings.Contains(out, "dross trust --replay") {
		t.Errorf("output does not name the grant command:\n%s", out)
	}
	if len(spawn.calls) != 0 {
		t.Errorf("an unconsented replay was spawned: %+v", spawn.calls)
	}
	c, err := changes.Load(changes.FilePath(f.root, f.phase), f.phase)
	if err != nil {
		t.Fatal(err)
	}
	if c.RedProof.SHA == f.pinned {
		t.Error("the pin was not repaired")
	}
}

// TestRepointDryRunNoSpawn: reporting a proposal is not consent to execute a
// command on the strength of it.
func TestRepointDryRunNoSpawn(t *testing.T) {
	f := rottedFixture(t)
	const line = "exit 3"
	recordReplay(t, f, f.phase, line)
	if err := GrantReplayConsent(f.root, line); err != nil {
		t.Fatal(err)
	}
	spawn := &countingSpawn{}
	spawn.install(t)

	out, err := runRepoint(t)
	if err != nil {
		t.Fatalf("dry run: %v\n%s", err, out)
	}
	if !strings.Contains(out, line) {
		t.Errorf("the dry run does not name the replay it would run:\n%s", out)
	}
	if len(spawn.calls) != 0 {
		t.Errorf("the dry run spawned %d command(s): %+v", len(spawn.calls), spawn.calls)
	}
}

// TestRepointRegisteredOnTheTree: doctor's hint (t-7) narrates this verb, and a
// narrated command that does not resolve is the failure that hint exists to fix.
func TestRepointRegisteredOnTheTree(t *testing.T) {
	for _, sub := range Phase().Commands() {
		if sub.Name() != "red-proof" {
			continue
		}
		for _, leaf := range sub.Commands() {
			if leaf.Name() == "repoint" {
				return
			}
		}
	}
	t.Error("`phase red-proof repoint` is not registered on the command tree")
}
