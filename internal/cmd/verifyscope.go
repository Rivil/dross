package cmd

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Rivil/dross/internal/pathfence"
	"github.com/Rivil/dross/internal/verify"
)

// phaseScope derives the verify.Scope for a phase: what git says the phase
// changed, unioned with what changes.json recorded.
//
// The GIT side never returns an error. Every git step that can fail degrades to
// the changes.json side with the reason recorded on the Scope, because the
// alternative — aborting verify because a base ref went missing — turns a
// bookkeeping gap into a blocked phase. What must never happen is the quiet
// version of the same thing: a scope that silently narrowed and produced a
// clean-looking pass. That is what Scope.Degraded exists to make visible.
//
// The RECORDED side is the one hard error, and it is taken FIRST, before any
// git work and before NewScope: a changes.json path that escapes the repo is a
// corrupt or hand-edited artifact, not a missing ref, and the soft lane would
// hide it behind a passing run (the escape_failure_mode lock). A refusal
// returns a nil scope alongside the error — there is no partial scope to
// inspect, because nothing was scoped.
//
// The diff is taken between the resolved merge-base and HEAD, not the working
// tree. A scope that shifted with unsaved edits could not be reproduced from
// the recorded base and the commit history, and c-6 is exactly the requirement
// that a mis-scoped run stays diagnosable after the fact.
//
// The base is the phase's fork point, and it is resolved in three tiers (see
// resolveScopeBase): an explicit --base wins outright, the merge-base of the
// recorded branch and HEAD is the normal case, and once the phase has MERGED —
// HEAD sits on the base branch, so that merge-base is HEAD itself and the diff
// is empty — the recorded base_commit stands in. Without that tier a post-merge
// re-verify silently collapsed to the changes-only, whole-file scope while
// still reporting pass.
func phaseScope(repoDir string, base scopeBase, recorded []string) (*verify.Scope, error) {
	if err := verify.ValidateRecorded(repoDir, recorded); err != nil {
		return nil, err
	}

	in := verify.ScopeInput{
		Root:     repoDir,
		Recorded: recorded,
	}

	sha, degraded, err := resolveScopeBase(repoDir, base)
	if err != nil {
		return nil, err
	}
	in.Degraded = append(in.Degraded, degraded...)
	if sha == "" {
		return verify.NewScope(in), nil
	}
	// The resolved sha, not the ref it came from: a base branch that has since
	// moved makes "merged from milestone/v1.3" indistinguishable from a stale
	// scope, whereas a sha can be checked out and diffed.
	in.Base = sha

	// -z suppresses git's path quoting outright, so a filename carrying
	// non-ASCII bytes arrives as itself rather than as an escaped literal that
	// would match no mutant. --no-renames emits both sides of a rename, which
	// puts the old and the new path in scope — the wider, fail-open reading.
	names, err := gitTrim(repoDir,
		gitRefArgs("diff", []string{"--name-only", "--no-renames", "-z"}, sha, "HEAD")...)
	if err != nil {
		in.Degraded = append(in.Degraded,
			fmt.Sprintf("git diff --name-only against %s failed: %v", short(sha), gitReason(err)))
		return verify.NewScope(in), nil
	}
	for _, f := range strings.Split(names, "\x00") {
		if f = strings.TrimSpace(f); f != "" {
			in.Git = append(in.Git, f)
		}
	}

	// Hunks refine the in-hunk vs inherited tag only. Losing them costs
	// precision, never scope, so a failure here degrades and carries on with
	// the file set already collected.
	patch, err := gitTrim(repoDir,
		gitRefArgs("diff", []string{"-U0", "--no-renames", "--no-color"}, sha, "HEAD")...)
	if err != nil {
		in.Degraded = append(in.Degraded,
			fmt.Sprintf("git diff -U0 against %s failed; survivors cannot be tagged in-hunk: %v",
				short(sha), gitReason(err)))
		return verify.NewScope(in), nil
	}
	hunks, degraded := verify.ParseHunks(patch)
	in.Hunks = hunks
	in.Degraded = append(in.Degraded, degraded...)

	return verify.NewScope(in), nil
}

// scopeBase carries the three things the diff base can be resolved from.
type scopeBase struct {
	Branch    string // changes.json `base` — the branch the phase forked from
	ForkPoint string // changes.json `base_commit` — the sha Branch held at the fork; "" on old records
	Override  string // --base <rev>: an explicit fork point that beats both
}

// resolveScopeBase picks the sha the phase diff starts from. It returns "" with
// the reason on degraded when no usable base exists — the changes-only lane —
// and an error only for an Override that does not resolve: the user typed that
// rev, and a typo degrading into whole-file measurement would hide exactly the
// mistake the flag exists to correct.
//
// The post-merge tier keys on merge-base(Branch, HEAD) == HEAD. On the phase
// branch the merge-base is the fork point and never HEAD; once the phase has
// merged and HEAD is on Branch, it always is, and the honest diff is
// base_commit..HEAD. That range can carry sibling work merged since the fork,
// which is more measurement, never less — but it is a substituted base, so it
// is named on Degraded rather than left to read as the ordinary path.
func resolveScopeBase(repoDir string, b scopeBase) (sha string, degraded []string, err error) {
	if rev := strings.TrimSpace(b.Override); rev != "" {
		sha, err = gitTrim(repoDir, gitRefArgs("rev-parse", []string{"--verify", "--quiet"}, rev+"^{commit}")...)
		if err != nil || sha == "" {
			return "", nil, fmt.Errorf("--base %q does not resolve to a commit: %s", rev, gitReason(err))
		}
		return sha, []string{fmt.Sprintf("base overridden by --base: diffing %s..HEAD", short(sha))}, nil
	}

	branch := strings.TrimSpace(b.Branch)
	if branch == "" {
		return "", []string{"changes.json records no base branch, so no git diff could be taken"}, nil
	}

	sha, err = gitTrim(repoDir, gitRefArgs("merge-base", nil, branch, "HEAD")...)
	if err != nil || sha == "" {
		return "", []string{fmt.Sprintf("could not resolve merge-base of %q and HEAD: %v", branch, gitReason(err))}, nil
	}

	// --verify, because rev-parse only honours --end-of-options in that mode
	// and otherwise echoes it as output.
	head, err := gitTrim(repoDir, gitRefArgs("rev-parse", []string{"--verify", "--quiet"}, "HEAD^{commit}")...)
	if err != nil || head != sha {
		// A HEAD that will not resolve is left to the diff steps to report;
		// the ordinary tier holds.
		return sha, nil, nil
	}

	fork := strings.TrimSpace(b.ForkPoint)
	if fork == "" {
		return "", []string{fmt.Sprintf(
			"phase already merged (merge-base of %q and HEAD is HEAD itself) and changes.json records no base_commit; pass --base <fork-sha> to diff from the fork point", branch)}, nil
	}
	forkSHA, err := gitTrim(repoDir, gitRefArgs("rev-parse", []string{"--verify", "--quiet"}, fork+"^{commit}")...)
	if err != nil || forkSHA == "" {
		return "", []string{fmt.Sprintf(
			"phase already merged and the recorded base_commit %s does not resolve: %s; pass --base <fork-sha>", short(fork), gitReason(err))}, nil
	}
	return forkSHA, []string{fmt.Sprintf(
		"phase already merged: diffing recorded fork point %s..HEAD, which may include sibling work merged since", short(forkSHA))}, nil
}

// gitReason renders a git failure for a degraded entry. exec errors carry only
// "exit status 1", which says nothing on its own, so a nil error (an empty but
// successful result) is spelled out rather than printed as "<nil>".
func gitReason(err error) string {
	if err == nil {
		return "no output"
	}
	return err.Error()
}

// containScope converts the scope's file set into the []pathfence.Contained
// mutationCandidates takes, so the path that reaches the filesystem there was
// built by the containment check rather than merely believed to be safe.
//
// It converts the UNION — scope.Files — and not the recorded set the gate in
// phaseScope validated. Those are different sets on purpose: a file git saw
// change but no task recorded must still be mutated, or its survivors could
// gate nothing. Feeding this ValidateRecorded's input instead would pass every
// containment assertion and silently narrow the mutation scope, which is the
// false-green shape this phase exists to close.
//
// Every scope.Files entry is already normalised, repo-relative and in-tree, so
// the conversion is total in practice. It returns an error rather than dropping
// the odd entry anyway: a future NewScope that admits something else must
// surface here, not shrink the mutation set on the quiet.
func containScope(repoDir string, s *verify.Scope) ([]pathfence.Contained, error) {
	if s == nil {
		return nil, nil
	}
	out := make([]pathfence.Contained, 0, len(s.Files))
	for _, f := range s.Files {
		c, err := pathfence.Contain(repoDir, "verify scope", f)
		if err != nil {
			return nil, fmt.Errorf("verify scope: %w", err)
		}
		out = append(out, c)
	}
	return out, nil
}

// verifyScope is `dross verify scope <phase-id>`: the last run's range
// provenance, read from tests.json and nothing else. It answers the question
// the score cannot — "which lines did this run actually instrument, and where
// did it fall back to the whole file, and why" — from the record the run
// itself wrote, so a claimed scope is checkable rather than inferred.
func verifyScope() *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "scope <phase-id>",
		Short: "Print the last verify run's range provenance: in-scope files, raw hunks, and per leg the effective ranges or whole-file fallback",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			phaseID := args[0]
			root, err := FindRoot()
			if err != nil {
				return err
			}
			testsPath, _ := verify.FilePaths(root, phaseID)
			tests, err := verify.LoadTests(testsPath)
			if err != nil {
				return err
			}
			if tests == nil {
				// The fix is named, not implied: a missing record is not an
				// empty scope, and "not found" would leave the reader to
				// guess whether the phase or the run is what is absent.
				return fmt.Errorf("no verify run recorded for %s — run `dross verify %s` first", phaseID, phaseID)
			}
			prov := verify.ProvenanceOf(tests)
			if asJSON {
				b, err := json.MarshalIndent(prov, "", "  ")
				if err != nil {
					return err
				}
				Print(string(b))
				return nil
			}
			printProvenance(tests, prov)
			return nil
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "emit the provenance record as JSON — the same values, nothing re-rendered")
	return c
}

// printProvenance is the human form: scope header, each in-scope file with its
// raw hunks, then per leg what ranged and what fell back. A leg recorded
// before provenance existed says so rather than printing as whole-file — the
// record does not know, and neither must the readout claim to.
func printProvenance(t *verify.Tests, p verify.Provenance) {
	if t.Scope == nil {
		Printf("verify scope: phase %s — unscoped run (no scope recorded)\n", p.Phase)
	} else {
		base := "no base resolved"
		if t.Scope.Base != "" {
			base = "base " + short(t.Scope.Base)
		}
		Printf("verify scope: phase %s — %d file(s) from %s, %s\n", p.Phase, len(p.Files), t.Scope.Source, base)
	}
	for _, f := range p.Files {
		hunks := p.Hunks[f]
		if len(hunks) == 0 {
			Printf("  %s (no hunks)\n", f)
			continue
		}
		spans := make([]string, 0, len(hunks))
		for _, h := range hunks {
			spans = append(spans, fmt.Sprintf("%d-%d", h.Start, h.End))
		}
		Printf("  %s  hunks %s\n", f, strings.Join(spans, ", "))
	}
	for _, leg := range p.Legs {
		Printf("leg %s (%s): %d file(s)\n", leg.Name, leg.Tool, len(leg.Files))
		if len(leg.Ranges) == 0 && len(leg.WholeFile) == 0 {
			Print("  no range provenance recorded")
			continue
		}
		for _, f := range sortedMapKeys(leg.Ranges) {
			spans := make([]string, 0, len(leg.Ranges[f]))
			for _, r := range leg.Ranges[f] {
				spans = append(spans, fmt.Sprintf("%d-%d (%s)", r.Start, r.End, constructLabel(r)))
			}
			Printf("  ranged %s  %s\n", f, strings.Join(spans, ", "))
		}
		for _, f := range sortedMapKeys(leg.WholeFile) {
			Printf("  whole-file %s — %s\n", f, leg.WholeFile[f])
		}
	}
}
