package cmd

import (
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/survivor"
)

// orphanFixture is the case source-existence checking structurally cannot see:
// an acceptance whose source line is still there, whose survivor is gone.
func orphanFixture(t *testing.T) string {
	t.Helper()
	dir := lifecycleRepo(t, "orphan")
	// a.go's line 3 is still in the tree — the fixture never edits it — so the
	// two existing staleness reasons (file gone, text gone) both say "fine".
	if err := runCmd(t, Survivor(), "accept", "a.go:3", "--op", "CONDITIONALS_BOUNDARY",
		"--reason", "accepted so the orphan case has something to orphan"); err != nil {
		t.Fatalf("survivor accept: %v", err)
	}
	return dir
}

// TestStaleFindsAnOrphanedAcceptance is c-4.
//
// The acceptance's source line still exists, which is exactly why the file-gone
// and text-gone checks report nothing. Before this, such an entry could only be
// retired by hand-supplied key — the verb that exists to find stale entries
// could not find them.
func TestStaleFindsAnOrphanedAcceptance(t *testing.T) {
	dir := orphanFixture(t)

	// A run in which the accepted survivor does NOT appear.
	writeTestsWithSurvivors(t, dir, "01-orphan")

	var out string
	if err := runCmdCapturing(t, &out, Survivor(), "list", "--stale"); err != nil {
		t.Fatalf("survivor list --stale: %v", err)
	}
	if strings.Contains(out, "(no stale acceptances)") {
		t.Fatalf("the orphaned acceptance was not detected — its source line still exists, which is the case source-existence checking cannot see:\n%s", out)
	}
	if !strings.Contains(out, "a.go") {
		t.Errorf("the stale list does not name the orphaned entry:\n%s", out)
	}
}

// TestLiveAcceptanceIsNotStale: reporting a still-needed acceptance would push
// a user to delete the reason and re-inherit the survivor it was silencing —
// the backlog re-flooding through the verb meant to drain it.
func TestLiveAcceptanceIsNotStale(t *testing.T) {
	dir := orphanFixture(t)

	// The same run, but the accepted survivor IS present in it.
	key := onlyAcceptedKey(t, dir)
	writeTestsWithSurvivors(t, dir, "01-orphan", key)

	var out string
	if err := runCmdCapturing(t, &out, Survivor(), "list", "--stale"); err != nil {
		t.Fatalf("survivor list --stale: %v", err)
	}
	if !strings.Contains(out, "(no stale acceptances)") {
		t.Errorf("a live acceptance was reported stale — retiring it would re-inherit the survivor it silences:\n%s", out)
	}
}

// TestStaleWithoutASurvivorSetSaysSo: with no run on disk, an acceptance whose
// source is intact cannot be judged. Reporting a confident zero is the answer
// least distinguishable from having checked.
func TestStaleWithoutASurvivorSetSaysSo(t *testing.T) {
	orphanFixture(t) // no tests.json written

	var out string
	if err := runCmdCapturing(t, &out, Survivor(), "list", "--stale"); err != nil {
		t.Fatalf("survivor list --stale: %v", err)
	}
	if !strings.Contains(out, "no tests.json") {
		t.Errorf("staleness did not say it had no run to compare against:\n%s", out)
	}
	if !strings.Contains(out, "cannot be seen without a run") {
		t.Errorf("the note does not say what it could not check:\n%s", out)
	}
}

// TestEmptyRunIsAnAnswerNotAnAbsence: a run that reported NO survivors is a
// real answer — every acceptance in the store is orphaned. Collapsing it with
// "no run at all" would either retire the store on a fresh clone or never
// retire anything.
func TestEmptyRunIsAnAnswerNotAnAbsence(t *testing.T) {
	dir := orphanFixture(t)
	writeTestsWithSurvivors(t, dir, "01-orphan") // a run, zero survivors

	var out string
	if err := runCmdCapturing(t, &out, Survivor(), "list", "--stale"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "no tests.json") {
		t.Errorf("a real run with zero survivors was treated as no run at all:\n%s", out)
	}
	if strings.Contains(out, "(no stale acceptances)") {
		t.Errorf("a run reporting zero survivors left the acceptance un-orphaned:\n%s", out)
	}
}

// --- helpers -------------------------------------------------------------

// writeTestsWithSurvivors writes a tests.json for phaseID whose survivor list
// holds exactly the given keys.
func writeTestsWithSurvivors(t *testing.T, dir, phaseID string, keys ...string) {
	t.Helper()
	var rows []string
	for _, k := range keys {
		rows = append(rows, `{"File":"a.go","Line":3,"Op":"CONDITIONALS_BOUNDARY","Key":"`+k+`","Lifecycle":"accepted"}`)
	}
	body := `{"phase":"` + phaseID + `","languages":[{"name":"go","tool":"gremlins","files":["a.go"],` +
		`"mutation":{"Tool":"gremlins","Killed":1,"Survived":` + strconv.Itoa(len(keys)) + `,"Surviving":[` + strings.Join(rows, ",") + `]}}]}`
	mustWrite(t, filepath.Join(dir, ".dross", "phases", phaseID, "tests.json"), body)
}

// onlyAcceptedKey returns the store's single acceptance key, failing loudly if
// the fixture ever grows a second — a test that silently picked one of two
// would assert about whichever it happened to get.
func onlyAcceptedKey(t *testing.T, dir string) string {
	t.Helper()
	store, err := survivor.Load(survivor.Path(filepath.Join(dir, ".dross")))
	if err != nil {
		t.Fatal(err)
	}
	if len(store.Accepted) != 1 {
		t.Fatalf("fixture holds %d acceptances, want exactly 1", len(store.Accepted))
	}
	return store.Accepted[0].Key
}
