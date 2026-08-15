package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/changes"
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
	root := filepath.Join(repoRootForDocs(t), ".dross")

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
		if !strings.Contains(string(b), "red-proof") {
			t.Errorf("%s does not mention the `red-proof` phase subcommand", file)
		}
	}

	var found bool
	for _, sub := range Phase().Commands() {
		if sub.Name() == "red-proof" {
			found = true
			var hasSet bool
			for _, leaf := range sub.Commands() {
				if leaf.Name() == "set" {
					hasSet = true
				}
			}
			if !hasSet {
				t.Error("`phase red-proof` has no `set` subcommand — the docs narrate a command that does not resolve")
			}
		}
	}
	if !found {
		t.Error("`red-proof` is not registered on the phase command tree")
	}
}
