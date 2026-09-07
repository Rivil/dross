package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/argfence"
	"github.com/Rivil/dross/internal/changes"
	"github.com/Rivil/dross/internal/pathfence"
)

// redProofSetFixture is a repo with one phase dir, one commit, and a doc to
// pin — the minimum the verb needs to say yes.
func redProofSetFixture(t *testing.T) (dir, phaseID, sha, doc string) {
	t.Helper()
	dir = initWithGit(t)
	phaseID = "config-trust-hardening"
	if err := os.MkdirAll(filepath.Join(dir, ".dross", "phases", phaseID), 0o755); err != nil {
		t.Fatal(err)
	}
	doc = "fixtures/proof/RUN.md"
	mustWrite(t, filepath.Join(dir, doc), "# proof\n\n**base commit: `deadbeef`**\n")
	sha = mustGit(t, dir, "rev-parse", "HEAD")
	return dir, phaseID, sha, doc
}

func readRedProof(t *testing.T, dir, phaseID string) *changes.RedProof {
	t.Helper()
	c, err := changes.Load(changes.FilePath(filepath.Join(dir, ".dross"), phaseID), phaseID)
	if err != nil {
		t.Fatalf("load changes: %v", err)
	}
	return c.RedProof
}

// TestRedProofSetRecordsPin is the happy path: the abbreviation the user types
// is stored expanded, because an abbreviation grows ambiguous as history does
// and the doc it is cross-checked against carries the full SHA.
func TestRedProofSetRecordsPin(t *testing.T) {
	dir, phaseID, sha, doc := redProofSetFixture(t)

	if err := runCmd(t, Phase(), "red-proof", "set", phaseID, "--sha", sha[:8], "--doc", doc); err != nil {
		t.Fatalf("red-proof set: %v", err)
	}

	pin := readRedProof(t, dir, phaseID)
	if pin == nil {
		t.Fatal("no red_proof recorded")
	}
	if pin.SHA != sha {
		t.Errorf("recorded sha = %q, want the full %q", pin.SHA, sha)
	}
	if pin.Doc != doc {
		t.Errorf("recorded doc = %q, want %q", pin.Doc, doc)
	}
}

// TestRedProofSetRejectsBadInput: a pin nothing validates is how the c-5 one
// came to name a commit no one else could reach. Every arm must refuse rather
// than write.
func TestRedProofSetRejectsBadInput(t *testing.T) {
	for _, tc := range []struct {
		name, sha, doc, phase string
	}{
		{"sha is not a commit", "0123456789abcdef0123456789abcdef01234567", "", ""},
		{"sha is not even a sha", "no-such-thing", "", ""},
		{"doc does not exist", "", "fixtures/proof/GONE.md", ""},
		{"doc is absolute", "", "/etc/hosts", ""},
		{"doc escapes the repo", "", "../elsewhere/RUN.md", ""},
		{"unknown phase", "", "", "never-existed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir, phaseID, sha, doc := redProofSetFixture(t)
			if tc.sha != "" {
				sha = tc.sha
			}
			if tc.doc != "" {
				doc = tc.doc
			}
			if tc.phase != "" {
				phaseID = tc.phase
			}

			if err := runCmd(t, Phase(), "red-proof", "set", phaseID, "--sha", sha, "--doc", doc); err == nil {
				t.Fatal("expected a refusal, got a recorded pin")
			}
			if pin := readRedProof(t, dir, "config-trust-hardening"); pin != nil {
				t.Errorf("a refused set still wrote %+v", pin)
			}
		})
	}
}

// TestRedProofSetPreservesRecord: the verb pins a proof, it does not author the
// record. Dropping the base or fork point here would break `phase complete` and
// the repoint hint the whole check exists to print.
func TestRedProofSetPreservesRecord(t *testing.T) {
	dir, phaseID, sha, doc := redProofSetFixture(t)
	root := filepath.Join(dir, ".dross")

	path := changes.FilePath(root, phaseID)
	c := changes.New(phaseID)
	c.Base = "milestone/v1.3"
	c.BaseCommit = sha
	c.PR = 76
	c.Status = changes.StatusComplete
	c.Record("t-1", []string{"a.go"}, "abc1234", "", nil)
	if err := c.Save(path); err != nil {
		t.Fatal(err)
	}
	// A second phase, to prove the write is addressed to one record.
	otherPath := changes.FilePath(root, "other-phase")
	other := changes.New("other-phase")
	other.Base = "main"
	if err := other.Save(otherPath); err != nil {
		t.Fatal(err)
	}

	if err := runCmd(t, Phase(), "red-proof", "set", phaseID, "--sha", sha, "--doc", doc); err != nil {
		t.Fatalf("red-proof set: %v", err)
	}

	got, err := changes.Load(path, phaseID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Base != "milestone/v1.3" || got.BaseCommit != sha {
		t.Errorf("base/base_commit = %q/%q, want %q/%q — the fork point is what a rotted pin gets repointed to",
			got.Base, got.BaseCommit, "milestone/v1.3", sha)
	}
	if got.PR != 76 || got.Status != changes.StatusComplete {
		t.Errorf("pr/status = %d/%q, want 76/%q", got.PR, got.Status, changes.StatusComplete)
	}
	if rec, ok := got.Tasks["t-1"]; !ok || rec.Commit != "abc1234" {
		t.Errorf("task records did not survive: %+v", got.Tasks)
	}
	if gotOther, err := changes.Load(otherPath, "other-phase"); err != nil {
		t.Fatal(err)
	} else if gotOther.RedProof != nil {
		t.Errorf("an unrelated phase gained a pin: %+v", gotOther.RedProof)
	}
}

// TestDiscoverRedProofPinsFindsRecordedPin reads THIS repo, not a fixture: the
// one live pin must be discoverable, or every check built on discovery is
// checking an empty set and passing.
func TestDiscoverRedProofPinsFindsRecordedPin(t *testing.T) {
	repo := repoRootForDocs(t)
	// The live record, copied into a throwaway root: discovery still runs over
	// this repo's REAL pin, but nothing hands a loader the live .dross where a
	// gitignored file could answer instead (hermetic_dross_read_test.go).
	root := liveRecordRoot(t, repo, "config-trust-hardening")

	pins, err := discoverRedProofPins(root)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	var found *redProofPin
	for i := range pins {
		if pins[i].Phase == "config-trust-hardening" {
			found = &pins[i]
		}
	}
	if found == nil {
		t.Fatalf("config-trust-hardening's pin is absent from %v — the live repo's red proof is unchecked", pinPhases(pins))
	}
	if !strings.HasPrefix(found.SHA, "a6ef729") {
		t.Errorf("recorded sha = %q, want the a6ef729 replay commit", found.SHA)
	}
	if found.Doc != "fixtures/hostile-config-c5/RUN.md" {
		t.Errorf("recorded doc = %q, want fixtures/hostile-config-c5/RUN.md", found.Doc)
	}
	docSHA, err := redProofDocSHA(repoRootForDocs(t), found.Doc)
	if err != nil {
		t.Fatalf("read pinned doc: %v", err)
	}
	if docSHA != found.SHA {
		t.Errorf("doc pins %q but the record pins %q — the prose and the record disagree", docSHA, found.SHA)
	}
}

// TestDocsCoverRedProofVerb: a verb the docs do not list is a verb nobody runs,
// and the pin goes back to being hand-edited. The CLI-tree assertion is the
// other half — narrating a command that does not resolve is the same failure in
// the opposite direction.
func TestDocsCoverRedProofVerb(t *testing.T) {
	root := repoRootForDocs(t)
	for _, file := range []string{"README.md", "docs/dross.1"} {
		b, err := os.ReadFile(filepath.Join(root, file))
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		for _, want := range []string{"red-proof", "repoint"} {
			if !strings.Contains(string(b), want) {
				t.Errorf("%s does not mention %q — a verb the docs do not list is a verb nobody runs, and the pin goes back to being hand-edited", file, want)
			}
		}
	}

	var found bool
	for _, sub := range Phase().Commands() {
		if sub.Name() == "red-proof" {
			found = true
			leaves := map[string]bool{}
			for _, leaf := range sub.Commands() {
				leaves[leaf.Name()] = true
			}
			for _, want := range []string{"set", "repoint"} {
				if !leaves[want] {
					t.Errorf("`phase red-proof` has no `%s` subcommand — the docs narrate a command that does not resolve", want)
				}
			}
		}
	}
	if !found {
		t.Error("`red-proof` is not registered on the phase command tree")
	}
}

// TestRedProofSetRecordsReplay: the command that replays the proof round-trips
// through the record. Without it a repoint can only hope the proof still goes
// red at the commit it proposes.
func TestRedProofSetRecordsReplay(t *testing.T) {
	dir, phaseID, sha, doc := redProofSetFixture(t)
	const line = "go test -count=1 ./internal/cmd/ -run TestHostileConfig"

	if err := runCmd(t, Phase(), "red-proof", "set", phaseID, "--sha", sha, "--doc", doc, "--replay", line); err != nil {
		t.Fatalf("red-proof set: %v", err)
	}

	pin := readRedProof(t, dir, phaseID)
	if pin == nil {
		t.Fatal("no red_proof recorded")
	}
	if pin.Replay != line {
		t.Errorf("recorded replay = %q, want %q", pin.Replay, line)
	}
}

// TestRedProofSetPreservesReplay: re-pinning without --replay keeps the
// recorded command. A silent drop would leave the pin checkable and the repair
// unverifiable, with nothing in the output to say so.
func TestRedProofSetPreservesReplay(t *testing.T) {
	dir, phaseID, sha, doc := redProofSetFixture(t)
	const line = "go test ./internal/cmd/"

	if err := runCmd(t, Phase(), "red-proof", "set", phaseID, "--sha", sha, "--doc", doc, "--replay", line); err != nil {
		t.Fatalf("first set: %v", err)
	}
	if err := runCmd(t, Phase(), "red-proof", "set", phaseID, "--sha", sha, "--doc", doc); err != nil {
		t.Fatalf("second set: %v", err)
	}

	if pin := readRedProof(t, dir, phaseID); pin == nil {
		t.Fatal("no red_proof recorded")
	} else if pin.Replay != line {
		t.Errorf("replay after a set without --replay = %q, want it preserved as %q", pin.Replay, line)
	}
}

// TestRedProofLegacyRecordLoads reads THIS repo's live record, written before
// the field existed. A required field would make every record on disk
// unloadable, which is the loudest possible way to learn a schema change was
// not additive.
func TestRedProofLegacyRecordLoads(t *testing.T) {
	// Named rather than composed from a bare .dross root: changes.json is
	// tracked, so a fresh checkout has it and this reads the same record
	// everywhere (hermetic_dross_read_test.go).
	path := filepath.Join(repoRootForDocs(t), ".dross", "phases", "config-trust-hardening", "changes.json")

	c, err := changes.Load(path, "config-trust-hardening")
	if err != nil {
		t.Fatalf("load the live legacy record: %v", err)
	}
	if c.RedProof == nil {
		t.Fatal("the live record carries no red_proof — this test is checking nothing")
	}
	if c.RedProof.Replay != "" {
		t.Errorf("a record written before --replay existed loaded with replay = %q", c.RedProof.Replay)
	}

	// Re-saving must not invent the field: `omitempty` is what keeps a load-
	// set-save of an untouched record out of the diff.
	out := filepath.Join(t.TempDir(), "changes.json")
	if err := c.Save(out); err != nil {
		t.Fatalf("re-save: %v", err)
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "\"replay\"") {
		t.Errorf("re-saving a legacy record emitted a replay key:\n%s", b)
	}
}

// TestRedProofSetRejectsEmptyReplay: a blank command reads as "recorded" to the
// verified/unverified split a repoint makes, and would be spawned as nothing —
// a repoint claiming it re-checked a proof it never ran.
func TestRedProofSetRejectsEmptyReplay(t *testing.T) {
	for _, tc := range []struct{ name, replay string }{
		{"empty", ""},
		{"whitespace", "   \t "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir, phaseID, sha, doc := redProofSetFixture(t)
			if err := runCmd(t, Phase(), "red-proof", "set", phaseID, "--sha", sha, "--doc", doc, "--replay", tc.replay); err == nil {
				t.Fatal("expected a refusal, got a recorded pin")
			}
			if pin := readRedProof(t, dir, phaseID); pin != nil {
				t.Errorf("a refused set still wrote %+v", pin)
			}
		})
	}
}

// TestRedProofSetRejectsLeadingDash: the replay line is ultimately handed to
// `sh -c`, which honours no end-of-options token, so argfence's reject side
// applies. The refusal must land before changes.json is written.
func TestRedProofSetRejectsLeadingDash(t *testing.T) {
	dir, phaseID, sha, doc := redProofSetFixture(t)
	path := changes.FilePath(filepath.Join(dir, ".dross"), phaseID)

	// Pin once so there is a file whose bytes can be asserted unchanged.
	if err := runCmd(t, Phase(), "red-proof", "set", phaseID, "--sha", sha, "--doc", doc); err != nil {
		t.Fatalf("first set: %v", err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	err = runCmd(t, Phase(), "red-proof", "set", phaseID, "--sha", sha, "--doc", doc, "--replay", "--output=/tmp/pwned go test ./...")
	if err == nil {
		t.Fatal("expected a refusal, got a recorded replay")
	}
	if !errors.Is(err, argfence.ErrLeadingDash) {
		t.Errorf("err = %v, want argfence.ErrLeadingDash", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Errorf("a refused replay still rewrote the record:\n%s", after)
	}
}

// TestDoctorHintNamesRepoint: doctor's rotted-pin line must name the repair
// verb, not the hand-typed `red-proof set --sha <fork>` line it used to print.
// That line asked the operator to copy a SHA doctor had already computed and
// then edit the replay doc themselves — which is how the c-5 pin came to be
// hand-edited in the first place.
func TestDoctorHintNamesRepoint(t *testing.T) {
	f := rottedFixture(t)

	lines, present := redProofChecks(f.root, f.repoDir)
	if !present {
		t.Fatal("doctor sees no pin at all")
	}
	var hint string
	for _, l := range lines {
		if l.level == doctorIssue && strings.Contains(l.text, "unreachable") {
			hint = l.text
		}
	}
	if hint == "" {
		t.Fatalf("doctor reports no unreachable-pin line: %+v", lines)
	}
	if !strings.Contains(hint, "red-proof repoint") {
		t.Errorf("the hint does not name the repoint verb:\n%s", hint)
	}
	for _, stale := range []string{"red-proof set", "--sha"} {
		if strings.Contains(hint, stale) {
			t.Errorf("the hint still narrates the hand-typed form (%q):\n%s", stale, hint)
		}
	}
}

// TestDoctorHintDegradesGracefully: with no fork point to resolve, the hint
// must name the phase and NO command. A copy-pasteable line carrying a blank
// SHA is worse than no line at all.
func TestDoctorHintDegradesGracefully(t *testing.T) {
	f := rottedFixture(t)
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

	hint := redProofRepointHint(f.root, f.repoDir, f.pin(t))
	if !strings.Contains(hint, f.phase) {
		t.Errorf("the degraded hint does not name the phase:\n%s", hint)
	}
	// No repair command offered: with no fork point there is nothing for
	// repoint to propose, so naming it would send the operator at a verb that
	// can only refuse. The inherited resolver error still explains what to fix.
	for _, stale := range []string{"red-proof repoint", "--apply", "--sha"} {
		if strings.Contains(hint, stale) {
			t.Errorf("the degraded hint still offers %q despite having no fork point:\n%s", stale, hint)
		}
	}
}

// TestCheckRedProofDocDelegatesContainment pins the swap done in t-4: the
// absolute and ..-escaping arms are pathfence's now, so they must carry
// pathfence's sentinels. Asserting on errors.Is rather than on wording is the
// point — it is what proves the delegation happened rather than the old inline
// test being reworded.
func TestCheckRedProofDocDelegatesContainment(t *testing.T) {
	repoDir := t.TempDir()

	for _, tc := range []struct {
		name, doc string
		want      error
	}{
		{"absolute", filepath.Join(string(filepath.Separator), "abs", "x.md"), pathfence.ErrAbsolute},
		{"escapes", filepath.Join("..", "x.md"), pathfence.ErrEscapes},
		{"deep escape", filepath.Join("..", "..", "etc", "passwd"), pathfence.ErrEscapes},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := checkRedProofDoc(repoDir, tc.doc)
			if err == nil {
				t.Fatalf("checkRedProofDoc(%q) returned %q, want a refusal", tc.doc, got)
			}
			if !errors.Is(err, tc.want) {
				t.Fatalf("checkRedProofDoc(%q) error = %v, want %v", tc.doc, err, tc.want)
			}
			// c-5: the message must be diagnosable on its own.
			for _, want := range []string{"--doc", repoDir} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("refusal does not name %q:\n%s", want, err.Error())
				}
			}
		})
	}
}

// TestCheckRedProofDocKeepsItsOwnRefusals covers what the delegation must NOT
// have swallowed. Three refusals stay this function's own, and each keeps a
// distinct message: a contained-but-absent doc, a contained directory, and the
// empty flag — which must still report the MISSING FLAG rather than a
// containment error, since it is answered before containment is asked.
func TestCheckRedProofDocKeepsItsOwnRefusals(t *testing.T) {
	repoDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repoDir, "docs", "proof"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Empty: the missing-flag message, and NOT a containment error.
	_, err := checkRedProofDoc(repoDir, "   ")
	if err == nil {
		t.Fatal("checkRedProofDoc accepted an empty --doc")
	}
	if !strings.Contains(err.Error(), "--doc is required") {
		t.Errorf("empty --doc must report the missing flag, got: %v", err)
	}
	if errors.Is(err, pathfence.ErrEscapes) || errors.Is(err, pathfence.ErrAbsolute) {
		t.Errorf("empty --doc reported a containment error; the flag check must run first: %v", err)
	}

	// Contained but absent.
	_, err = checkRedProofDoc(repoDir, "docs/proof/GONE.md")
	if err == nil {
		t.Fatal("checkRedProofDoc accepted a doc that does not exist")
	}
	if !strings.Contains(err.Error(), "does not exist under") {
		t.Errorf("absent doc lost its own message: %v", err)
	}

	// Contained, exists, but is a directory — a distinct message from the above.
	_, err = checkRedProofDoc(repoDir, "docs/proof")
	if err == nil {
		t.Fatal("checkRedProofDoc accepted a directory")
	}
	if !strings.Contains(err.Error(), "is a directory") {
		t.Errorf("directory refusal lost its own message: %v", err)
	}
}

// TestCheckRedProofDocReturnsRepoRelativeSlashForm pins the return shape. What
// changes.json stores is portable text, so a delegation that started handing
// back pathfence's joined absolute form would write a pin keyed to one
// machine's layout — the exact failure the absolute-path refusal exists to
// prevent, arriving through the back door.
func TestCheckRedProofDocReturnsRepoRelativeSlashForm(t *testing.T) {
	repoDir := t.TempDir()
	mustWrite(t, filepath.Join(repoDir, "fixtures", "proof", "RUN.md"), "# proof\n")

	got, err := checkRedProofDoc(repoDir, filepath.Join("fixtures", "proof", "RUN.md"))
	if err != nil {
		t.Fatalf("checkRedProofDoc refused a valid nested doc: %v", err)
	}
	if got != "fixtures/proof/RUN.md" {
		t.Fatalf("checkRedProofDoc = %q, want the repo-relative slash form %q", got, "fixtures/proof/RUN.md")
	}
	if filepath.IsAbs(got) || strings.HasPrefix(got, repoDir) {
		t.Fatalf("checkRedProofDoc returned a joined absolute path (%q) — the stored field is repo-relative", got)
	}
	// A "./"-prefixed spelling of the same doc normalises to the same stored text.
	same, err := checkRedProofDoc(repoDir, "./fixtures/proof/RUN.md")
	if err != nil {
		t.Fatalf("checkRedProofDoc refused a ./-prefixed doc: %v", err)
	}
	if same != got {
		t.Errorf("checkRedProofDoc(%q) = %q, want it to clean to %q", "./fixtures/proof/RUN.md", same, got)
	}
}
