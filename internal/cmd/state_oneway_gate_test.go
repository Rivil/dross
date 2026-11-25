package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/state"
)

// Fixtures reuse edgeRepo (validate_edges_test.go), writeDoorSpec
// (validate_reversibility_test.go), setState / statusAt (state_edges_gate_test.go)
// and runConfirm (phase_confirm_test.go): the phase under lock is "edgy".

// twoDoors holds two locked one-way decisions, neither confirmed, and one
// reversible decision the gate must not name.
const twoDoors = `
[[decisions]]
key = "format"
choice = "TOML"
why = "hand-editable"
locked = true
reversibility = "one-way"

[[decisions]]
key = "naming"
choice = "kebab-case"
why = "readable"
locked = true
reversibility = "reversible"

[[decisions]]
key = "schema"
choice = "v1"
why = "first cut"
locked = true
reversibility = "one-way"
`

// lockEdgy points current_phase at edgy and tries plan-lock.
func lockEdgy(t *testing.T) error {
	t.Helper()
	mustSetState(t, "current_phase", "edgy")
	return setState(t, "current_phase_status", "planned")
}

func mustConfirm(t *testing.T, key string) {
	t.Helper()
	if out, err := runConfirm(t, "edgy", key); err != nil {
		t.Fatalf("dross phase confirm edgy %s: %v\n%s", key, err, out)
	}
}

// TestPlanLockRefusesUnconfirmedOneWay (c-4): two unconfirmed one-way keys
// refuse lock, naming both and not the reversible one, before state.json
// changes; the refusal routes to /dross-spec and the exact confirm form.
func TestPlanLockRefusesUnconfirmedOneWay(t *testing.T) {
	dir := edgeRepo(t)
	writeDoorSpec(t, dir, twoDoors)
	mustSetState(t, "current_phase", "edgy")
	statePath := filepath.Join(dir, ".dross", state.File)
	before, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}

	err = setState(t, "current_phase_status", "planned")
	if err == nil {
		t.Fatal("plan-lock passed with two unconfirmed one-way decisions")
	}
	msg := err.Error()
	for _, want := range []string{"format: one-way decision not confirmed", "schema: one-way decision not confirmed", "/dross-spec", "`dross phase confirm edgy <key>`"} {
		if !strings.Contains(msg, want) {
			t.Errorf("refusal lacks %q:\n%s", want, msg)
		}
	}
	if strings.Contains(msg, "naming") {
		t.Errorf("refusal names the reversible decision:\n%s", msg)
	}
	after, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Errorf("state.json changed on a refused plan-lock:\nbefore: %s\nafter:  %s", before, after)
	}
}

// TestPlanLockPassesAfterConfirm: the real verb, run in-process on both keys,
// clears the gate.
func TestPlanLockPassesAfterConfirm(t *testing.T) {
	dir := edgeRepo(t)
	writeDoorSpec(t, dir, twoDoors)
	mustSetState(t, "current_phase", "edgy")
	mustConfirm(t, "format")
	mustConfirm(t, "schema")
	if err := lockEdgy(t); err != nil {
		t.Fatalf("plan-lock refused after both confirmations: %v", err)
	}
	if got := statusAt(t, dir); got != "planned" {
		t.Errorf("status = %q, want planned", got)
	}
}

// TestPlanLockRefusesEditedChoice (c-8): editing a confirmed decision's choice
// voids its confirmation — lock refuses naming only that key as changed, and a
// fresh confirm clears it.
func TestPlanLockRefusesEditedChoice(t *testing.T) {
	dir := edgeRepo(t)
	specPath := writeDoorSpec(t, dir, twoDoors)
	mustSetState(t, "current_phase", "edgy")
	mustConfirm(t, "format")
	mustConfirm(t, "schema")

	raw, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatal(err)
	}
	edited := strings.Replace(string(raw), `"v1"`, `"v2"`, 1)
	if edited == string(raw) {
		t.Fatalf("fixture edit found no \"v1\" choice to change:\n%s", raw)
	}
	mustWrite(t, specPath, edited)

	err = lockEdgy(t)
	if err == nil {
		t.Fatal("plan-lock passed after a confirmed choice was edited")
	}
	msg := err.Error()
	if !strings.Contains(msg, "schema: one-way decision's choice changed since confirmation") {
		t.Errorf("refusal does not mark schema as changed:\n%s", msg)
	}
	if strings.Contains(msg, "format:") {
		t.Errorf("refusal names format, whose confirmation still matches:\n%s", msg)
	}

	mustConfirm(t, "schema")
	if err := lockEdgy(t); err != nil {
		t.Fatalf("plan-lock refused after re-confirming schema: %v", err)
	}
}

// TestPlanLockOneWayGateAllows (c-9): no decisions, only reversible, only
// unlocked one-way, and an unlocked decision with an unknown tag all lock —
// only locked decisions are judged (tag_scope).
func TestPlanLockOneWayGateAllows(t *testing.T) {
	for _, tc := range []struct{ name, decisions string }{
		{"no-decisions", ""},
		{"only-reversible", `
[[decisions]]
key = "naming"
choice = "kebab-case"
locked = true
reversibility = "reversible"
`},
		{"only-unlocked-one-way", `
[[decisions]]
key = "format"
choice = "TOML"
reversibility = "one-way"
`},
		{"unlocked-unknown-tag", `
[[decisions]]
key = "format"
choice = "TOML"
reversibility = "oneway"
`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := edgeRepo(t)
			writeDoorSpec(t, dir, tc.decisions)
			if err := lockEdgy(t); err != nil {
				t.Fatalf("plan-lock refused: %v", err)
			}
			if got := statusAt(t, dir); got != "planned" {
				t.Errorf("status = %q, want planned", got)
			}
		})
	}
}

// TestPlanLockRefusesUntaggedOrUnknownLocked (legacy_boundary): the phase being
// locked is never legacy — an untagged locked decision refuses, and a locked
// decision with an unknown tag refuses naming its key and its original
// decisions[i] index.
func TestPlanLockRefusesUntaggedOrUnknownLocked(t *testing.T) {
	for _, tc := range []struct{ name, decisions, want string }{
		{"untagged", `
[[decisions]]
key = "storage"
choice = "a JSON column"
why = "simple"
locked = true
`, "decisions[0] (storage): missing reversibility"},
		{"unknown", `
[[decisions]]
key = "naming"
choice = "kebab-case"
locked = true
reversibility = "reversible"

[[decisions]]
key = "format"
choice = "TOML"
locked = true
reversibility = "oneway"
`, `decisions[1] (format): unknown reversibility "oneway"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := edgeRepo(t)
			writeDoorSpec(t, dir, tc.decisions)
			err := lockEdgy(t)
			if err == nil {
				t.Fatal("plan-lock passed")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("refusal lacks %q:\n%v", tc.want, err)
			}
		})
	}
}

// TestPlanLockOneWayGateAbsenceAndDecode: called directly (the edge gate would
// refuse an undecodable spec first), no phase and no spec pass, and an
// undecodable spec refuses naming its path.
func TestPlanLockOneWayGateAbsenceAndDecode(t *testing.T) {
	dir := edgeRepo(t)
	root := filepath.Join(dir, ".dross")
	if err := planLockOneWayGate(root, ""); err != nil {
		t.Errorf("no current phase refused: %v", err)
	}
	if err := planLockOneWayGate(root, "ghost"); err != nil {
		t.Errorf("phase with no spec.toml refused: %v", err)
	}
	path := filepath.Join(root, "phases", "edgy", "spec.toml")
	mustWrite(t, path, "[phase\nid = \"edgy\"\n")
	err := planLockOneWayGate(root, "edgy")
	if err == nil {
		t.Fatal("undecodable spec passed the one-way gate")
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("refusal does not name %s: %v", path, err)
	}
}

// TestPlanLockOneWayGateScopedToPlanned: with unconfirmed one-way decisions,
// every status other than planned is set without the gate running.
func TestPlanLockOneWayGateScopedToPlanned(t *testing.T) {
	dir := edgeRepo(t)
	writeDoorSpec(t, dir, twoDoors)
	mustSetState(t, "current_phase", "edgy")
	for _, status := range []string{"specced", "in-progress", "complete", "partial"} {
		mustSetState(t, "current_phase_status", status)
		if got := statusAt(t, dir); got != status {
			t.Errorf("status = %q, want %q", got, status)
		}
	}
}
