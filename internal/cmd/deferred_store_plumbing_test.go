package cmd

import (
	"path/filepath"
	"strings"
	"testing"
)

// writeStore writes a project store holding a single item with the given target.
func writeStore(t *testing.T, dir, target string) string {
	t.Helper()
	body := "[phase]\n  id = \"_project\"\n  title = \"project-level deferred store\"\n\n[[deferred]]\n  text = \"store item\"\n"
	if target != "" {
		body += "  target = \"" + target + "\"\n"
	}
	path := filepath.Join(dir, ".dross", "deferred.toml")
	mustWrite(t, path, body)
	return path
}

// TestValidateRejectsDanglingStoreTarget: the store is hand-editable like any
// spec, so a bogus target written into it must be caught. Without this walk the
// store is a hole in the dangling-target guard.
func TestValidateRejectsDanglingStoreTarget(t *testing.T) {
	dir := setupTargetFixture(t)
	writeStore(t, dir, "ghost-slug")

	var out string
	if err := runCmdCapturing(t, &out, Validate()); err == nil {
		t.Fatalf("validate should reject a dangling target in the store:\n%s", out)
	}
	if !strings.Contains(out, "deferred.toml") {
		t.Errorf("validate did not name deferred.toml as the source of the problem:\n%s", out)
	}
}

// TestValidateAcceptsValidStoreTarget is the no-false-positive arm, over both
// ways a target can be valid — proving the store is judged by t-2's shared
// predicate rather than a narrower rule of its own.
func TestValidateAcceptsValidStoreTarget(t *testing.T) {
	for _, target := range []string{
		"gamma",              // phase dir, in no milestone array
		"watch-pr-ci-status", // an earlier milestone's array, no phase dir
	} {
		t.Run(target, func(t *testing.T) {
			dir := setupTargetFixture(t)
			writeStore(t, dir, target)
			var out string
			if err := runCmdCapturing(t, &out, Validate()); err != nil {
				t.Errorf("validate should accept store target %q: %v\n%s", target, err, out)
			}
		})
	}
}

// TestValidateRejectsReservedPhaseDir: a phases/_project directory makes
// `_project 0` ambiguous between two sources. `deferred list` skips it silently;
// validate is what tells the user the directory is the problem.
func TestValidateRejectsReservedPhaseDir(t *testing.T) {
	dir := setupTargetFixture(t)
	mustWrite(t, filepath.Join(dir, ".dross", "phases", projectStoreSlug, "spec.toml"),
		"[phase]\nid = \"_project\"\ntitle = \"Impostor\"\n\n[[criteria]]\nid = \"c-1\"\ntext = \"x\"\n")

	var out string
	if err := runCmdCapturing(t, &out, Validate()); err == nil {
		t.Fatalf("validate should reject a phases/_project directory:\n%s", out)
	}
	if !strings.Contains(out, "reserved") || !strings.Contains(out, projectStoreSlug) {
		t.Errorf("problem should name the reserved slug and the directory:\n%s", out)
	}
}

// TestPhaseRenameRepointsStoreTarget: `dross phase rename` must not orphan a
// store entry's target. A repoint that walks phase.List only leaves the store
// pointing at the dead slug.
func TestPhaseRenameRepointsStoreTarget(t *testing.T) {
	dir := setupLifecycleFixture(t) // current_milestone=v1, phases p1,p2,p3
	storePath := writeStore(t, dir, "p1")

	if err := runCmd(t, Phase(), "rename", "p1", "p1x"); err != nil {
		t.Fatalf("phase rename: %v", err)
	}
	body := mustRead(t, storePath)
	if !strings.Contains(body, `target = "p1x"`) {
		t.Errorf("store target not repointed to the new slug:\n%s", body)
	}
	if strings.Contains(body, `target = "p1"`+"\n") {
		t.Errorf("store still points at the dead slug:\n%s", body)
	}
}

// TestPhaseRenameLeavesUnrelatedStoreTarget: the walk repoints only matching
// targets. An entry routed elsewhere stays byte-identical.
func TestPhaseRenameLeavesUnrelatedStoreTarget(t *testing.T) {
	dir := setupLifecycleFixture(t)
	storePath := writeStore(t, dir, "p3")
	before := mustRead(t, storePath)

	if err := runCmd(t, Phase(), "rename", "p1", "p1x"); err != nil {
		t.Fatalf("phase rename: %v", err)
	}
	if got := mustRead(t, storePath); got != before {
		t.Errorf("an unrelated store target was rewritten:\n got %s\nwant %s", got, before)
	}
}
