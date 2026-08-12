package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/spf13/pflag"

	"github.com/Rivil/dross/internal/mutation"
	"github.com/Rivil/dross/internal/survivor"
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

	// Attribution is the claim under test: exactly one survivor is reported as
	// THIS PHASE's ("gremlins mutant survived"). The sibling still appears, as
	// unclassified out-of-scope debt with the verbs that clear it — visible, but
	// never attributed to this phase and never in its score.
	// The phase's own survivor is BLOCKING — an unclassified survivor inside
	// the diff is the mutation leg's fail lever. The sibling's stays a FLAG:
	// visible for draining, never able to fail a phase that did not create it.
	var phaseBlocking, siblingFlags []verify.Finding
	for _, f := range v.Findings {
		switch {
		case strings.Contains(f.Text, "mutant survived: b.go"):
			t.Errorf("the untouched sibling was attributed to this phase: %+v", f)
		case f.Severity == "FLAG" && strings.Contains(f.Text, "unclassified out-of-scope survivor: b.go"):
			siblingFlags = append(siblingFlags, f)
		case f.Severity == "BLOCKING":
			phaseBlocking = append(phaseBlocking, f)
		}
	}
	if len(phaseBlocking) != 1 || !strings.Contains(phaseBlocking[0].Text, "a.go") {
		t.Errorf("expected exactly one phase BLOCKING finding, for a.go's survivor: %+v", phaseBlocking)
	}
	if len(siblingFlags) != 1 {
		t.Errorf("the sibling's survivor must be surfaced once as unclassified: %+v", siblingFlags)
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
		// Nothing in scope survived, so nothing may be attributed to this
		// phase. The filtered mutants are still FLAGged as unclassified
		// out-of-scope debt — that is triage, not this phase's verdict, and
		// it is what keeps a mis-scoped run from reading as a clean one.
		if f.Severity == "FLAG" && !strings.Contains(f.Text, "unclassified out-of-scope survivor") {
			t.Errorf("nothing in scope survived; there is nothing to FLAG as this phase's: %+v", f)
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

// --- survivor lifecycle wiring (t-8) ---

// lifecycleRepo builds the standard two-file scoped repo: a.go is the phase's
// own file, b.go its untouched sibling, each with one survivor. Returns the
// repo dir; the phase id is "01-<slug>".
func lifecycleRepo(t *testing.T, slug string) string {
	t.Helper()
	dir := scopedVerifyRepo(t, slug)
	phaseSpec(t, "01-"+slug)
	writeScopeFile(t, dir, "a.go", "package x\n\nfunc A() bool { return 1 > 0 }\n")
	mustGit(t, dir, "commit", "-qam", "phase edits a.go")
	if err := runCmd(t, Changes(), "record", "01-"+slug, "t-1", "--files", "a.go"); err != nil {
		t.Fatal(err)
	}
	mustSetBase(t, "01-"+slug, "base")
	useStubAdapter(t, &stubMutationAdapter{name: "gremlins", exts: []string{".go"},
		report: goReport(
			map[string]mutation.FileStat{"a.go": {Survived: 1}, "b.go": {Survived: 1}},
			mutation.Mutant{File: "a.go", Line: 3, Op: "CONDITIONALS_BOUNDARY"},
			mutation.Mutant{File: "b.go", Line: 3, Op: "CONDITIONALS_NEGATION"},
		)})
	return dir
}

func runVerifyCapturing(t *testing.T, phaseID string) string {
	t.Helper()
	return captureStdout(t, func() {
		if err := runCmd(t, Verify(), phaseID); err != nil {
			t.Fatalf("verify %s: %v", phaseID, err)
		}
	})
}

// TestVerifyReadsStoreAcrossPhases is c-3 through the CLI: an acceptance
// recorded while one phase was current still suppresses in a LATER phase's
// verify run. A phase-local read would resurrect the whole drained backlog the
// moment the phase changed — the failure that killed the board mapping.
func TestVerifyReadsStoreAcrossPhases(t *testing.T) {
	dir := lifecycleRepo(t, "crossphase")

	// Accept b.go's survivor while phase 01-crossphase is current.
	if err := runCmd(t, Survivor(), "accept", "b.go:3",
		"--op", "CONDITIONALS_NEGATION", "--reason", "unreachable by design"); err != nil {
		t.Fatalf("accept: %v", err)
	}
	// Now a DIFFERENT phase becomes current and runs verify.
	if err := runCmd(t, State(), "set", "current_phase", "later"); err != nil {
		t.Fatal(err)
	}

	out := runVerifyCapturing(t, "01-crossphase")
	if !strings.Contains(out, "1 accepted") {
		t.Errorf("acceptance from an earlier phase stopped applying:\n%s", out)
	}
	vbody := mustRead(t, filepath.Join(dir, ".dross/phases/01-crossphase/verify.toml"))
	if strings.Contains(vbody, "b.go") {
		t.Errorf("accepted survivor still reported in a later phase's run:\n%s", vbody)
	}
}

// TestVerifyPrintsLifecycleCounts pins the summary line that makes the drain
// measurable, and that the four counts sum to the run's survivor total.
func TestVerifyPrintsLifecycleCounts(t *testing.T) {
	lifecycleRepo(t, "counts")

	out := runVerifyCapturing(t, "01-counts")
	if !strings.Contains(out, "survivors: 1 in-diff, 0 routed, 0 accepted, 1 unclassified") {
		t.Errorf("lifecycle summary line missing or wrong:\n%s", out)
	}
	// The unrouted, unaccepted out-of-scope survivor must NOT read as zero
	// unclassified — that is the state the whole phase exists to surface.
	if strings.Contains(out, "0 unclassified") {
		t.Errorf("an unrouted out_of_scope survivor read as zero unclassified:\n%s", out)
	}
}

// TestVerifyWithoutStoreIsUnchangedForInScopeSurvivors: with no survivors.toml,
// the phase's own findings must be exactly what they were before this phase
// landed. The lifecycle machinery is additive; it must not rewrite the verdict
// surface of a repo that never accepted anything.
func TestVerifyWithoutStoreIsUnchangedForInScopeSurvivors(t *testing.T) {
	dir := lifecycleRepo(t, "nostore")

	runVerifyCapturing(t, "01-nostore")
	if _, err := os.Stat(filepath.Join(dir, ".dross", survivor.StoreFile)); !os.IsNotExist(err) {
		t.Fatalf("verify created a store it should not have (stat err = %v)", err)
	}
	vbody := mustRead(t, filepath.Join(dir, ".dross/phases/01-nostore/verify.toml"))
	if !strings.Contains(vbody, "gremlins mutant survived: a.go:3 (CONDITIONALS_BOUNDARY)") {
		t.Errorf("in-scope FLAG text drifted from pre-phase behaviour:\n%s", vbody)
	}
	if strings.Contains(vbody, "a.go:3 (CONDITIONALS_BOUNDARY) —") {
		t.Errorf("an in-diff survivor gained an explanatory suffix it should not have:\n%s", vbody)
	}
}

// TestVerifyCorruptStoreFailsLoud: a malformed store must stop the run, not
// proceed with zero acceptances. Failing open would silently re-emit the entire
// drained backlog and, on the next accept, rewrite the file without the entries
// it could not read.
func TestVerifyCorruptStoreFailsLoud(t *testing.T) {
	dir := lifecycleRepo(t, "corrupt")
	mustWrite(t, filepath.Join(dir, ".dross", survivor.StoreFile), "[[accepted\nkey = = broken\n")

	err := runCmd(t, Verify(), "01-corrupt")
	if err == nil {
		t.Fatal("verify proceeded past a corrupt survivors.toml, want an error")
	}
	if !strings.Contains(err.Error(), "survivors") {
		t.Errorf("error should name the survivors store, got %v", err)
	}
}

// TestVerifyReportsStaleAcceptance is c-5 through the CLI: an acceptance whose
// subject is gone surfaces as exactly one NOTE naming the file and the reason —
// and changes nothing about the verdict, the score, or the exit code. A stale
// acceptance is bookkeeping, and failing the phase that happens to run next
// would punish the wrong thing.
func TestVerifyReportsStaleAcceptance(t *testing.T) {
	dir := lifecycleRepo(t, "stale")

	if err := runCmd(t, Survivor(), "accept", "b.go:3",
		"--op", "CONDITIONALS_NEGATION", "--reason", "unreachable by design"); err != nil {
		t.Fatal(err)
	}
	before := runVerifyCapturing(t, "01-stale")
	beforeToml := mustRead(t, filepath.Join(dir, ".dross/phases/01-stale/verify.toml"))

	// Rewrite b.go so the accepted text no longer occurs anywhere in it.
	writeScopeFile(t, dir, "b.go", "package x\n\nfunc B() string { return \"gone\" }\n")

	out := runVerifyCapturing(t, "01-stale")
	if n := strings.Count(out, "1 accepted"); n != 0 {
		t.Errorf("a stale acceptance still counted as suppressing: %s", out)
	}
	vbody := mustRead(t, filepath.Join(dir, ".dross/phases/01-stale/verify.toml"))
	notes := strings.Count(vbody, "stale acceptance: b.go")
	if notes != 1 {
		t.Fatalf("want exactly 1 stale-acceptance NOTE, got %d:\n%s", notes, vbody)
	}
	if !strings.Contains(vbody, survivor.ReasonTextGone) {
		t.Errorf("stale NOTE must carry the structural reason:\n%s", vbody)
	}
	if !strings.Contains(vbody, `severity = "NOTE"`) {
		t.Errorf("staleness must be a NOTE, never a gating finding:\n%s", vbody)
	}
	// The score is untouched by staleness in either direction.
	if scoreLine(t, beforeToml) != scoreLine(t, vbody) {
		t.Errorf("staleness changed the mutation score: %q vs %q", scoreLine(t, beforeToml), scoreLine(t, vbody))
	}
	if !strings.Contains(before, "in-scope mutants") || !strings.Contains(out, "in-scope mutants") {
		t.Errorf("scope summary missing from one of the runs")
	}
}

// scoreLine extracts the mutation_score line from a verify.toml body.
func scoreLine(t *testing.T, body string) string {
	t.Helper()
	for _, l := range strings.Split(body, "\n") {
		if strings.Contains(l, "mutation_score") {
			return strings.TrimSpace(l)
		}
	}
	return ""
}

// TestVerifyPersistsKeyAndStateIntoTestsJSON: the classification has to reach
// the persisted record, on both in-scope and out-of-scope survivors. A state
// that lives only in the printed summary is invisible to the next run.
func TestVerifyPersistsKeyAndStateIntoTestsJSON(t *testing.T) {
	dir := lifecycleRepo(t, "persist")

	// Route the sibling's survivor so a non-trivial state is persisted too.
	// The deferred entry lands in the CURRENT phase's spec, so point state at
	// the phase whose spec this fixture actually wrote.
	if err := runCmd(t, State(), "set", "current_phase", "01-persist"); err != nil {
		t.Fatal(err)
	}
	if err := runCmd(t, Survivor(), "route", "b.go:3",
		"--op", "CONDITIONALS_NEGATION", "--target", "01-persist"); err != nil {
		t.Fatalf("route: %v", err)
	}
	runVerifyCapturing(t, "01-persist")

	body := mustRead(t, filepath.Join(dir, ".dross/phases/01-persist/tests.json"))
	var tests verify.Tests
	if err := json.Unmarshal([]byte(body), &tests); err != nil {
		t.Fatalf("tests.json is not parseable: %v\n%s", err, body)
	}

	var inScope []mutation.Mutant
	for _, lr := range tests.Languages {
		if lr.Mutation != nil {
			inScope = append(inScope, lr.Mutation.Surviving...)
		}
	}
	if len(inScope) != 1 {
		t.Fatalf("want 1 in-scope survivor in tests.json, got %d", len(inScope))
	}
	if inScope[0].Key == "" || inScope[0].Lifecycle != verify.LifecycleInDiff {
		t.Errorf("in-scope survivor persisted without key/state: %+v", inScope[0])
	}
	if len(tests.OutOfScope) != 1 {
		t.Fatalf("want 1 out_of_scope entry, got %d", len(tests.OutOfScope))
	}
	o := tests.OutOfScope[0]
	if o.Key == "" || o.Lifecycle != verify.LifecycleRouted {
		t.Errorf("out_of_scope entry persisted without key/state: %+v", o)
	}
	if !strings.Contains(o.Note, "01-persist") {
		t.Errorf("routed entry lost its destination: %+v", o)
	}
}
