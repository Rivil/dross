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

	// Precondition: no breadcrumb anywhere for "a".
	s, err := state.Load(filepath.Join(dir, ".dross", state.File))
	if err != nil {
		t.Fatal(err)
	}
	if historyCompletedPhase(s, "a") {
		t.Fatal("precondition: this fixture must carry no completion breadcrumb")
	}

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

// TestProgressHistoryFallbackMatchesWholeSlug: the fallback matches the action
// token, not a substring. historyHasAction's strings.Contains would let one
// "completed mutation-diff-scope" breadcrumb close `mutation-diff` too.
func TestProgressHistoryFallbackMatchesWholeSlug(t *testing.T) {
	dir := progressRepo(t, "v1.3", "active", "mutation-diff", "mutation-diff-scope")
	scaffoldPhase(t, dir, "mutation-diff", "")
	scaffoldPhase(t, dir, "mutation-diff-scope", "")
	touchHistory(t, dir, "completed mutation-diff-scope")

	rep := progressJSON(t, "v1.3", "--json")
	if rep.Done != 1 {
		t.Errorf("done = %d, want 1 — only the slug the breadcrumb names is done", rep.Done)
	}
	if !hasSlug(rep.Remaining, "mutation-diff") {
		t.Errorf("the shorter slug was closed by a longer slug's breadcrumb: remaining = %v", rep.Remaining)
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

// TestProgressAgainstThisRepo runs against dross's own .dross: v1.3's three
// finished phases must read done, on the strength of t-9's backfilled markers
// rather than any breadcrumb.
func TestProgressAgainstThisRepo(t *testing.T) {
	root := filepath.Join(repoRootFromTest(t), ".dross")
	rep, err := buildMilestoneProgress(root, "v1.3")
	if err != nil {
		t.Fatal(err)
	}
	for _, slug := range []string{"mutation-diff-scope", "survivor-lifecycle", "survivor-drain"} {
		if hasSlug(rep.Remaining, slug) {
			t.Errorf("%s is finished but reads as remaining", slug)
		}
	}
	if rep.Done < 3 {
		t.Errorf("done = %d, want at least the three finished v1.3 phases", rep.Done)
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
