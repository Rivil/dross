package cmd

import (
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/spf13/pflag"

	"github.com/Rivil/dross/internal/mutation"
	"github.com/Rivil/dross/internal/verify"
)

// The standing regression for this phase's whole reason to exist.
//
// Before diff scoping, `dross verify` handed gremlins a package and took the
// package's answer. A survivor in a file the phase never opened was reported
// as the phase's survivor, failed its verdict, and dragged its score down;
// a neighbour's kills lifted the same score back up. These tests drive the
// real CLI over a real git repo and assert on the artefacts, not on printed
// text, so a regression cannot hide behind a reworded summary line.

// TestScopingAttributionHoldsEndToEnd: two survivors in one package, one in
// the file the phase edited and one in its untouched sibling. Exactly one of
// them is this phase's.
func TestScopingAttributionHoldsEndToEnd(t *testing.T) {
	dir := scopedVerifyRepo(t, "attrib")
	phaseSpec(t, "01-attrib")
	writeScopeFile(t, dir, "a.go", "package x\n\nfunc A() bool { return 1 > 0 }\n")
	mustGit(t, dir, "commit", "-qam", "phase edits a.go only")
	mustSetBase(t, "01-attrib", "base")

	useStubAdapter(t, &stubMutationAdapter{name: "gremlins", exts: []string{".go"},
		report: goReport(
			map[string]mutation.FileStat{
				"a.go": {Killed: 1, Survived: 1},
				"b.go": {Killed: 9, Survived: 1},
			},
			mutation.Mutant{File: "a.go", Line: 3, Op: "CONDITIONALS_BOUNDARY"},
			mutation.Mutant{File: "b.go", Line: 3, Op: "CONDITIONALS_NEGATION"},
		)})

	if err := runCmd(t, Verify(), "01-attrib"); err != nil {
		t.Fatalf("verify: %v", err)
	}

	tests, err := verify.LoadTests(filepath.Join(dir, ".dross/phases/01-attrib/tests.json"))
	if err != nil {
		t.Fatal(err)
	}
	if tests == nil {
		t.Fatal("tests.json was not written")
	}

	// The sibling's survivor is recorded, in full, and only there.
	if len(tests.OutOfScope) != 1 {
		t.Fatalf("expected exactly one filtered survivor, got %+v", tests.OutOfScope)
	}
	if got := tests.OutOfScope[0]; got.File != "b.go" || got.Line != 3 || got.Op != "CONDITIONALS_NEGATION" {
		t.Errorf("filtered survivor lost detail: %+v", got)
	}
	for _, lr := range tests.Languages {
		if lr.Mutation == nil {
			continue
		}
		for _, m := range lr.Mutation.Surviving {
			if m.File == "b.go" {
				t.Errorf("the sibling's survivor is still a phase survivor: %+v", m)
			}
		}
		// b.go's nine kills must not have lifted the score either. Pruning
		// the survivor list alone would leave 10/11 = 0.91 here.
		if lr.Mutation.Killed != 1 {
			t.Errorf("out-of-scope kills entered the numerator: killed=%d want 1", lr.Mutation.Killed)
		}
	}

	v, err := verify.LoadVerify(filepath.Join(dir, ".dross/phases/01-attrib/verify.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if v.Summary.MutationScore != 0.5 {
		t.Errorf("score = %v want 0.50 (1 killed / 2 in scope), not 0.91", v.Summary.MutationScore)
	}
	if v.Summary.MutantsInScope != 2 {
		t.Errorf("mutants_in_scope = %d want 2", v.Summary.MutantsInScope)
	}

	var flags []verify.Finding
	for _, f := range v.Findings {
		if f.Severity == "FLAG" {
			flags = append(flags, f)
		}
		if strings.Contains(f.Text, "b.go") {
			t.Errorf("the untouched sibling reached verify.toml as a finding: %+v", f)
		}
	}
	if len(flags) != 1 || !strings.Contains(flags[0].Text, "a.go") {
		t.Errorf("expected exactly one FLAG, for a.go's survivor: %+v", flags)
	}
}

// TestScopingVacuousPassIsNotReachable is the failure mode that makes this
// phase load-bearing rather than cosmetic.
//
// If every mutant filters out — a mis-scoped run, a tool whose paths don't
// match, an adapter pointed at the wrong tree — the naive result is 0 killed,
// 0 survived, score 0.00, no findings. That is byte-for-byte what a phase with
// perfect tests and nothing left to kill looks like. The run must be
// distinguishable from a clean one by its status and by a NOTE that says how
// much was dropped.
func TestScopingVacuousPassIsNotReachable(t *testing.T) {
	dir := scopedVerifyRepo(t, "vacuous")
	phaseSpec(t, "01-vacuous")
	writeScopeFile(t, dir, "a.go", "package x\n\nfunc A() bool { return 1 > 0 }\n")
	mustGit(t, dir, "commit", "-qam", "phase edits a.go")
	mustSetBase(t, "01-vacuous", "base")

	// Paths that match nothing in scope — the shape an unprefixed gremlins
	// report or a mis-rooted stryker report actually has.
	useStubAdapter(t, &stubMutationAdapter{name: "gremlins", exts: []string{".go"},
		report: goReport(
			map[string]mutation.FileStat{"/elsewhere/a.go": {Survived: 3}},
			mutation.Mutant{File: "/elsewhere/a.go", Line: 1, Op: "X"},
			mutation.Mutant{File: "/elsewhere/a.go", Line: 2, Op: "Y"},
			mutation.Mutant{File: "/elsewhere/a.go", Line: 3, Op: "Z"},
		)})

	if err := runCmd(t, Verify(), "01-vacuous"); err != nil {
		t.Fatalf("verify: %v", err)
	}

	v, err := verify.LoadVerify(filepath.Join(dir, ".dross/phases/01-vacuous/verify.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if v.Summary.MutationStatus != verify.MutationOutOfScope {
		t.Errorf("status = %q want %q — a run that measured nothing must say so",
			v.Summary.MutationStatus, verify.MutationOutOfScope)
	}
	if v.Summary.MutantsInScope != 0 {
		t.Errorf("mutants_in_scope = %d want 0", v.Summary.MutantsInScope)
	}

	var note string
	for _, f := range v.Findings {
		if f.Severity == "NOTE" && strings.Contains(f.Text, "out-of-scope survivor") {
			note = f.Text
		}
		if f.Severity == "FLAG" {
			t.Errorf("nothing in scope survived; there is nothing to FLAG: %+v", f)
		}
	}
	if note == "" {
		t.Fatalf("no NOTE recorded the filtered set: %+v", v.Findings)
	}
	if !strings.Contains(note, "3") {
		t.Errorf("the NOTE must name how many were filtered: %q", note)
	}

	// The distinguishing test: a genuinely clean run over the same phase must
	// NOT produce this status. If both look the same, the signal is worthless.
	dir2 := scopedVerifyRepo(t, "clean")
	phaseSpec(t, "01-clean")
	writeScopeFile(t, dir2, "a.go", "package x\n\nfunc A() bool { return 1 > 0 }\n")
	mustGit(t, dir2, "commit", "-qam", "phase edits a.go")
	mustSetBase(t, "01-clean", "base")
	useStubAdapter(t, &stubMutationAdapter{name: "gremlins", exts: []string{".go"},
		report: goReport(map[string]mutation.FileStat{"a.go": {Killed: 3}})})

	if err := runCmd(t, Verify(), "01-clean"); err != nil {
		t.Fatalf("verify: %v", err)
	}
	clean, err := verify.LoadVerify(filepath.Join(dir2, ".dross/phases/01-clean/verify.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if clean.Summary.MutationStatus != verify.MutationMeasured {
		t.Errorf("a real 3/3 run must read as measured, got %q", clean.Summary.MutationStatus)
	}
	if clean.Summary.MutationScore != 1.0 || clean.Summary.MutantsInScope != 3 {
		t.Errorf("clean run: score=%v in_scope=%d want 1.00 over 3",
			clean.Summary.MutationScore, clean.Summary.MutantsInScope)
	}
}

// TestScopingHasNoOptOut pins the scoping_always_on lock by enumerating the
// command's entire flag set against an allowlist, rather than guessing at the
// names an opt-out might take. Adding any flag to `dross verify` fails this
// until it is listed here — which is the point: a new flag is exactly where an
// opt-out would arrive, and it should take a deliberate edit to add one.
func TestScopingHasNoOptOut(t *testing.T) {
	allowed := map[string]bool{
		// Skips mutation ENTIRELY (status becomes "skipped"); it does not run
		// the adapters with attribution turned off, which is what an opt-out
		// would mean.
		"skip-mutation": true,
	}

	var got []string
	Verify().Flags().VisitAll(func(f *pflag.Flag) { got = append(got, f.Name) })
	sort.Strings(got)

	for _, name := range got {
		if !allowed[name] {
			t.Errorf("unexpected flag --%s on `dross verify`: diff scoping is unconditional, "+
				"so any new flag needs a deliberate look before it is allowlisted here", name)
		}
	}
}
