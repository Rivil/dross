package cmd

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/mutation"
	"github.com/Rivil/dross/internal/verify"
)

// TestRangedDispatchReachesTheAdapterEndToEnd is the one test in the phase
// that feeds NOTHING by hand. Every other range test builds a Scope and calls
// RunScoped; this drives `dross verify` over a real repo with a real two-line
// edit, so phaseScope → containScope → mutationCandidates (Contained.Rel()) →
// RunScoped → PlanRanges → RunRanges runs unstubbed.
//
// What it pins is the path VOCABULARY. Scope.Hunks is keyed by the diff's
// repo-relative slash path; the adapter's files come off pathfence's
// Contained.Rel(). If those two ever differ in form — a leading "./", an OS
// separator, an absolute path — PlanRanges finds no hunk for any file, records
// every one as file-absent-from-hunks, and the run measures whole files while
// still passing: that reason is INFORMATIONAL by design, so nothing else in
// the suite would notice a run that never narrows. This test would.
func TestRangedDispatchReachesTheAdapterEndToEnd(t *testing.T) {
	const (
		phaseID = "01-ranged"
		file    = "internal/pkg/c.go" // nested, so the key is more than a basename
		lines   = 60
	)
	dir := scopedVerifyRepo(t, "ranged")
	phaseSpec(t, phaseID)

	// A 60-line file in the BASE, so the phase's edit is a hunk inside it
	// rather than a whole new file whose hunk covers every line.
	writeScopeFile(t, dir, file, goFileOfLines(lines, nil))
	mustGit(t, dir, "add", file)
	mustGit(t, dir, "commit", "-qm", "base gains c.go")
	mustGit(t, dir, "branch", "-f", "base", "HEAD")

	// The phase edits lines 40 and 41 — two lines, one hunk.
	writeScopeFile(t, dir, file, goFileOfLines(lines, map[int]string{
		40: "\t_ = 40 + 1 // edited",
		41: "\t_ = 41 + 1 // edited",
	}))
	mustGit(t, dir, "commit", "-qam", "phase edits two lines of c.go")
	mustSetBase(t, phaseID, "base")

	ranger := &stubRangeAdapter{stubMutationAdapter: stubMutationAdapter{
		name: "stryker", exts: []string{".go"},
		report: goReport(map[string]mutation.FileStat{file: {Killed: 1}}),
	}}
	useStubAdapter(t, ranger)
	if err := runCmd(t, Verify(), phaseID); err != nil {
		t.Fatalf("verify: %v", err)
	}

	// The adapter was handed a non-empty map keyed by the edited file's
	// repo-relative slash path — not one key of any other form.
	if len(ranger.ranges) == 0 {
		t.Fatalf("the adapter's RunRanges was never given a range; the pipeline's path vocabulary and the adapter's have diverged (files dispatched: %v)", ranger.got)
	}
	got, ok := ranger.ranges[file]
	if !ok {
		keys := make([]string, 0, len(ranger.ranges))
		for k := range ranger.ranges {
			keys = append(keys, k)
		}
		t.Fatalf("range map keyed %v, want exactly %q", keys, file)
	}
	// Raw hunk {40,41}, padded by 25 → {15,66}: narrowed to the edit, not
	// clamped to the whole file.
	if len(got) != 1 || got[0] != (mutation.Range{Start: 15, End: 66}) {
		t.Errorf("dispatched %v, want [{15 66}] (hunk 40-41 padded by 25)", got)
	}

	// And the record says the same thing.
	tests, err := verify.LoadTests(filepath.Join(dir, ".dross/phases", phaseID, "tests.json"))
	if err != nil {
		t.Fatal(err)
	}
	if tests == nil || len(tests.Languages) == 0 {
		t.Fatal("tests.json carries no leg")
	}
	leg := tests.Languages[0]
	rec := leg.Ranges[file]
	if len(rec) != 1 || rec[0].Start != 15 || rec[0].End != 66 {
		t.Errorf("languages[0].ranges[%s] = %v, want [{15 66}]", file, rec)
	}
	if len(leg.WholeFile) != 0 {
		t.Errorf("whole_file must be empty when every file ranged, got %v", leg.WholeFile)
	}
	if raw := tests.Scope.Hunks[file]; len(raw) != 1 || raw[0] != (verify.Range{Start: 40, End: 41}) {
		t.Errorf("scope.hunks[%s] = %v, want the raw [{40 41}]", file, raw)
	}
}

// goFileOfLines writes a compilable-looking Go file of exactly n lines, with
// any line in overrides replaced, so an edit lands on a known line number.
func goFileOfLines(n int, overrides map[int]string) string {
	var b strings.Builder
	for i := 1; i <= n; i++ {
		switch {
		case overrides[i] != "":
			b.WriteString(overrides[i])
		case i == 1:
			b.WriteString("package pkg")
		case i == 2:
			b.WriteString("")
		case i == 3:
			b.WriteString("func C() {")
		case i == n:
			b.WriteString("}")
		default:
			fmt.Fprintf(&b, "\t_ = %d", i)
		}
		b.WriteString("\n")
	}
	return b.String()
}
