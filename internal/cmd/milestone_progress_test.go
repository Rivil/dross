package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/changes"
	"github.com/Rivil/dross/internal/milestone"
	"github.com/Rivil/dross/internal/state"
)

// progressRepo is an initialized repo with a milestone whose phases array is
// exactly `phases`, and nothing scaffolded yet.
func progressRepo(t *testing.T, version string, status string, phases ...string) string {
	t.Helper()
	dir := t.TempDir()
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	m := &milestone.Milestone{Phases: phases}
	m.Milestone.Version = version
	m.Milestone.Status = status
	if err := m.Save(milestone.FilePath(filepath.Join(dir, ".dross"), version)); err != nil {
		t.Fatal(err)
	}
	return dir
}

// scaffoldPhase creates .dross/phases/<slug>/, optionally with a changes.json
// carrying `status`. An empty status writes a record with no status key at all —
// the pre-field shape.
func scaffoldPhase(t *testing.T, dir, slug, status string) {
	t.Helper()
	root := filepath.Join(dir, ".dross")
	if err := os.MkdirAll(filepath.Join(root, "phases", slug), 0o755); err != nil {
		t.Fatal(err)
	}
	if status == "" {
		return
	}
	if err := changes.SetStatus(root, slug, status); err != nil {
		t.Fatal(err)
	}
}

func progressJSON(t *testing.T, args ...string) milestoneProgressReport {
	t.Helper()
	var out string
	if err := runCmdCapturing(t, &out, Milestone(), append([]string{"progress"}, args...)...); err != nil {
		t.Fatalf("progress %v: %v", args, err)
	}
	var rep milestoneProgressReport
	if err := json.Unmarshal([]byte(out), &rep); err != nil {
		t.Fatalf("parse progress json %q: %v", out, err)
	}
	return rep
}

// TestProgressCountsDurableMarkerWithoutBreadcrumb is the case history-only
// doneness gets wrong today: mutation-diff-scope was finished (verdict pass, PR
// 79 merged) and its breadcrumb had already scrolled out of the 50-entry
// history, so nothing counting from history could see it.
func TestProgressCountsDurableMarkerWithoutBreadcrumb(t *testing.T) {
	dir := progressRepo(t, "v1.3", "active", "a", "b")
	scaffoldPhase(t, dir, "a", changes.StatusComplete)
	scaffoldPhase(t, dir, "b", "")

	// Precondition: no breadcrumb anywhere for "a" — the marker is the only
	// evidence in this fixture.
	touchHistory(t, dir, "scoped v1.3")

	rep := progressJSON(t, "v1.3", "--json")
	if rep.Done != 1 || rep.Total != 2 {
		t.Errorf("done/total = %d/%d, want 1/2", rep.Done, rep.Total)
	}
	if len(rep.Remaining) != 1 || rep.Remaining[0] != "b" {
		t.Errorf("remaining = %v, want [b]", rep.Remaining)
	}
	if rep.AllDone {
		t.Error("all_done should be false with one phase outstanding")
	}
}

// TestProgressShippedCountsDoneAndVerifiedDoesNot: shipped is delivery
// evidence; a verify verdict is not. A phase can pass verification and never
// open a PR, and counting that as done would close a milestone over work that
// never landed.
func TestProgressShippedCountsDoneAndVerifiedDoesNot(t *testing.T) {
	dir := progressRepo(t, "v1.3", "active", "shipped-one", "verified-only")
	scaffoldPhase(t, dir, "shipped-one", changes.StatusShipped)
	scaffoldPhase(t, dir, "verified-only", "")
	// verify.toml says pass, and nothing else does.
	mustWrite(t, filepath.Join(dir, ".dross", "phases", "verified-only", "verify.toml"),
		"[summary]\n  verdict = \"pass\"\n")

	rep := progressJSON(t, "v1.3", "--json")
	if rep.Done != 1 {
		t.Errorf("done = %d, want 1 — shipped counts, a verify verdict does not", rep.Done)
	}
	if len(rep.Remaining) != 1 || rep.Remaining[0] != "verified-only" {
		t.Errorf("remaining = %v, want [verified-only]", rep.Remaining)
	}
}

// TestProgressUnscaffoldedSlugIsNotDone is locked phases_done_test: a slug on
// the roadmap with no directory is work that was listed and never built, and no
// breadcrumb closes it. An implementation counting array entries, or one
// skipping missing directories, both pass a milestone with unbuilt criteria.
func TestProgressUnscaffoldedSlugIsNotDone(t *testing.T) {
	dir := progressRepo(t, "v1.3", "active", "built", "never-built")
	scaffoldPhase(t, dir, "built", changes.StatusComplete)
	// History insists the unbuilt phase is done. The directory disagrees.
	touchHistory(t, dir, "completed never-built")

	rep := progressJSON(t, "v1.3", "--json")
	if rep.Done != 1 {
		t.Errorf("done = %d, want 1 — an unscaffolded slug is never done", rep.Done)
	}
	if len(rep.Unscaffolded) != 1 || rep.Unscaffolded[0] != "never-built" {
		t.Errorf("unscaffolded = %v, want [never-built]", rep.Unscaffolded)
	}
	if !hasSlug(rep.Remaining, "never-built") {
		t.Errorf("an unscaffolded slug is outstanding work: remaining = %v", rep.Remaining)
	}
}

// TestProgressBreadcrumbAloneIsNotDone: a "completed <slug>" breadcrumb is not
// a record. Both phases here carry one and neither carries a marker, so a
// doneness reader that still consults history reports 2/2 instead of 0/2.
func TestProgressBreadcrumbAloneIsNotDone(t *testing.T) {
	dir := progressRepo(t, "v1.3", "active", "mutation-diff", "mutation-diff-scope")
	scaffoldPhase(t, dir, "mutation-diff", "")
	scaffoldPhase(t, dir, "mutation-diff-scope", "")
	touchHistory(t, dir, "completed mutation-diff")
	touchHistory(t, dir, "completed mutation-diff-scope")

	rep := progressJSON(t, "v1.3", "--json")
	if rep.Done != 0 {
		t.Errorf("done = %d, want 0 — a breadcrumb is a window, not a record", rep.Done)
	}
	if !hasSlug(rep.Remaining, "mutation-diff") || !hasSlug(rep.Remaining, "mutation-diff-scope") {
		t.Errorf("both breadcrumb-only phases are outstanding: remaining = %v", rep.Remaining)
	}
}

// TestProgressAllDoneAndStatusVerbatim: status is emitted exactly as the toml
// spells it, and every arm exits 0 — this is dispatch data, not a gate.
func TestProgressAllDoneAndStatusVerbatim(t *testing.T) {
	dir := progressRepo(t, "v2.0", "planning", "only")
	scaffoldPhase(t, dir, "only", changes.StatusComplete)

	rep := progressJSON(t, "v2.0", "--json")
	if rep.Status != "planning" {
		t.Errorf("status = %q, want the toml's own %q", rep.Status, "planning")
	}
	if !rep.AllDone {
		t.Error("all_done should be true when every listed phase is done")
	}
	if len(rep.Remaining) != 0 {
		t.Errorf("remaining = %v, want empty", rep.Remaining)
	}
}

// TestProgressNoCurrentMilestoneExitsNonZero: the bare form has to fail
// legibly, because /dross-milestone reads that failure as its "no active
// milestone → go scope one" arm.
func TestProgressNoCurrentMilestoneExitsNonZero(t *testing.T) {
	chdir(t, t.TempDir())
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}

	err := runCmd(t, Milestone(), "progress")
	if err == nil {
		t.Fatal("progress with no version and no current_milestone must exit non-zero")
	}
	if !strings.Contains(err.Error(), "current_milestone") {
		t.Errorf("the error should name the missing current_milestone: %v", err)
	}
}

// TestProgressCountsSeveralMarkerCompletePhases pins the many-phase case on a
// fixture: three phases carrying durable changes.json markers must all read
// done, on the strength of those markers rather than any breadcrumb, while an
// unfinished fourth still reads remaining.
//
// This replaces a version that ran against dross's own .dross directory. That
// one handed the real root to buildMilestoneProgress, whose phaseIsDone falls
// back to state.json history — a gitignored, machine-local file. So it passed
// locally off the fallback and depended on CI's fresh checkout to exercise the
// markers it claimed to measure: green here for a reason CI does not have,
// which is the split test-hermeticity-guard exists to close.
func TestProgressCountsSeveralMarkerCompletePhases(t *testing.T) {
	dir := progressRepo(t, "v1.3", "active", "one", "two", "three", "left")
	for _, slug := range []string{"one", "two", "three"} {
		scaffoldPhase(t, dir, slug, changes.StatusComplete)
	}
	scaffoldPhase(t, dir, "left", "")

	rep, err := buildMilestoneProgress(filepath.Join(dir, ".dross"), "v1.3")
	if err != nil {
		t.Fatal(err)
	}
	for _, slug := range []string{"one", "two", "three"} {
		if hasSlug(rep.Remaining, slug) {
			t.Errorf("%s is marker-complete but reads as remaining", slug)
		}
	}
	if rep.Done != 3 {
		t.Errorf("done = %d, want the three marker-complete phases", rep.Done)
	}
	if !hasSlug(rep.Remaining, "left") {
		t.Errorf("the unfinished phase should still read remaining: %v", rep.Remaining)
	}
}

// TestProgressWithoutStateFile pins the fresh-clone case CI caught: state.json
// is machine-local and gitignored, so a checkout nobody has run a dross command
// in has none at all. It feeds only phaseIsDone's history fallback, never the
// authoritative changes.json marker, so its absence has to degrade the fallback
// to "nothing recorded" rather than fail the command outright.
//
// A sibling test used to run this same shape against the real repo's .dross and
// relied on that fallback to pass locally; it is now
// TestProgressCountsSeveralMarkerCompletePhases, on a fixture. This test is the
// one that pins the absence of state.json directly.
func TestProgressWithoutStateFile(t *testing.T) {
	dir := progressRepo(t, "v1.3", "active", "built", "left")
	scaffoldPhase(t, dir, "built", changes.StatusComplete)
	scaffoldPhase(t, dir, "left", "")
	if err := os.Remove(filepath.Join(dir, ".dross", state.File)); err != nil {
		t.Fatalf("remove state.json: %v", err)
	}

	rep, err := buildMilestoneProgress(filepath.Join(dir, ".dross"), "v1.3")
	if err != nil {
		t.Fatalf("progress must survive a missing state.json: %v", err)
	}
	if rep.Done != 1 {
		t.Errorf("done = %d, want 1 — the durable marker alone should carry it", rep.Done)
	}
	if !hasSlug(rep.Remaining, "left") {
		t.Errorf("the unfinished phase should still read remaining: %v", rep.Remaining)
	}
}

// TestProgressPlainOutputListsWhatIsLeft covers the arm every other test in
// this file skips. They all read --json, which returns before a single line is
// printed — so the narration a human actually gets from `dross milestone
// progress` was executed by nothing. Negating either guard silences the list it
// protects while the JSON stays perfect.
func TestProgressPlainOutputListsWhatIsLeft(t *testing.T) {
	dir := progressRepo(t, "v1.3", "active", "built", "missing")
	scaffoldPhase(t, dir, "built", changes.StatusComplete)
	// "missing" is deliberately left unscaffolded, so one run puts something in
	// both lists at once.

	var out string
	if err := runCmdCapturing(t, &out, Milestone(), "progress", "v1.3"); err != nil {
		t.Fatalf("progress: %v", err)
	}
	for _, want := range []string{"1/2 phases done", "remaining: missing", "not scaffolded yet: missing"} {
		if !strings.Contains(out, want) {
			t.Errorf("the plain output does not carry %q:\n%s", want, out)
		}
	}
}

// TestProgressPlainOutputOmitsEmptyLists is the other half, and it is the half
// a loosened boundary lives in: `len(x) > 0` relaxed to `>= 0` prints both
// labels with nothing after them, which no presence assertion can see. A
// milestone with everything done is the only fixture where the two spellings
// disagree.
func TestProgressPlainOutputOmitsEmptyLists(t *testing.T) {
	dir := progressRepo(t, "v2.0", "active", "only")
	scaffoldPhase(t, dir, "only", changes.StatusComplete)

	var out string
	if err := runCmdCapturing(t, &out, Milestone(), "progress", "v2.0"); err != nil {
		t.Fatalf("progress: %v", err)
	}
	for _, unwanted := range []string{"remaining:", "not scaffolded yet:"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("an empty list still printed the %q label:\n%s", unwanted, out)
		}
	}
	if !strings.Contains(out, "dross milestone complete v2.0") {
		t.Errorf("the all-done arm should hand over the next command:\n%s", out)
	}
}

// TestPhaseDoneResolvesScaffoldedness pins the shared reader's entry point
// (phasedone.go): a caller holding only a slug gets the unscaffolded arm
// resolved for it, so `dross status` and `dross phase list` cannot answer that
// case differently from buildMilestoneProgress, which computes scaffolded-ness
// for its own reporting and calls the inner phaseIsDone.
func TestPhaseDoneResolvesScaffoldedness(t *testing.T) {
	dir := progressRepo(t, "v1.3", "active", "built", "never-built")
	scaffoldPhase(t, dir, "built", changes.StatusComplete)
	// History insists the unbuilt slug is done. It has no directory, so it is
	// not — and the entry point has to reach that on its own.
	touchHistory(t, dir, "completed never-built")

	root := filepath.Join(dir, ".dross")
	if !phaseDone(root, "built") {
		t.Error("a scaffolded phase with a complete record must read done")
	}
	if phaseDone(root, "never-built") {
		t.Error("an unscaffolded slug is never done, whatever history says")
	}
}

// TestPhaseDoneSurvivesMissingStateFile: the reader never touches state.json at
// all. It is machine-local and gitignored, so a fresh clone has none — and a
// doneness question must still be answerable there, off the durable
// changes.json marker.
func TestPhaseDoneSurvivesMissingStateFile(t *testing.T) {
	dir := progressRepo(t, "v1.3", "active", "built")
	scaffoldPhase(t, dir, "built", changes.StatusComplete)
	root := filepath.Join(dir, ".dross")
	if err := os.Remove(filepath.Join(root, state.File)); err != nil {
		t.Fatalf("remove state.json: %v", err)
	}

	if !phaseDone(root, "built") {
		t.Error("the durable marker alone must carry a phase with no state.json")
	}
}

func hasSlug(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

// touchHistory appends one action to state history.
func touchHistory(t *testing.T, dir, action string) {
	t.Helper()
	path := filepath.Join(dir, ".dross", state.File)
	s, err := state.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	s.Touch(action)
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
}
