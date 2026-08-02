package cmd

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/state"
)

// This file asserts c-3 across the whole surface at once: every dross command
// that switches branches leaves .dross/state.json holding every history entry
// it had before the switch, in order.
//
// The survival assertion applies to all four commands. The appended-own-entry
// assertion applies only where a command actually records one, which is fewer
// than the plan assumed: `ship recover` does (after its delta gate), and
// `phase complete --recover` does through the recovery routine it delegates
// to. Plain `phase complete` and `milestone complete --finalize` never load or
// save state at all — asserting an entry for either would be wrong by
// construction, not stricter.

// seedHistory writes N distinct, ordered actions into the live state.json and
// returns them. Distinct strings so a reorder is as visible as a drop.
func seedHistory(t *testing.T, dir string, n int) []string {
	t.Helper()
	stPath := filepath.Join(dir, ".dross", state.File)
	s, err := state.Load(stPath)
	if err != nil {
		t.Fatal(err)
	}
	var seeded []string
	for i := 0; i < n; i++ {
		action := "seeded entry " + string(rune('a'+i))
		s.Touch(action)
		seeded = append(seeded, action)
	}
	if err := s.Save(stPath); err != nil {
		t.Fatal(err)
	}
	return seeded
}

// assertHistorySurvives checks every seeded action is still present, in the
// original relative order. Extra entries appended after them are fine — that is
// what a command recording its own action looks like.
func assertHistorySurvives(t *testing.T, dir string, seeded []string) *state.State {
	t.Helper()
	s, err := state.Load(filepath.Join(dir, ".dross", state.File))
	if err != nil {
		t.Fatalf("state.json should still be readable: %v", err)
	}
	at := 0
	for _, want := range seeded {
		found := -1
		for i := at; i < len(s.History); i++ {
			if s.History[i].Action == want {
				found = i
				break
			}
		}
		if found < 0 {
			t.Fatalf("history lost %q (or reordered it before %q); got %d entries: %+v",
				want, seeded[0], len(s.History), actions(s))
		}
		at = found + 1
	}
	return s
}

func actions(s *state.State) []string {
	var out []string
	for _, a := range s.History {
		out = append(out, a.Action)
	}
	return out
}

func hasAction(s *state.State, substr string) bool {
	for _, a := range s.History {
		if strings.Contains(a.Action, substr) {
			return true
		}
	}
	return false
}

// TestHistorySurvivesEveryBranchSwitch is the table. Each case seeds 12
// entries, runs one branch-switching command, and asserts all 12 survive in
// order with the version untouched.
func TestHistorySurvivesEveryBranchSwitch(t *testing.T) {
	cases := []struct {
		name string
		// setup returns the repo dir, ready for run.
		setup func(t *testing.T) string
		run   func(t *testing.T, dir string) error
		// ownEntry, when non-empty, must be the LAST history entry afterwards.
		ownEntry string
	}{
		{
			// `phase complete` is the sole writer of the completion record:
			// `dross ship` only marks the phase shipped, because a phase is
			// not complete until its PR is merged. So the run appends
			// `completed <id>` last, on top of every seeded entry.
			name: "phase complete",
			setup: func(t *testing.T) string {
				dir, _ := completeFixture(t)
				stubPRMerged(t, true)
				return dir
			},
			run:      func(t *testing.T, _ string) error { return runCmd(t, Phase(), "complete") },
			ownEntry: "completed auth",
		},
		{
			// The recover path appends two: recovery's own `merged auth`, then
			// the completion record on top — so `completed auth` is last here
			// too. That `merged auth` survives the completion write (i.e. that
			// the write re-loads state rather than saving a stale copy) is
			// pinned separately by TestCompleteRecoverKeepsMergedEntry.
			name: "phase complete --recover",
			setup: func(t *testing.T) string {
				dir, _, _ := divergedCompleteFixture(t)
				stubPRMerged(t, true)
				return dir
			},
			run:      func(t *testing.T, _ string) error { return runCmd(t, Phase(), "complete", "--recover") },
			ownEntry: "completed auth",
		},
		{
			name: "ship recover",
			setup: func(t *testing.T) string {
				dir, _ := recoverFixture(t)
				return dir
			},
			run:      func(t *testing.T, _ string) error { return runCmd(t, Ship(), "recover") },
			ownEntry: "merged 01-x",
		},
		{
			// No own entry by construction: milestoneFinalize performs the
			// checkout, ff-merge and branch deletes without loading state at
			// all. Asserting one here would be wrong, not stricter.
			name: "milestone complete --finalize",
			setup: func(t *testing.T) string {
				dir, _ := milestoneFinalizeFixture(t, true)
				return dir
			},
			run: func(t *testing.T, _ string) error {
				return runCmd(t, Milestone(), "complete", "v0.9", "--finalize")
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := tc.setup(t)
			seeded := seedHistory(t, dir, 12)
			before, err := state.Load(filepath.Join(dir, ".dross", state.File))
			if err != nil {
				t.Fatal(err)
			}

			if err := tc.run(t, dir); err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}

			after := assertHistorySurvives(t, dir, seeded)
			if after.Version != before.Version {
				t.Errorf("the switch reset the version: %q -> %q", before.Version, after.Version)
			}
			if tc.ownEntry == "" {
				if len(after.History) != len(before.History) {
					t.Errorf("%s records no entry of its own, so history should be unchanged at %d, got %d: %+v",
						tc.name, len(before.History), len(after.History), actions(after))
				}
				return
			}
			last := after.History[len(after.History)-1].Action
			if !strings.Contains(last, tc.ownEntry) {
				t.Errorf("%s should append %q last, got %q (history: %+v)", tc.name, tc.ownEntry, last, actions(after))
			}
		})
	}
}

// TestShipRecoverInSyncAppendsNothing is the other half of the ship recover
// row, and it is deliberately NOT in the table: with the base already carrying
// the full .dross/ tree the delta gate returns before the Touch, so the correct
// assertion is 12 entries unchanged and no new entry. Asserting an appended
// entry here would be wrong by construction.
func TestShipRecoverInSyncAppendsNothing(t *testing.T) {
	dir, _ := recoverFixture(t)
	// Give origin/main the full tree so there is nothing to restore.
	mustGit(t, dir, "push", "-q", "--force", "origin", "main")
	mustGit(t, dir, "fetch", "-q", "origin")
	seeded := seedHistory(t, dir, 12)

	var out string
	if err := runCmdCapturing(t, &out, Ship(), "recover"); err != nil {
		t.Fatalf("recover: %v", err)
	}
	if !strings.Contains(out, "already in sync") {
		t.Fatalf("precondition: this run should hit the in-sync early return:\n%s", out)
	}

	after := assertHistorySurvives(t, dir, seeded)
	if len(after.History) != len(seeded)+1 { // +1 for init's own entry
		t.Errorf("an in-sync recovery writes nothing: %d entries, want %d: %+v",
			len(after.History), len(seeded)+1, actions(after))
	}
	if hasAction(after, "merged 01-x") {
		t.Errorf("the delta gate returns before the Touch — no merge entry should exist: %+v", actions(after))
	}
}

// TestRecoverKeepsHistoryAcrossHardReset pins the --recover case specifically:
// the heal is `git reset --hard origin/<base>` plus a tree restore, and either
// step reintroducing the clobber loses everything the machine accumulated.
func TestRecoverKeepsHistoryAcrossHardReset(t *testing.T) {
	dir, _, _ := divergedCompleteFixture(t)
	stubPRMerged(t, true)
	seeded := seedHistory(t, dir, 12)

	if err := runCmd(t, Phase(), "complete", "--recover"); err != nil {
		t.Fatalf("complete --recover: %v", err)
	}

	after := assertHistorySurvives(t, dir, seeded)
	// And the file is still out of the index after the whole operation.
	if tracked := mustGit(t, dir, "ls-files", ".dross/"+state.File); tracked != "" {
		t.Errorf("recovery re-tracked state.json: %q", tracked)
	}
	if !hasAction(after, "completed auth") {
		t.Errorf("the pre-existing completion record should survive too: %+v", actions(after))
	}
}
