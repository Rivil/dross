package cmd

import (
	"os"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/milestone"
)

// listEditRepo is an initialized repo with milestone v1.3 carrying phases
// [a, b, c] — enough shape for order to be observable, which is the property
// most of these tests are about.
func listEditRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	if err := runCmd(t, Milestone(), "create", "v1.3"); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"a", "b", "c"} {
		if err := runCmd(t, Milestone(), "add", "v1.3", "phases", p); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// listedPhases reads the phases list back off disk, which is the only place
// the edit counts.
func listedPhases(t *testing.T, dir, version string) []string {
	t.Helper()
	m, err := milestone.Load(milestone.FilePath(dir+"/.dross", version))
	if err != nil {
		t.Fatal(err)
	}
	return m.Phases
}

func milestoneBytes(t *testing.T, dir, version string) string {
	t.Helper()
	b, err := os.ReadFile(milestone.FilePath(dir+"/.dross", version))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func eqStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestMilestoneRemovePreservesOrder: `phases` is a delivery sequence, so the
// delete has to close the gap rather than fill it. A swap-with-last leaves
// [a, c, b]-shaped damage that nothing else in the toolchain would notice.
func TestMilestoneRemovePreservesOrder(t *testing.T) {
	dir := listEditRepo(t)

	if err := runCmd(t, Milestone(), "remove", "v1.3", "phases", "b"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if got, want := listedPhases(t, dir, "v1.3"), []string{"a", "c"}; !eqStrings(got, want) {
		t.Errorf("phases = %v, want %v", got, want)
	}
}

// TestMilestoneRemoveMissingValueErrors is locked remove_addressing. A remove
// that quietly succeeds against nothing is worse than one that fails: the user
// believes the correction landed and moves on with the wrong roadmap.
func TestMilestoneRemoveMissingValueErrors(t *testing.T) {
	dir := listEditRepo(t)

	if err := runCmd(t, Milestone(), "remove", "v1.3", "phases", "b"); err != nil {
		t.Fatalf("first remove: %v", err)
	}
	before := milestoneBytes(t, dir, "v1.3")

	err := runCmd(t, Milestone(), "remove", "v1.3", "phases", "b")
	if err == nil {
		t.Fatal("removing a value that is not there must error, not no-op")
	}
	if !strings.Contains(err.Error(), `"b"`) {
		t.Errorf("the error should quote the missing value, got: %v", err)
	}
	if after := milestoneBytes(t, dir, "v1.3"); after != before {
		t.Errorf("a failed remove rewrote the toml:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

// TestMilestoneReplaceKeepsPosition: replace is not remove-then-add. Fixing a
// phase's name must not move it to the end of the delivery order.
func TestMilestoneReplaceKeepsPosition(t *testing.T) {
	dir := listEditRepo(t)

	if err := runCmd(t, Milestone(), "replace", "v1.3", "phases", "b", "x"); err != nil {
		t.Fatalf("replace: %v", err)
	}
	if got, want := listedPhases(t, dir, "v1.3"), []string{"a", "x", "c"}; !eqStrings(got, want) {
		t.Errorf("phases = %v, want %v — replace must keep index 1", got, want)
	}
}

// TestMilestoneReplaceMissingValueErrors: same contract as remove. Nothing to
// replace is a failed correction, and the toml stays untouched.
func TestMilestoneReplaceMissingValueErrors(t *testing.T) {
	dir := listEditRepo(t)
	before := milestoneBytes(t, dir, "v1.3")

	err := runCmd(t, Milestone(), "replace", "v1.3", "phases", "nope", "x")
	if err == nil {
		t.Fatal("replacing a value that is not there must error")
	}
	if !strings.Contains(err.Error(), `"nope"`) {
		t.Errorf("the error should quote the missing value, got: %v", err)
	}
	if after := milestoneBytes(t, dir, "v1.3"); after != before {
		t.Errorf("a failed replace rewrote the toml")
	}
}

// TestMilestoneRemoveRejectsScalarPath: `milestone.title` is settable, not a
// list. The refusal has to name the three fields that ARE lists, or the user's
// next guess is another wrong one.
func TestMilestoneRemoveRejectsScalarPath(t *testing.T) {
	dir := listEditRepo(t)
	before := milestoneBytes(t, dir, "v1.3")

	err := runCmd(t, Milestone(), "remove", "v1.3", "milestone.title", "whatever")
	if err == nil {
		t.Fatal("a scalar path must be refused")
	}
	for _, want := range []string{"scope.success_criteria", "scope.non_goals", "phases"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not name %q: %v", want, err)
		}
	}
	if after := milestoneBytes(t, dir, "v1.3"); after != before {
		t.Errorf("a refused remove rewrote the toml")
	}
}

// TestMilestoneRemoveAcceptsFieldAliases: remove resolves bare and
// scope-prefixed names exactly as add does. An inverse that only accepts the
// long spelling is an inverse people get wrong.
func TestMilestoneRemoveAcceptsFieldAliases(t *testing.T) {
	dir := listEditRepo(t)

	for _, alias := range []string{"success_criteria", "scope.success_criteria"} {
		if err := runCmd(t, Milestone(), "add", "v1.3", "scope.success_criteria", "crit"); err != nil {
			t.Fatal(err)
		}
		if err := runCmd(t, Milestone(), "remove", "v1.3", alias, "crit"); err != nil {
			t.Fatalf("remove via alias %q: %v", alias, err)
		}
		m, err := milestone.Load(milestone.FilePath(dir+"/.dross", "v1.3"))
		if err != nil {
			t.Fatal(err)
		}
		if len(m.Scope.SuccessCriteria) != 0 {
			t.Errorf("alias %q did not hit scope.success_criteria, left %v", alias, m.Scope.SuccessCriteria)
		}
	}
}

// TestMilestoneRemoveDefaultsToCurrentMilestone: add takes the version from
// state when it is omitted, and the inverse has to agree — otherwise the two
// commands disagree about which milestone the bare form means.
func TestMilestoneRemoveDefaultsToCurrentMilestone(t *testing.T) {
	dir := listEditRepo(t)
	if err := runCmd(t, State(), "set", "current_milestone", "v1.3"); err != nil {
		t.Fatal(err)
	}

	if err := runCmd(t, Milestone(), "remove", "phases", "b"); err != nil {
		t.Fatalf("bare remove: %v", err)
	}
	if got, want := listedPhases(t, dir, "v1.3"), []string{"a", "c"}; !eqStrings(got, want) {
		t.Errorf("phases = %v, want %v", got, want)
	}
}

// TestReadmeMilestoneRowDocumentsListEdits: the verbs are only reachable if the
// row that enumerates the subcommands names them. Same grep-guard shape as
// TestReadmeMilestoneRowDocumentsBaseFlag.
func TestReadmeMilestoneRowDocumentsListEdits(t *testing.T) {
	text := docText(t, "README.md")
	for _, want := range []string{"add,remove,replace", "remove and replace address an entry by its exact value"} {
		if !strings.Contains(text, want) {
			t.Errorf("the README milestone row must document the list-edit verbs (missing %q)", want)
		}
	}
}
