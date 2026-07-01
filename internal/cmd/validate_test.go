package cmd

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestValidateCover_MalformedSpec drives validate.go:92 down its err != nil
// branch: a phase dir with a syntactically broken spec.toml makes
// phase.LoadSpec fail, so validate must append a problem naming the spec
// path and return a non-nil error. The CONDITIONALS_NEGATION mutant that
// flips `err != nil` to `err == nil` would skip appending that problem and
// let validate pass on an otherwise-clean project, so this test kills it.
func TestValidateCover_MalformedSpec(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)

	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	// Make the project itself clean so the only problem is the bad spec.
	mustRunSet(t, "project.name", "test-app")
	mustRunSet(t, "runtime.mode", "native")

	// Broken TOML — LoadSpec's toml.DecodeFile returns an error.
	mustWrite(t, filepath.Join(dir, ".dross", "phases", "bad-phase", "spec.toml"),
		"this is not ][ valid toml")

	err := runCmd(t, Validate())
	if err == nil {
		t.Fatal("validate should error on a malformed spec.toml")
	}
	if !strings.Contains(err.Error(), "problem") {
		t.Errorf("error should mention problems: %v", err)
	}
}

// TestValidateCover_MalformedPlan drives validate.go:96 down its err != nil
// branch: a phase dir with a syntactically broken plan.toml makes
// phase.LoadPlan fail, so validate must append a problem naming the plan
// path and return a non-nil error. The CONDITIONALS_NEGATION mutant that
// flips `err != nil` to `err == nil` would skip appending that problem and
// let validate pass on an otherwise-clean project, so this test kills it.
func TestValidateCover_MalformedPlan(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)

	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	mustRunSet(t, "project.name", "test-app")
	mustRunSet(t, "runtime.mode", "native")

	// Broken TOML — LoadPlan's toml.DecodeFile returns an error.
	mustWrite(t, filepath.Join(dir, ".dross", "phases", "bad-phase", "plan.toml"),
		"this is not ][ valid toml")

	err := runCmd(t, Validate())
	if err == nil {
		t.Fatal("validate should error on a malformed plan.toml")
	}
	if !strings.Contains(err.Error(), "problem") {
		t.Errorf("error should mention problems: %v", err)
	}
}
