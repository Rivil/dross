package cmd

import (
	"path/filepath"
	"strings"
	"testing"
)

// writeDoorSpec writes the "edgy" phase's spec with one criterion and the
// given raw [[decisions]] TOML, returning its path.
func writeDoorSpec(t *testing.T, dir, decisions string) string {
	t.Helper()
	path := filepath.Join(dir, ".dross", "phases", "edgy", "spec.toml")
	mustWrite(t, path, `[phase]
id = "edgy"
title = "Edgy"

[[criteria]]
id = "c-1"
text = "first"
`+decisions)
	return path
}

// markPhase writes edgy's changes.json with the given status.
func markPhase(t *testing.T, dir, status string) {
	t.Helper()
	mustWrite(t, filepath.Join(dir, ".dross", "phases", "edgy", "changes.json"),
		`{"phase":"edgy","status":"`+status+`","tasks":{}}`)
}

// reversibilityFailLines returns the ✗ lines naming specPath that carry a
// reversibility problem.
func reversibilityFailLines(out, specPath string) []string {
	var lines []string
	for _, l := range failLines(out) {
		if strings.HasPrefix(l, "✗ "+specPath+": decisions[") && strings.Contains(l, "reversibility") {
			lines = append(lines, l)
		}
	}
	return lines
}

const untaggedLocked = `
[[decisions]]
key = "storage"
choice = "a JSON column"
why = "simple"
locked = true
`

// TestValidateReportsMissingReversibilityOnOpenPhase (c-1): an open phase's
// untagged locked decision is a ✗ line naming the spec, the index and the key
// — exactly once. "Open" covers both no changes.json and a record that is not
// complete, so swapping the completeness predicate fails a subtest.
func TestValidateReportsMissingReversibilityOnOpenPhase(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status string // "" = no changes.json at all
	}{
		{"no-changes-json", ""},
		{"in-progress-record", "in_progress"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := edgeRepo(t)
			specPath := writeDoorSpec(t, dir, untaggedLocked)
			if tc.status != "" {
				markPhase(t, dir, tc.status)
			}
			out, err := validateOut(t)
			if err == nil {
				t.Fatalf("validate passed an untagged locked decision on an open phase:\n%s", out)
			}
			want := "✗ " + specPath + ": decisions[0] (storage): missing reversibility"
			lines := reversibilityFailLines(out, specPath)
			if len(lines) != 1 {
				t.Fatalf("got %d reversibility ✗ lines, want exactly 1:\n%s", len(lines), out)
			}
			if !strings.HasPrefix(lines[0], want) {
				t.Errorf("line %q does not start %q", lines[0], want)
			}
		})
	}
}

// TestValidateCompletePhaseLegacyDecisionsPass (c-7): the same untagged spec
// under a complete record is legacy history and passes.
func TestValidateCompletePhaseLegacyDecisionsPass(t *testing.T) {
	dir := edgeRepo(t)
	writeDoorSpec(t, dir, untaggedLocked)
	markPhase(t, dir, "complete")
	out, err := validateOut(t)
	if err != nil {
		t.Fatalf("complete phase with legacy decisions refused: %v\n%s", err, out)
	}
	if !strings.Contains(out, "✓ all dross artefacts valid") {
		t.Errorf("no ✓ line:\n%s", out)
	}
}

// TestValidateUnknownReversibilityReportedEverywhere: an unknown value is a
// ✗ line even on a complete phase, quoting the value.
func TestValidateUnknownReversibilityReportedEverywhere(t *testing.T) {
	for _, status := range []string{"", "complete"} {
		t.Run("status="+status, func(t *testing.T) {
			dir := edgeRepo(t)
			specPath := writeDoorSpec(t, dir, `
[[decisions]]
key = "storage"
choice = "a JSON column"
locked = true
reversibility = "oneway"
`)
			if status != "" {
				markPhase(t, dir, status)
			}
			out, err := validateOut(t)
			if err == nil {
				t.Fatalf("validate passed an unknown reversibility value:\n%s", out)
			}
			lines := reversibilityFailLines(out, specPath)
			if len(lines) != 1 {
				t.Fatalf("got %d reversibility ✗ lines, want 1:\n%s", len(lines), out)
			}
			if !strings.HasPrefix(lines[0], "✗ "+specPath+": decisions[0] (storage)") || !strings.Contains(lines[0], `"oneway"`) {
				t.Errorf("line %q does not name decisions[0] (storage) and quote \"oneway\"", lines[0])
			}
		})
	}
}

// TestValidateUnconfirmedOneWayIsNotAValidateProblem: a tagged one-way locked
// decision with no confirmation is plan-lock's business, not validate's.
func TestValidateUnconfirmedOneWayIsNotAValidateProblem(t *testing.T) {
	dir := edgeRepo(t)
	writeDoorSpec(t, dir, `
[[decisions]]
key = "storage"
choice = "a JSON column"
locked = true
reversibility = "one-way"

[[decisions]]
key = "naming"
choice = "kebab-case"
locked = true
reversibility = "reversible"
`)
	out, err := validateOut(t)
	if err != nil {
		t.Fatalf("validate refused a tagged, unconfirmed one-way decision: %v\n%s", err, out)
	}
	if !strings.Contains(out, "✓ all dross artefacts valid") {
		t.Errorf("no ✓ line:\n%s", out)
	}
}
