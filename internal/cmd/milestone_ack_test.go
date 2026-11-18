package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Rivil/dross/internal/milestone"
	"github.com/Rivil/dross/internal/state"
	"github.com/Rivil/dross/internal/verify"
)

// ackRepo is an initialized repo whose milestone v9.9 lists four phases:
//
//	built — scaffolded, verified, not done: a phase:built close-out item
//	done  — verified and complete: nothing to acknowledge
//	unv   — complete but never verified: only /dross-verify clears it
//	p1    — verified and complete, its verify run recording the given survivors
//
// It returns the .dross root.
func ackRepo(t *testing.T, survivors ...verify.SurvivorRecord) string {
	t.Helper()
	dir := t.TempDir()
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(dir, ".dross")
	m := &milestone.Milestone{Milestone: milestone.Meta{Version: "v9.9"}, Phases: []string{"built", "done", "unv", "p1"}}
	if err := m.Save(milestone.FilePath(root, "v9.9")); err != nil {
		t.Fatal(err)
	}
	t0 := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	ckVerify(t, root, "built", t0)
	ckVerify(t, root, "done", t0)
	ckPhase(t, root, "done", "complete")
	ckPhase(t, root, "unv", "complete")
	ckVerify(t, root, "p1", t0, survivors...)
	ckPhase(t, root, "p1", "complete")
	return root
}

func ackRecords(t *testing.T, root string) []milestone.CloseoutAck {
	t.Helper()
	m, err := milestone.Load(milestone.FilePath(root, "v9.9"))
	if err != nil {
		t.Fatal(err)
	}
	return m.Closeout
}

// The ack lands as exactly one [[closeout_ack]] carrying item, reason and a
// real timestamp.
func TestMilestoneAckWritesRecord(t *testing.T) {
	root := ackRepo(t)
	before := time.Now().UTC().Add(-time.Second)
	if err := runCmd(t, Milestone(), "ack", "phase:built", "v9.9", "--reason", "moved to v2.0"); err != nil {
		t.Fatalf("ack: %v", err)
	}
	got := ackRecords(t, root)
	if len(got) != 1 {
		t.Fatalf("records = %+v, want exactly one", got)
	}
	if got[0].Item != "phase:built" || got[0].Reason != "moved to v2.0" {
		t.Errorf("record = %+v, want item phase:built, reason %q", got[0], "moved to v2.0")
	}
	if got[0].At.IsZero() || got[0].At.Before(before) {
		t.Errorf("at = %v, want a timestamp from this run", got[0].At)
	}
	body, err := os.ReadFile(milestone.FilePath(root, "v9.9"))
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(body), "[[closeout_ack]]"); n != 1 {
		t.Errorf("toml carries %d [[closeout_ack]] tables, want 1:\n%s", n, body)
	}
}

// With no version given, the ack goes to state.current_milestone.
func TestMilestoneAckDefaultsToCurrentMilestone(t *testing.T) {
	root := ackRepo(t)
	sp := filepath.Join(root, state.File)
	s, err := state.Load(sp)
	if err != nil {
		t.Fatal(err)
	}
	s.CurrentMilestone = "v9.9"
	if err := s.Save(sp); err != nil {
		t.Fatal(err)
	}
	if err := runCmd(t, Milestone(), "ack", "phase:built", "--reason", "r"); err != nil {
		t.Fatalf("ack: %v", err)
	}
	if got := ackRecords(t, root); len(got) != 1 || got[0].Item != "phase:built" {
		t.Errorf("records = %+v, want one phase:built", got)
	}
}

// c-5: a blank reason is refused and the toml is not touched.
func TestMilestoneAckRefusesEmptyReason(t *testing.T) {
	root := ackRepo(t)
	path := milestone.FilePath(root, "v9.9")
	before := sha256OfFile(t, path)
	for _, args := range [][]string{
		{"ack", "phase:built", "v9.9", "--reason", ""},
		{"ack", "phase:built", "v9.9", "--reason", "  "},
		{"ack", "phase:built", "v9.9"},
	} {
		err := runCmd(t, Milestone(), args...)
		if err == nil {
			t.Errorf("%v: accepted", args)
			continue
		}
		if !strings.Contains(err.Error(), "--reason") {
			t.Errorf("%v: refusal does not name --reason: %v", args, err)
		}
	}
	if sha256OfFile(t, path) != before {
		t.Error("a refused ack changed the milestone toml")
	}
}

// c-9: a re-ack keeps one record and carries the latest reason — including
// after the item has resolved, so a stale ack's reason can still be corrected.
func TestMilestoneReAckReplaces(t *testing.T) {
	root := ackRepo(t)
	for _, reason := range []string{"first", "second"} {
		if err := runCmd(t, Milestone(), "ack", "phase:built", "v9.9", "--reason", reason); err != nil {
			t.Fatalf("ack %q: %v", reason, err)
		}
	}
	if got := ackRecords(t, root); len(got) != 1 || got[0].Reason != "second" {
		t.Fatalf("records = %+v, want one carrying %q", got, "second")
	}

	ckPhase(t, root, "built", "complete")
	if err := runCmd(t, Milestone(), "ack", "phase:built", "v9.9", "--reason", "third"); err != nil {
		t.Fatalf("re-ack of an acknowledged, now-resolved item refused: %v", err)
	}
	if got := ackRecords(t, root); len(got) != 1 || got[0].Reason != "third" {
		t.Errorf("records = %+v, want one carrying %q", got, "third")
	}
}

// Membership: only an item the close-out check produces now (or one already
// acknowledged) is ackable, and every refusal leaves the toml unchanged.
func TestMilestoneAckRefusesNonMembers(t *testing.T) {
	root := ackRepo(t)
	path := milestone.FilePath(root, "v9.9")
	before := sha256OfFile(t, path)
	cases := []struct {
		item string
		want []string
	}{
		{"bogus", []string{"phase:<slug>", "deferred:<source>#<index>", "survivor:<key>"}},
		{"phase:done", []string{"phase:done", "nothing to acknowledge"}},
		{"phase:nope", []string{"nope", "not in milestone v9.9"}},
		{"phase:unv", []string{"unverified", "/dross-verify unv", "--force-unverified"}},
		{"survivor:K", []string{"not an unresolved close-out item", "phase:built"}},
		{"deferred:built#0", []string{"not an unresolved close-out item"}},
	}
	for _, tc := range cases {
		err := runCmd(t, Milestone(), "ack", tc.item, "v9.9", "--reason", "r")
		if err == nil {
			t.Errorf("%s: accepted", tc.item)
			continue
		}
		for _, w := range tc.want {
			if !strings.Contains(err.Error(), w) {
				t.Errorf("%s: refusal %q does not contain %q", tc.item, err, w)
			}
		}
	}
	if sha256OfFile(t, path) != before {
		t.Error("a refused ack changed the milestone toml")
	}
}

// No milestone record: refused, and no file is created.
func TestMilestoneAckWithoutMilestone(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	path := milestone.FilePath(filepath.Join(dir, ".dross"), "v9.9")
	err := runCmd(t, Milestone(), "ack", "phase:x", "v9.9", "--reason", "r")
	if err == nil || !strings.Contains(err.Error(), "nothing to acknowledge") {
		t.Fatalf("err = %v, want a 'nothing to acknowledge' refusal", err)
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Errorf("ack created %s (stat err %v)", path, statErr)
	}
}

// The struct-based list writers re-save the whole milestone; an ack must
// survive them.
func TestMilestoneAckSurvivesListEdits(t *testing.T) {
	root := ackRepo(t)
	if err := runCmd(t, Milestone(), "ack", "phase:built", "v9.9", "--reason", "moved to v2.0"); err != nil {
		t.Fatalf("ack: %v", err)
	}
	if err := runCmd(t, Milestone(), "add", "v9.9", "phases", "p-z"); err != nil {
		t.Fatalf("add: %v", err)
	}
	if got := ackRecords(t, root); len(got) != 1 || got[0].Item != "phase:built" || got[0].Reason != "moved to v2.0" {
		t.Errorf("records after add = %+v, want the phase:built ack intact", got)
	}
}

// A keyless survivor has no key to accept or route by, so an ack is its only
// way out — its file:line:op id must be ackable.
func TestMilestoneAckKeylessSurvivor(t *testing.T) {
	root := ackRepo(t, verify.SurvivorRecord{File: "f.go", Line: 12, Op: "op"})
	if err := runCmd(t, Milestone(), "ack", "survivor:f.go:12:op", "v9.9", "--reason", "r"); err != nil {
		t.Fatalf("ack keyless survivor: %v", err)
	}
	if got := ackRecords(t, root); len(got) != 1 || got[0].Item != "survivor:f.go:12:op" {
		t.Errorf("records = %+v, want one survivor:f.go:12:op", got)
	}
}
