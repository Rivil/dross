package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/phase"
)

// absorbRepo is an initialised repo with three phases and a project store:
//
//   - source parks four items: aaa → target, bbb → other, ccc unrouted, ddd dismissed
//   - the project store parks eee → target
//   - target is the phase whose spec absorbs; other is just a real phase dir
func absorbRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	mustRunSet(t, "project.name", "test-app")
	mustRunSet(t, "runtime.mode", "native")

	phases := filepath.Join(dir, ".dross", "phases")
	mustWrite(t, filepath.Join(phases, "other", ".keep"), "")
	mustWrite(t, filepath.Join(phases, "source", "spec.toml"), `[phase]
id = "source"
title = "Source"

[[criteria]]
id = "c-1"
text = "does a thing"

[[deferred]]
id = "aaa"
text = "routed here"
target = "target"

[[deferred]]
id = "bbb"
text = "routed elsewhere"
target = "other"

[[deferred]]
id = "ccc"
text = "someday"

[[deferred]]
id = "ddd"
text = "dropped"
dismissed = true
`)
	mustWrite(t, filepath.Join(dir, ".dross", "deferred.toml"), `[[deferred]]
id = "eee"
text = "parked in the store"
target = "target"
`)
	return dir
}

// writeAbsorbingSpec gives the target phase one criterion absorbing ids.
func writeAbsorbingSpec(t *testing.T, dir string, ids ...string) string {
	t.Helper()
	quoted := make([]string, len(ids))
	for i, id := range ids {
		quoted[i] = `"` + id + `"`
	}
	path := filepath.Join(dir, ".dross", "phases", "target", "spec.toml")
	mustWrite(t, path, `[phase]
id = "target"
title = "Target"

[[criteria]]
id = "c-1"
text = "takes the parked items"
deferred = [`+strings.Join(quoted, ", ")+`]
`)
	return path
}

// validateOut runs validate and returns its stdout and error.
func validateOut(t *testing.T) (string, error) {
	t.Helper()
	var err error
	out := captureStdout(t, func() { err = runCmd(t, Validate()) })
	return out, err
}

// TestValidateAcceptsAbsorbedIDs: an id naming an item routed to the absorbing
// phase is valid whether the item lives in another phase's spec or in the
// project store.
func TestValidateAcceptsAbsorbedIDs(t *testing.T) {
	dir := absorbRepo(t)
	writeAbsorbingSpec(t, dir, "aaa", "eee")
	if out, err := validateOut(t); err != nil {
		t.Fatalf("validate: %v\n%s", err, out)
	}
}

// TestValidateRefusesBadAbsorbedIDs: each way an absorbed id can fail to name
// an item routed here is a ✗ line naming the spec, the criterion and the id.
func TestValidateRefusesBadAbsorbedIDs(t *testing.T) {
	for _, tc := range []struct {
		id, want string
	}{
		{"zzz", "names no deferred item"},
		{"bbb", "routed to other, not target"},
		{"ccc", "unrouted (someday)"},
		{"ddd", "dismissed"},
	} {
		t.Run(tc.id, func(t *testing.T) {
			dir := absorbRepo(t)
			specPath := writeAbsorbingSpec(t, dir, tc.id)
			out, err := validateOut(t)
			if err == nil {
				t.Fatalf("validate passed with absorbed id %s:\n%s", tc.id, out)
			}
			var line string
			for _, l := range strings.Split(out, "\n") {
				if strings.HasPrefix(l, "✗ ") && strings.Contains(l, "absorbs deferred "+tc.id) {
					line = l
				}
			}
			if line == "" {
				t.Fatalf("no ✗ line for absorbed id %s:\n%s", tc.id, out)
			}
			for _, want := range []string{specPath, "criterion c-1", tc.want} {
				if !strings.Contains(line, want) {
					t.Errorf("✗ line %q does not name %q", line, want)
				}
			}
		})
	}
}

// TestValidateAbsorbedHistoryIsFrozen: once the absorbing phase is complete
// its spec is history. An item it absorbed that has since been re-routed is
// accepted there, and refused while the phase is still open.
func TestValidateAbsorbedHistoryIsFrozen(t *testing.T) {
	dir := absorbRepo(t)
	writeAbsorbingSpec(t, dir, "bbb")
	changesPath := filepath.Join(dir, ".dross", "phases", "target", "changes.json")

	mustWrite(t, changesPath, `{"phase":"target","status":"shipped","tasks":{}}`)
	if out, err := validateOut(t); err == nil || !strings.Contains(out, "routed to other, not target") {
		t.Fatalf("open phase: err = %v, want the moved target named:\n%s", err, out)
	}

	mustWrite(t, changesPath, `{"phase":"target","status":"complete","tasks":{}}`)
	if out, err := validateOut(t); err != nil {
		t.Fatalf("complete phase: validate refused frozen history: %v\n%s", err, out)
	}

	// The item's later life does not reopen the record either: dismissed, or
	// gone from every source, a complete phase's absorbed id still passes.
	for _, id := range []string{"ddd", "gone"} {
		writeAbsorbingSpec(t, dir, id)
		if out, err := validateOut(t); err != nil {
			t.Errorf("complete phase absorbing %s: validate refused frozen history: %v\n%s", id, err, out)
		}
	}
}

// TestCriterionDeferredRoundTrips: LoadSpec populates Criterion.Deferred, and a
// spec that absorbs nothing saves without growing a deferred key.
func TestCriterionDeferredRoundTrips(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "spec.toml")
	mustWrite(t, path, `[phase]
id = "p"
title = "P"

[[criteria]]
id = "c-1"
text = "x"
deferred = ["abc"]
`)
	spec, err := phase.LoadSpec(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := spec.Criteria[0].Deferred; len(got) != 1 || got[0] != "abc" {
		t.Fatalf("Criterion.Deferred = %v, want [abc]", got)
	}

	plain := filepath.Join(dir, "plain.toml")
	s := &phase.Spec{Criteria: []phase.Criterion{{ID: "c-1", Text: "x"}}}
	s.Phase.ID, s.Phase.Title = "p", "P"
	if err := s.Save(plain); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(plain)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "deferred") {
		t.Errorf("a spec absorbing nothing saved a deferred key:\n%s", b)
	}
}
