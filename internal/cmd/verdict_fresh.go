package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/Rivil/dross/internal/treefp"
	"github.com/Rivil/dross/internal/verify"
)

// verdictFreshness judges a phase's recorded verdict against the tree in
// repoDir now: verify.toml's measured tree, or tests.json's copy from the same
// run when a hand edit dropped it.
//
// A verdict that records no tree at all — written before trees were kept — is
// unknown without the tree being taken, so a legacy phase never pays for, or
// fails on, a capture it cannot use.
func verdictFreshness(root, repoDir, phaseID string) (verify.FreshnessReport, error) {
	testsPath, verifyPath := verify.FilePaths(root, phaseID)
	v, err := verify.LoadVerify(verifyPath)
	if err != nil {
		return verify.FreshnessReport{}, err
	}
	if v == nil {
		return verify.FreshnessReport{State: verify.FreshnessUnknown}, nil
	}
	// An unreadable tests.json is not evidence; without it there is simply no
	// fallback copy, and ClassifyFreshness reads verify.toml alone.
	t, _ := verify.LoadTests(testsPath)
	// The fallback rule is ClassifyFreshness's own (same generated_at); it is
	// asked here only to decide whether a capture is needed at all.
	if v.Verify.MeasuredTree == "" && (t == nil || t.MeasuredTree == "" || !t.GeneratedAt.Equal(v.Verify.GeneratedAt)) {
		return verify.FreshnessReport{State: verify.FreshnessUnknown}, nil
	}
	cur, err := measuredTreeFn(repoDir)
	if err != nil {
		return verify.FreshnessReport{}, fmt.Errorf("fingerprint the tree that would ship: %w", err)
	}
	return verify.ClassifyFreshness(v, t, verify.TreeRef{Commit: cur.Commit, Tree: cur.Tree}, treeDiffer{dir: repoDir}), nil
}

// treeDiffer lists changed paths through treefp in one repository.
type treeDiffer struct{ dir string }

func (d treeDiffer) Diff(a, b string) ([]string, error) { return treefp.Diff(d.dir, a, b) }

// gateFreshness refuses a pass verdict that no longer covers the tree it would
// ship (locked stale_override: --force-unverified is the one override). A
// verdict with no recorded tree warns and proceeds (locked legacy_freshness).
// Warnings go to stderr, so `ship --json` keeps stdout to its one object.
func gateFreshness(root, repoDir, phaseID string, force bool) error {
	r, err := verdictFreshness(root, repoDir, phaseID)
	if err != nil {
		if force {
			fmt.Fprintf(os.Stderr, "warning: %s's verdict freshness could not be checked (%v) — shipping anyway under --force-unverified\n", phaseID, err)
			return nil
		}
		return fmt.Errorf("could not check that %s's pass verdict still covers the tree: %w\nRe-run `dross verify %s` and /dross-verify, or pass --force-unverified to override", phaseID, err, phaseID)
	}
	var problem string
	switch r.State {
	case verify.Fresh:
		return nil
	case verify.FreshnessUnknown:
		fmt.Fprintf(os.Stderr, "warning: %s's verdict records no measured tree (written before trees were kept) — freshness unknown; shipping on the pass alone\n", phaseID)
		return nil
	case verify.Malformed:
		problem = fmt.Sprintf("its %s is not an object id (%q)", r.Field, recordedValue(r))
	case verify.Stale:
		if r.ListErr != nil {
			problem = fmt.Sprintf("the tree it measured is gone from this clone, so the files changed since could not be listed (%v)", r.ListErr)
		} else {
			problem = fmt.Sprintf("%d file(s) changed since the run measured its tree:\n  %s", len(r.Changed), strings.Join(r.Changed, "\n  "))
		}
	}
	if force {
		fmt.Fprintf(os.Stderr, "warning: shipping a stale pass under --force-unverified — %s\n", problemCount(r, problem))
		return nil
	}
	return fmt.Errorf("%s's pass verdict is stale: %s\nRe-measure with `dross verify %s` and /dross-verify, or pass --force-unverified to override", phaseID, problem, phaseID)
}

// recordedValue is the malformed field's value, for the refusal to quote.
func recordedValue(r verify.FreshnessReport) string {
	if strings.HasSuffix(r.Field, "measured_commit") {
		return r.Recorded.Commit
	}
	return r.Recorded.Tree
}

// problemCount is the override warning's one-line form of a stale verdict.
func problemCount(r verify.FreshnessReport, problem string) string {
	if r.State == verify.Stale && r.ListErr == nil {
		return fmt.Sprintf("%d file(s) changed since the run measured its tree", len(r.Changed))
	}
	return problem
}
