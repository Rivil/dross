package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Rivil/dross/internal/mutation"
	"github.com/Rivil/dross/internal/phase"
	"github.com/Rivil/dross/internal/verify"
)

// finishFixture scaffolds the phase directory finishVerify writes into and
// returns the dross root.
func finishFixture(t *testing.T, phaseID string) string {
	t.Helper()
	root := chdirDross(t)
	if err := os.MkdirAll(filepath.Join(root, "phases", phaseID), 0o755); err != nil {
		t.Fatal(err)
	}
	return root
}

// finishSpec is the criteria list finishVerify derives the skeleton's criterion
// blocks from.
func finishSpec(phaseID string, ids ...string) *phase.Spec {
	s := &phase.Spec{Phase: phase.SpecPhase{ID: phaseID}}
	for _, id := range ids {
		s.Criteria = append(s.Criteria, phase.Criterion{ID: id})
	}
	return s
}

// finishTests is a completed run with one surviving mutant, shaped so every
// pipeline step has something to act on: a survivor for the lifecycle pass to
// classify, a language leg for the score, and a phase for the skeleton.
func finishTests(phaseID string) *verify.Tests {
	return &verify.Tests{
		Phase:       phaseID,
		GeneratedAt: time.Date(2026, 8, 30, 22, 0, 0, 0, time.UTC),
		Languages: []verify.LanguageRun{{
			Name:  "go",
			Tool:  "gremlins",
			Files: []string{"internal/cmd/verify.go"},
			Mutation: &mutation.Report{
				Tool:     "gremlins",
				Killed:   9,
				Survived: 1,
				Score:    0.9,
				Surviving: []mutation.Mutant{{
					File: "internal/cmd/verify.go",
					Line: 42,
					Op:   "CONDITIONALS_NEGATION",
				}},
			},
		}},
	}
}

// TestFinishVerifyWritesBothArtefacts is the seam's basic contract: one call
// after the mutants stop moving produces the two files the /dross-verify
// judgement step reads.
//
// This is what makes the detached path possible at all — a fetch hours later
// calls exactly this, so anything RunE still did on its own afterwards would be
// missing from every detached verdict.
func TestFinishVerifyWritesBothArtefacts(t *testing.T) {
	const id = "remote-run-detach"
	root := finishFixture(t, id)

	if err := finishVerify(root, id, finishSpec(id, "c-1", "c-2"), finishTests(id), "helicon", nil); err != nil {
		t.Fatalf("finishVerify: %v", err)
	}

	testsPath, verifyPath := verify.FilePaths(root, id)
	for _, p := range []string{testsPath, verifyPath} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("finishVerify did not write %s: %v", filepath.Base(p), err)
		}
	}
}

// TestFinishVerifyStampsTheHostThatMeasured pins measuredOn flowing through
// rather than being re-derived.
//
// The detached path's whole correctness rests on this: the report was measured
// on the host recorded at dispatch, and a pipeline that re-read today's grant
// would label it with whichever machine happens to be preferred now — a claim
// about provenance that nothing checked.
func TestFinishVerifyStampsTheHostThatMeasured(t *testing.T) {
	const id = "remote-run-detach"
	root := finishFixture(t, id)

	if err := finishVerify(root, id, finishSpec(id, "c-1"), finishTests(id), "anachryon", nil); err != nil {
		t.Fatalf("finishVerify: %v", err)
	}

	testsPath, _ := verify.FilePaths(root, id)
	raw, err := os.ReadFile(testsPath)
	if err != nil {
		t.Fatal(err)
	}
	var got verify.Tests
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.MeasuredOn != "anachryon" {
		t.Errorf("measured_on = %q, want the host passed in — a re-derived value "+
			"would name whichever host is granted now, not the one that measured", got.MeasuredOn)
	}
}

// TestFinishVerifyRecordsDeletedFilesAsSkips pins the gone-file step. It is the
// one pipeline step with no other observable effect, so a refactor that dropped
// it would leave every other assertion here passing while the record silently
// stopped explaining why a file the phase touched was never mutated.
func TestFinishVerifyRecordsDeletedFilesAsSkips(t *testing.T) {
	const id = "remote-run-detach"
	root := finishFixture(t, id)

	gone := []string{"internal/cmd/deleted.go"}
	if err := finishVerify(root, id, finishSpec(id, "c-1"), finishTests(id), "helicon", gone); err != nil {
		t.Fatalf("finishVerify: %v", err)
	}

	testsPath, _ := verify.FilePaths(root, id)
	raw, err := os.ReadFile(testsPath)
	if err != nil {
		t.Fatal(err)
	}
	var got verify.Tests
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, s := range got.Skipped {
		if s.File == gone[0] {
			found = true
			if !strings.Contains(s.Reason, "no longer exists") {
				t.Errorf("the skip does not say why: %q", s.Reason)
			}
		}
	}
	if !found {
		t.Errorf("a deleted file was dropped from the record entirely: %+v", got.Skipped)
	}
}

// TestFinishVerifyClassifiesSurvivorLifecycle is the step whose absence would
// be least visible and most damaging: without it every survivor reaches
// verify.toml unclassified, the gate reads as open on a phase that has nothing
// wrong with it, and the operator drains survivors that were never survivors.
//
// Asserted on the PERSISTED record rather than on a return value, because
// tests.json is what the next run and the judgement step read.
func TestFinishVerifyClassifiesSurvivorLifecycle(t *testing.T) {
	const id = "remote-run-detach"
	root := finishFixture(t, id)

	if err := finishVerify(root, id, finishSpec(id, "c-1"), finishTests(id), "helicon", nil); err != nil {
		t.Fatalf("finishVerify: %v", err)
	}

	testsPath, _ := verify.FilePaths(root, id)
	raw, err := os.ReadFile(testsPath)
	if err != nil {
		t.Fatal(err)
	}
	var got verify.Tests
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Languages) == 0 || got.Languages[0].Mutation == nil {
		t.Fatalf("the language leg did not survive the write: %+v", got.Languages)
	}
	surviving := got.Languages[0].Mutation.Surviving
	if len(surviving) != 1 {
		t.Fatalf("want 1 survivor in the record, got %d", len(surviving))
	}
	if surviving[0].Lifecycle == "" {
		t.Error("the survivor reached tests.json with no lifecycle state — the " +
			"classification step ran after the write, or not at all")
	}
}

// TestFinishVerifySeedsEveryCriterion pins the skeleton step against the spec
// it was given. A skeleton built from the wrong source — the previous
// verify.toml, or the tests.json phase name — would produce a file the
// judgement step fills in for criteria this phase does not have.
func TestFinishVerifySeedsEveryCriterion(t *testing.T) {
	const id = "remote-run-detach"
	root := finishFixture(t, id)

	if err := finishVerify(root, id, finishSpec(id, "c-1", "c-2", "c-3"), finishTests(id), "helicon", nil); err != nil {
		t.Fatalf("finishVerify: %v", err)
	}

	_, verifyPath := verify.FilePaths(root, id)
	raw, err := os.ReadFile(verifyPath)
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	for _, want := range []string{`"c-1"`, `"c-2"`, `"c-3"`} {
		if !strings.Contains(body, want) {
			t.Errorf("verify.toml carries no block for %s:\n%s", want, body)
		}
	}
	if !strings.Contains(body, `verdict = "pending"`) {
		t.Errorf("the skeleton did not land pending — the judgement step is what resolves it:\n%s", body)
	}
}

// TestFinishVerifyIsTheOnlyArtefactWriter guards the extraction itself.
//
// The refactor's failure mode is a step left behind in RunE: the attached path
// keeps working because it still does that step inline, and only the detached
// path — which nothing exercises yet — silently loses it. Asserting that
// verify.go writes tests.json and verify.toml through exactly one call site
// catches the half-extraction that both other tests would pass.
func TestFinishVerifyIsTheOnlyArtefactWriter(t *testing.T) {
	src, err := os.ReadFile("verify.go")
	if err != nil {
		t.Fatal(err)
	}
	body := string(src)
	// Counted on the WRITES, not on verify.FilePaths: finalizeVerify resolves
	// the same two paths in order to READ verify.toml and stamp the resolved
	// verdict, which is a legitimate second reader. Conflating the two made an
	// earlier draft of this test fail on correct code — the property that
	// actually matters is that nothing constructs or persists the artefacts
	// outside the pipeline.
	if n := strings.Count(body, "verify.Skeleton("); n != 1 {
		t.Errorf("verify.Skeleton is called %d times in verify.go, want 1 — a "+
			"second construction site builds a skeleton without the lifecycle "+
			"and staleness steps that must precede it", n)
	}
	if n := strings.Count(body, "t.Save(testsPath)"); n != 1 {
		t.Errorf("tests.json is persisted from %d sites in verify.go, want 1 — a "+
			"second writer would land a record whose survivors were never classified", n)
	}
}
