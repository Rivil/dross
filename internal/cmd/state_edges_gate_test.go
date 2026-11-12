package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/state"
)

// badEdge covers a criterion edgy's spec does not have.
const badEdge = `
[[edges]]
class = "empty"
outcome = "covered"
criteria = ["c-9"]
`

// goodEdges resolves one class per outcome, the deferred one against an id
// parked in another phase (fff, from edgeRepo).
const goodEdges = `
[[edges]]
class = "boundary"
outcome = "covered"
criteria = ["c-1"]

[[edges]]
class = "empty"
outcome = "dismissed"
reason = "no collection input"

[[edges]]
class = "ordering"
outcome = "deferred"
deferred = "fff"
`

func setState(t *testing.T, field, value string) error {
	t.Helper()
	return runCmd(t, State(), "set", field, value)
}

func mustSetState(t *testing.T, field, value string) {
	t.Helper()
	if err := setState(t, field, value); err != nil {
		t.Fatalf("set %s %s: %v", field, value, err)
	}
}

func statusAt(t *testing.T, dir string) string {
	t.Helper()
	s, err := state.Load(filepath.Join(dir, ".dross", state.File))
	if err != nil {
		t.Fatal(err)
	}
	return s.CurrentPhaseStatus
}

// TestPlanLockRefusesUnresolvedEdge (c-7): the refusal names the record and
// its class, and lands before anything mutates — state.json is byte-identical.
func TestPlanLockRefusesUnresolvedEdge(t *testing.T) {
	dir := edgeRepo(t)
	writeEdgeSpec(t, dir, badEdge)
	mustSetState(t, "current_phase", "edgy")
	statePath := filepath.Join(dir, ".dross", state.File)
	before, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}

	err = setState(t, "current_phase_status", "planned")
	if err == nil {
		t.Fatal("plan-lock passed with an unresolved edge")
	}
	if !strings.Contains(err.Error(), "edges[0] (empty)") {
		t.Errorf("refusal does not name edges[0] (empty): %v", err)
	}
	after, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Errorf("state.json changed on a refused plan-lock:\nbefore: %s\nafter:  %s", before, after)
	}
}

// TestPlanLockSharesValidateRules: the gate's problem lines are validate's
// lines for the same spec, minus the ✗ and path prefix — one rule set.
func TestPlanLockSharesValidateRules(t *testing.T) {
	dir := edgeRepo(t)
	specPath := writeEdgeSpec(t, dir, badEdge+`
[[edges]]
class = "latency"
outcome = "dismissed"
`)
	mustSetState(t, "current_phase", "edgy")

	err := setState(t, "current_phase_status", "planned")
	if err == nil {
		t.Fatal("plan-lock passed with unresolved edges")
	}
	var gate []string
	for _, l := range strings.Split(err.Error(), "\n") {
		if l = strings.TrimSpace(l); strings.HasPrefix(l, "edges[") {
			gate = append(gate, l)
		}
	}

	out, _ := validateOut(t)
	prefix := "✗ " + specPath + ": "
	var val []string
	for _, l := range failLines(out) {
		if strings.HasPrefix(l, prefix+"edges[") {
			val = append(val, strings.TrimPrefix(l, prefix))
		}
	}
	if len(val) != 2 || strings.Join(gate, "\n") != strings.Join(val, "\n") {
		t.Errorf("gate lines differ from validate's:\ngate:     %q\nvalidate: %q", gate, val)
	}
}

// TestPlanLockAllowsResolvedAndLegacy: the gate does not over-block — a fully
// resolved spec (including a deferred edge whose id lives only in another
// phase) and a legacy spec with no [[edges]] both lock.
func TestPlanLockAllowsResolvedAndLegacy(t *testing.T) {
	for name, edges := range map[string]string{"resolved": goodEdges, "legacy": ""} {
		t.Run(name, func(t *testing.T) {
			dir := edgeRepo(t)
			writeEdgeSpec(t, dir, edges)
			mustSetState(t, "current_phase", "edgy")
			mustSetState(t, "current_phase_status", "planned")
			if got := statusAt(t, dir); got != "planned" {
				t.Errorf("status = %q, want planned", got)
			}
		})
	}
}

// TestPlanLockGateIsScopedToPlanned: with the same bad spec, other statuses
// and other fields are untouched by the gate.
func TestPlanLockGateIsScopedToPlanned(t *testing.T) {
	dir := edgeRepo(t)
	writeEdgeSpec(t, dir, badEdge)
	mustSetState(t, "current_phase", "edgy")
	mustSetState(t, "current_phase_status", "in-progress")
	mustSetState(t, "current_phase", "other")
}

// TestPlanLockAbsenceAllows: no current phase, or a current phase with no
// spec.toml, has nothing to judge.
func TestPlanLockAbsenceAllows(t *testing.T) {
	t.Run("no-current-phase", func(t *testing.T) {
		dir := edgeRepo(t)
		writeEdgeSpec(t, dir, badEdge)
		mustSetState(t, "current_phase", "")
		mustSetState(t, "current_phase_status", "planned")
		if got := statusAt(t, dir); got != "planned" {
			t.Errorf("status = %q, want planned", got)
		}
	})
	t.Run("no-spec", func(t *testing.T) {
		dir := edgeRepo(t)
		writeEdgeSpec(t, dir, badEdge)
		mustSetState(t, "current_phase", "ghost")
		mustSetState(t, "current_phase_status", "planned")
		if got := statusAt(t, dir); got != "planned" {
			t.Errorf("status = %q, want planned", got)
		}
	})
}

// TestPlanLockRefusesMalformedSpec: a spec that does not decode is not waved
// through; the refusal names its path.
func TestPlanLockRefusesMalformedSpec(t *testing.T) {
	dir := edgeRepo(t)
	path := filepath.Join(dir, ".dross", "phases", "edgy", "spec.toml")
	mustWrite(t, path, "[phase\nid = \"edgy\"\n")
	mustSetState(t, "current_phase", "edgy")
	err := setState(t, "current_phase_status", "planned")
	if err == nil {
		t.Fatal("plan-lock passed a malformed spec")
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("refusal does not name %s: %v", path, err)
	}
}

// TestPlanLockReadsCurrentPhaseOnly: an unresolved edge in another phase does
// not block locking the current one.
func TestPlanLockReadsCurrentPhaseOnly(t *testing.T) {
	dir := edgeRepo(t)
	writeEdgeSpec(t, dir, badEdge) // edgy is phase B, unresolved
	mustWrite(t, filepath.Join(dir, ".dross", "phases", "clean", "spec.toml"), `[phase]
id = "clean"
title = "Clean"

[[criteria]]
id = "c-1"
text = "first"

[[edges]]
class = "boundary"
outcome = "covered"
criteria = ["c-1"]
`)
	mustSetState(t, "current_phase", "clean")
	mustSetState(t, "current_phase_status", "planned")
	if got := statusAt(t, dir); got != "planned" {
		t.Errorf("status = %q, want planned", got)
	}
}

// TestPlanLockUnrelatedBrokenSpec: a broken spec elsewhere does not block a
// phase with no deferred edge, but does refuse one whose deferred edge can no
// longer be judged.
func TestPlanLockUnrelatedBrokenSpec(t *testing.T) {
	for _, tc := range []struct {
		name, edges string
		refuse      bool
	}{
		{"no-deferred-edge", `
[[edges]]
class = "boundary"
outcome = "covered"
criteria = ["c-1"]
`, false},
		{"deferred-edge", goodEdges, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := edgeRepo(t)
			writeEdgeSpec(t, dir, tc.edges)
			mustWrite(t, filepath.Join(dir, ".dross", "phases", "broken", "spec.toml"), "[phase\n")
			mustSetState(t, "current_phase", "edgy")
			err := setState(t, "current_phase_status", "planned")
			if tc.refuse && err == nil {
				t.Fatal("plan-lock judged a deferred edge against ids that failed to load")
			}
			if !tc.refuse && err != nil {
				t.Fatalf("unrelated broken spec blocked plan-lock: %v", err)
			}
		})
	}
}
