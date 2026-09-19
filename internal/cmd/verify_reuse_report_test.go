package cmd

import (
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/mutation"
	"github.com/Rivil/dross/internal/project"
)

// reuseReportRepo is a .go-only phase whose configured adapters are a gremlins
// stub AND a zero *mutation.Stryker. The real Stryker type is what
// applyReuseReport type-asserts on, so a negated `if reuseReport` guard is
// observable as its ReuseReport flag flipping; the .go-only phase guarantees
// the Stryker is never dispatched, so nothing spawns.
func reuseReportRepo(t *testing.T) *mutation.Stryker {
	t.Helper()
	dir := scopedVerifyRepo(t, "reuse")
	phaseSpec(t, "01-reuse")
	writeScopeFile(t, dir, "a.go", "package x\n\nfunc A() bool { return 1 > 0 }\n")
	mustGit(t, dir, "commit", "-qam", "phase edits a.go")
	if err := runCmd(t, Changes(), "record", "01-reuse", "t-1", "--files", "a.go"); err != nil {
		t.Fatal(err)
	}
	mustSetBase(t, "01-reuse", "base")

	gremlins := &stubMutationAdapter{name: "gremlins", exts: []string{".go"},
		report: goReport(map[string]mutation.FileStat{"a.go": {Killed: 1}})}
	stryker := &mutation.Stryker{}
	prev := configuredAdaptersFn
	configuredAdaptersFn = func(_ *project.Project, _ string, _ bool) ([]mutation.Adapter, mutationTuning, error) {
		return []mutation.Adapter{gremlins, stryker}, mutationTuning{}, nil
	}
	t.Cleanup(func() { configuredAdaptersFn = prev })
	return stryker
}

// A plain `dross verify` must never go through applyReuseReport: with the
// guard negated the run either errors ("no stryker adapter" is impossible
// here, a Stryker IS configured) or flips ReuseReport on the configured
// Stryker — so the assertion covers both arms.
func TestPlainVerifyLeavesReuseReportOff(t *testing.T) {
	stryker := reuseReportRepo(t)
	captureStdout(t, func() {
		if err := runCmd(t, Verify(), "01-reuse"); err != nil {
			t.Fatalf("plain verify: %v", err)
		}
	})
	if stryker.ReuseReport {
		t.Fatal("a plain verify flipped Stryker.ReuseReport — applyReuseReport ran without --reuse-report")
	}
}

// --reuse-report --detach is refused BY applyReuseReport, before the detach
// path's own host check. A negated guard skips the refusal, and the command
// then fails on detachRequiresAHost with a different message — so the exact
// wording is the assertion.
func TestReuseReportWithDetachRefusesThroughTheCommand(t *testing.T) {
	stryker := reuseReportRepo(t)
	err := runCmd(t, Verify(), "01-reuse", "--reuse-report", "--detach")
	if err == nil {
		t.Fatal("--reuse-report --detach was accepted")
	}
	if !strings.Contains(err.Error(), "--reuse-report with --detach") {
		t.Fatalf("refusal came from the wrong place: %v", err)
	}
	if stryker.ReuseReport {
		t.Fatal("the refusal must precede the flag flip")
	}
}
