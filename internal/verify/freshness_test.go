package verify

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

const (
	treeA   = "1111111111111111111111111111111111111111"
	treeB   = "2222222222222222222222222222222222222222"
	commitA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	commitB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

var runAt = time.Date(2026, 10, 5, 3, 0, 0, 0, time.UTC)

// diffSpy is a TreeDiffer that records whether it was asked.
type diffSpy struct {
	calls int
	names []string
	err   error
}

func (d *diffSpy) Diff(a, b string) ([]string, error) {
	d.calls++
	return d.names, d.err
}

func stamped(tree, commit string) *Verify {
	return &Verify{Verify: VerifyMeta{Phase: "p", GeneratedAt: runAt, Verdict: "pass", MeasuredCommit: commit, MeasuredTree: tree}}
}

// TestSkeletonCarriesMeasuredTree: the run's tree reaches verify.toml, and a
// finalize-shaped rewrite — load, resolve the verdict, mark it finalized, save —
// keeps it.
func TestSkeletonCarriesMeasuredTree(t *testing.T) {
	run := &Tests{Phase: "p", GeneratedAt: runAt, MeasuredCommit: commitA, MeasuredTree: treeA}
	v := Skeleton(run, []string{"c-1"})
	if v.Verify.MeasuredCommit != commitA || v.Verify.MeasuredTree != treeA {
		t.Fatalf("Skeleton [verify] = %+v, want the run's commit and tree", v.Verify)
	}
	path := filepath.Join(t.TempDir(), VerifyFile)
	if err := v.Save(path); err != nil {
		t.Fatal(err)
	}
	back, err := LoadVerify(path)
	if err != nil {
		t.Fatal(err)
	}
	back.Verify.Verdict, back.Verify.Finalized, back.Verify.FinalizedAt = "pass", true, runAt
	if err := back.Save(path); err != nil {
		t.Fatal(err)
	}
	again, err := LoadVerify(path)
	if err != nil {
		t.Fatal(err)
	}
	if again.Verify.MeasuredCommit != commitA || again.Verify.MeasuredTree != treeA {
		t.Errorf("a finalize-shaped rewrite dropped the tree: %+v", again.Verify)
	}
}

// TestLegacyVerifyRoundTripsByteIdentical: a verify.toml written before the
// fields existed — this repo's own, from the phase that shipped just before
// them — re-saves byte for byte, with no measured_* keys.
func TestLegacyVerifyRoundTripsByteIdentical(t *testing.T) {
	legacy, err := os.ReadFile(filepath.Join("..", "..", ".dross", "phases", "board-finalize-on-complete", VerifyFile))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(legacy), "measured_tree") {
		t.Fatal("the legacy fixture already records a tree — it no longer stands for a pre-phase verdict")
	}
	path := filepath.Join(t.TempDir(), VerifyFile)
	if err := os.WriteFile(path, legacy, 0o644); err != nil {
		t.Fatal(err)
	}
	v, err := LoadVerify(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := v.Save(path); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(legacy) {
		t.Errorf("a legacy verify.toml did not round-trip byte for byte:\n%s", after)
	}
	if strings.Contains(string(after), "measured_commit") || strings.Contains(string(after), "measured_tree") {
		t.Errorf("a verify.toml with no tree grew measured_* keys:\n%s", after)
	}
}

// TestClassifyLegacyIsUnknownWithoutGit: no recorded tree anywhere is unknown,
// decided without asking git.
func TestClassifyLegacyIsUnknownWithoutGit(t *testing.T) {
	t.Chdir(t.TempDir()) // outside any repository: a stray git call has nothing to answer it
	spy := &diffSpy{}
	for _, tc := range []struct {
		name string
		v    *Verify
		run  *Tests
	}{
		{"no verify.toml", nil, nil},
		{"verify.toml without a tree", stamped("", ""), nil},
		{"tests.json without a tree", stamped("", ""), &Tests{GeneratedAt: runAt}},
	} {
		r := ClassifyFreshness(tc.v, tc.run, TreeRef{Tree: treeB}, spy)
		if r.State != FreshnessUnknown {
			t.Errorf("%s: state = %v, want unknown", tc.name, r.State)
		}
	}
	if spy.calls != 0 {
		t.Errorf("an unknown verdict asked git for a diff %d time(s)", spy.calls)
	}
}

// TestClassifyFallsBackToTestsJSON: a verify.toml whose tree was dropped by a
// hand edit is judged by tests.json's copy — but only the copy from the same
// run.
func TestClassifyFallsBackToTestsJSON(t *testing.T) {
	v := stamped("", "")
	run := &Tests{GeneratedAt: runAt, MeasuredCommit: commitA, MeasuredTree: treeA}
	if r := ClassifyFreshness(v, run, TreeRef{Tree: treeA}, (&diffSpy{})); r.State != Fresh || r.Source != TestsFile {
		t.Errorf("same-run fallback, same tree: %+v, want fresh from %s", r, TestsFile)
	}
	if r := ClassifyFreshness(v, run, TreeRef{Tree: treeB}, &diffSpy{names: []string{"a.go"}}); r.State != Stale {
		t.Errorf("same-run fallback, moved tree: %+v, want stale", r)
	}
	other := &Tests{GeneratedAt: runAt.Add(time.Hour), MeasuredTree: treeA}
	if r := ClassifyFreshness(v, other, TreeRef{Tree: treeA}, (&diffSpy{})); r.State != FreshnessUnknown {
		t.Errorf("a tests.json from another run vouched for this verdict: %+v", r)
	}
}

// TestClassifyFreshByTreeNotCommit: freshness is the tree's. A recorded commit
// HEAD has since moved past is still fresh when the tree is the same.
func TestClassifyFreshByTreeNotCommit(t *testing.T) {
	spy := &diffSpy{}
	r := ClassifyFreshness(stamped(treeA, commitA), nil, TreeRef{Commit: commitB, Tree: treeA}, spy)
	if r.State != Fresh || spy.calls != 0 {
		t.Errorf("equal trees under different commits: %+v (diff calls %d), want fresh without a diff", r, spy.calls)
	}
}

// TestClassifyRejectsMalformedTree: a recorded value that is not an object id
// is malformed and never reaches git.
func TestClassifyRejectsMalformedTree(t *testing.T) {
	for _, bad := range []string{"HEAD", "--output=x", "abc", strings.ToUpper(commitA)} {
		spy := &diffSpy{}
		r := ClassifyFreshness(stamped(bad, ""), nil, TreeRef{Tree: treeB}, spy)
		if r.State != Malformed || r.Field != VerifyFile+" measured_tree" || spy.calls != 0 {
			t.Errorf("tree %q: %+v (diff calls %d), want malformed measured_tree, git never asked", bad, r, spy.calls)
		}
	}
	spy := &diffSpy{}
	r := ClassifyFreshness(stamped(treeA, "--output=x"), nil, TreeRef{Tree: treeB}, spy)
	if r.State != Malformed || r.Field != VerifyFile+" measured_commit" || spy.calls != 0 {
		t.Errorf("malformed commit: %+v (diff calls %d), want malformed measured_commit", r, spy.calls)
	}
}

// TestClassifyStaleNamesAndUnlistable: a moved tree is stale with the changed
// paths sorted; a diff that fails is stale all the same, with the reason kept.
func TestClassifyStaleNamesAndUnlistable(t *testing.T) {
	spy := &diffSpy{names: []string{"z.go", "a.go", "m/n.go"}}
	r := ClassifyFreshness(stamped(treeA, commitA), nil, TreeRef{Tree: treeB}, spy)
	if r.State != Stale || !reflect.DeepEqual(r.Changed, []string{"a.go", "m/n.go", "z.go"}) || r.ListErr != nil {
		t.Errorf("moved tree: %+v, want stale naming a.go, m/n.go, z.go", r)
	}
	if r.Recorded.Tree != treeA || r.Source != VerifyFile {
		t.Errorf("recorded = %+v from %q, want %s from %s", r.Recorded, r.Source, treeA, VerifyFile)
	}
	pruned := &diffSpy{err: errors.New("bad object " + treeA)}
	r = ClassifyFreshness(stamped(treeA, commitA), nil, TreeRef{Tree: treeB}, pruned)
	if r.State != Stale || r.ListErr == nil || len(r.Changed) != 0 {
		t.Errorf("unlistable diff: %+v, want stale with the list error", r)
	}
}
