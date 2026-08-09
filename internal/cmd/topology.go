package cmd

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Rivil/dross/internal/project"
)

// topology is the answer to "where does my work actually live right now?" — the
// question a mid-milestone repo makes hard to answer. Under the branch model a
// correct state (phase work merged onto milestone/<version>, nothing on main
// yet) looks identical to a stuck one (a squash that never reached main) unless
// something says so out loud.
type topology struct {
	// Head is the branch HEAD is on, empty when detached.
	Head string
	// Work is the branch the current work lands on: the caller's authoritative
	// override when it supplied one, else the inferred new-work base.
	Work string
	// Main is the configured main branch.
	Main string
	// AheadOfMain counts commits on Work that Main does not have.
	AheadOfMain int
	// OnMain reports that Work is Main — nothing is staged between the work
	// and the trunk.
	OnMain bool
}

// branchTopology reads the repo's branch topology. It is a pure read — no
// fetch, no network — and it degrades rather than failing: a missing milestone
// branch, a missing origin, a detached HEAD or any git error yields a partial
// answer (Work falling back to main, AheadOfMain 0) with a nil error. Callers
// print it on every run, so it must never be the thing that breaks them. The
// only hard error is a project.toml that won't load, which has already broken
// its caller by the time this runs.
//
// workOverride, when non-empty, is used verbatim as Work. That is how a caller
// with the authoritative branch in hand — phase complete, holding the base it
// resolved from the phase's own record — keeps this helper from re-inferring
// one. Inference (resolveNewWorkBase) is only reached when the override is
// empty: acceptable for a standing status line, never for a statement about
// where a completed run actually landed. Naming an inferred branch there is
// exactly the stale-milestone lie the base-truth work removed.
func branchTopology(repoDir, root, workOverride string) (topology, error) {
	p, err := project.Load(filepath.Join(root, project.File))
	if err != nil {
		return topology{}, err
	}
	t := topology{Main: p.Repo.GitMainBranch}
	if t.Main == "" {
		t.Main = "main"
	}

	// Detached HEAD leaves Head empty rather than erroring — the rest of the
	// answer is still true and still worth printing.
	if head, err := gitTrim(repoDir, "symbolic-ref", "--short", "HEAD"); err == nil {
		t.Head = head
	}

	t.Work = workOverride
	if t.Work == "" {
		// Inference, and only here. A failure (no state.json, a git-less dir)
		// falls back to main rather than propagating.
		if base, _, err := resolveNewWorkBase(repoDir, root); err == nil {
			t.Work = base
		} else {
			t.Work = t.Main
		}
	}
	t.OnMain = t.Work == t.Main

	// Commits on Work that Main lacks — the direction that answers "is this
	// work still waiting to reach the trunk?". The reverse range counts how
	// far Work is behind, which is a different question and always 0 for a
	// freshly-merged milestone branch.
	if !t.OnMain {
		if out, err := gitTrim(repoDir, gitRefArgs("rev-list", []string{"--count"}, t.Main+".."+t.Work)...); err == nil {
			if n, err := strconv.Atoi(strings.TrimSpace(out)); err == nil {
				t.AheadOfMain = n
			}
		}
	}
	return t, nil
}

// renderTopologyLine renders the one-line topology statement shared by `dross
// status` and `dross phase complete`, so both surfaces answer the question the
// same way rather than drifting into two half-truths.
//
// The "not yet on <main>" clause hangs off exactly one condition —
// AheadOfMain > 0 — and never appears when the work branch is level with main
// (or is main). Hardcoding it would turn a finished milestone into a warning.
// The branch name is always present: the whole point is naming where the work
// is, and a bare count names nothing.
func renderTopologyLine(t topology) string {
	head := t.Head
	if head == "" {
		head = "(detached HEAD)"
	}
	if t.AheadOfMain > 0 {
		clause := fmt.Sprintf("%d %s on %s, not yet on %s",
			t.AheadOfMain, pluralCommits(t.AheadOfMain), t.Work, t.Main)
		if head == t.Work {
			// HEAD already names the work branch — don't say it twice.
			return clause
		}
		return head + " · " + clause
	}
	if head == t.Work {
		return head
	}
	return head + " · " + t.Work
}

func pluralCommits(n int) string {
	if n == 1 {
		return "commit"
	}
	return "commits"
}
