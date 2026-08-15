package cmd

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/Rivil/dross/internal/state"
)

// reconcileFixture builds a repo with the named phase branches present and no
// completion records — the candidate shape reconcile sweeps.
func reconcileFixture(t *testing.T, ids ...string) string {
	t.Helper()
	dir := t.TempDir()
	remote := t.TempDir()
	mustGit(t, remote, "init", "-q", "--bare", "-b", "main")
	gitInit(t, dir, remote)
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatalf("init: %v", err)
	}
	mustWrite(t, filepath.Join(dir, "README.md"), "base\n")
	mustGit(t, dir, "add", ".")
	mustGit(t, dir, "commit", "-q", "-m", "chore: baseline")
	mustGit(t, dir, "push", "-q", "-u", "origin", "main")

	for _, id := range ids {
		mustWrite(t, filepath.Join(dir, ".dross", "phases", id, "spec.toml"),
			"[phase]\n  id = \""+id+"\"\n  title = \""+id+"\"\n")
		mustGit(t, dir, "add", ".dross")
		mustGit(t, dir, "commit", "-q", "-m", "chore(dross): scaffold "+id)
		mustGit(t, dir, "branch", "phase/"+id)
	}
	return dir
}

// stubReconcileRuns replaces the per-phase completion with a recorder, so the
// SWEEP's own behaviour — which phases it picks, that it continues past a
// failure, what it reports — is testable without a live forge. The gate itself
// is `dross phase complete`'s and is tested where it lives.
func stubReconcileRuns(t *testing.T, fail map[string]error) *[]string {
	t.Helper()
	var ran []string
	orig := runSubcommand
	t.Cleanup(func() { runSubcommand = orig })
	runSubcommand = func(_ *cobra.Command, _ *cobra.Command, args ...string) error {
		id := ""
		if len(args) > 0 {
			id = args[0]
		}
		ran = append(ran, id)
		return fail[id]
	}
	return &ran
}

// TestReconcileSkipsUnmergedPhases: a batch that relaxed the gate would be
// worse than the chore list it replaces — it would do the unsafe thing at N
// times the rate. A phase whose completion refuses stays exactly as it was.
func TestReconcileSkipsUnmergedPhases(t *testing.T) {
	dir := reconcileFixture(t, "alpha")
	stubReconcileRuns(t, map[string]error{"alpha": errors.New("PR #7 is not merged on origin/main")})

	var out string
	if err := runCmdCapturing(t, &out, Phase(), "reconcile"); err != nil {
		t.Fatalf("reconcile should report a skip, not fail: %v", err)
	}
	if !branchExists(t, dir, "phase/alpha") {
		t.Error("a phase whose completion refused lost its branch")
	}
	if !strings.Contains(out, "skipped") || !strings.Contains(out, "not merged") {
		t.Errorf("the skip was not reported with its reason:\n%s", out)
	}
}

// TestReconcileContinuesPastASkip: one blocked phase must not block the verb
// forever. That state — a phase whose PR is still open sitting next to three
// that merged — is precisely what produces the chore list.
func TestReconcileContinuesPastASkip(t *testing.T) {
	reconcileFixture(t, "alpha", "beta", "gamma")
	ran := stubReconcileRuns(t, map[string]error{"beta": errors.New("PR #7 is not merged")})

	var out string
	if err := runCmdCapturing(t, &out, Phase(), "reconcile"); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	for _, want := range []string{"alpha", "beta", "gamma"} {
		if !containsString(*ran, want) {
			t.Errorf("reconcile stopped before %s — a blocked phase must not abort the sweep (ran: %v)", want, *ran)
		}
	}
	if !strings.Contains(out, "reconciled 2, skipped 1") {
		t.Errorf("the tally does not match what happened:\n%s", out)
	}
}

// TestReconcileReportsPerPhaseOutcome: a sweep that reported success for a
// phase it did not complete is the worst possible output, because it is the one
// a user would act on.
func TestReconcileReportsPerPhaseOutcome(t *testing.T) {
	reconcileFixture(t, "alpha", "beta")
	stubReconcileRuns(t, map[string]error{"beta": errors.New("inconclusive merge check")})

	var out string
	if err := runCmdCapturing(t, &out, Phase(), "reconcile"); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	for _, id := range []string{"alpha", "beta"} {
		if !strings.Contains(out, "--- "+id) {
			t.Errorf("no line for %s:\n%s", id, out)
		}
	}
	if !strings.Contains(out, "inconclusive merge check") {
		t.Errorf("beta's skip reason is missing:\n%s", out)
	}
	if strings.Count(out, "skipped") < 1 {
		t.Errorf("the skip was not labelled:\n%s", out)
	}
}

// TestReconcileWithNothingToDoExitsZero: the ordinary state of a tidy repo is
// not a failure.
func TestReconcileWithNothingToDoExitsZero(t *testing.T) {
	reconcileFixture(t)
	ran := stubReconcileRuns(t, nil)

	var out string
	if err := runCmdCapturing(t, &out, Phase(), "reconcile"); err != nil {
		t.Fatalf("reconcile on a tidy repo must exit 0: %v", err)
	}
	if len(*ran) != 0 {
		t.Errorf("reconcile ran completions with no candidates: %v", *ran)
	}
	if !strings.Contains(out, "nothing to reconcile") {
		t.Errorf("reconcile did not say the repo was tidy:\n%s", out)
	}
}

// TestReconcileSkipsAlreadyCompletedPhases: the completion record is the
// signal, so a phase that was completed by hand is not swept again.
func TestReconcileSkipsAlreadyCompletedPhases(t *testing.T) {
	dir := reconcileFixture(t, "alpha", "beta")
	root := filepath.Join(dir, ".dross")
	s, err := state.Load(filepath.Join(root, state.File))
	if err != nil {
		t.Fatal(err)
	}
	s.Touch("completed alpha")
	if err := s.Save(filepath.Join(root, state.File)); err != nil {
		t.Fatal(err)
	}

	ran := stubReconcileRuns(t, nil)
	if err := runCmd(t, Phase(), "reconcile"); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if containsString(*ran, "alpha") {
		t.Errorf("reconcile re-ran a phase that already has a completion record: %v", *ran)
	}
	if !containsString(*ran, "beta") {
		t.Errorf("reconcile skipped a phase that still needs completing: %v", *ran)
	}
}

// TestReconcileIgnoresPhasesWithNoBranch: nothing left to tear down, so nothing
// to do — and running a completion against a missing branch would just produce
// a confusing refusal per phase.
func TestReconcileIgnoresPhasesWithNoBranch(t *testing.T) {
	dir := reconcileFixture(t, "alpha", "beta")
	mustGit(t, dir, "branch", "-D", "phase/alpha")

	ran := stubReconcileRuns(t, nil)
	if err := runCmd(t, Phase(), "reconcile"); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if containsString(*ran, "alpha") {
		t.Errorf("reconcile tried to complete a phase with no branch: %v", *ran)
	}
}
