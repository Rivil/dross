package cmd

import (
	"fmt"
	"strings"
)

// originDelta is how a local branch stands against its origin copy after a
// fetch. It is the one shared reading behind every push dross makes on the
// user's behalf (the shared_origin_gate locked decision): the base safety net
// and the phase-branch push both consume it, neither runs its own rev-list.
type originDelta struct {
	// Missing: origin/<branch> does not exist — never pushed. Ahead and
	// Behind are meaningless when set.
	Missing bool
	// Ahead: commits local has that origin lacks, newest first.
	Ahead []string
	// Behind: origin has at least one commit local lacks.
	Behind bool
}

// compareWithOrigin fetches origin and classifies <branch> against
// origin/<branch>. The fetch is load-bearing: without it a commit pushed from
// another machine reads as in-sync and the caller pushes into a divergence it
// never saw. A missing remote ref is a state, not an error — it is the first
// push of every branch.
func compareWithOrigin(repoDir, branch string) (originDelta, error) {
	var d originDelta
	if out, err := gitCombined(repoDir, "fetch", "origin"); err != nil {
		return d, fmt.Errorf("git fetch: %w\n%s", err, out)
	}
	if gitNoOut(repoDir, gitRefArgs("rev-parse", []string{"--verify"}, "refs/remotes/origin/"+branch)...) != nil {
		d.Missing = true
		return d, nil
	}
	ahead, err := gitTrim(repoDir, gitRefArgs("rev-list", nil, "origin/"+branch+".."+branch)...)
	if err != nil {
		return d, fmt.Errorf("git rev-list origin/%s..%s: %w", branch, branch, err)
	}
	d.Ahead = strings.Fields(ahead)
	behind, err := gitTrim(repoDir, gitRefArgs("rev-list", nil, branch+"..origin/"+branch)...)
	if err != nil {
		return d, fmt.Errorf("git rev-list %s..origin/%s: %w", branch, branch, err)
	}
	d.Behind = behind != ""
	return d, nil
}

// pushPhaseBranch pushes phase/<id> to origin, gated on the origin comparison
// rather than on the local index — a commit that exists locally but not on
// origin is pushed whether or not anything is staged. Four arms:
//
//   - missing, or ahead and not behind → `git push -u origin <branch>`
//   - level → nothing to push; pushed=false, err=nil
//   - behind only → refuse, naming the pull. Local must carry origin's tip
//     before it ships; there is nothing here to force, so --force is not
//     offered.
//   - diverged (ahead AND behind) → refuse, naming the pull and
//     `dross ship --force`; with force set, `--force-with-lease` instead
//     (the diverged_phase_branch locked decision: never merge or force on
//     dross's own initiative).
//
// `-u` rides on every push so the prompt's later bare `git push` on the
// branch has an upstream to resolve.
func pushPhaseBranch(repoDir, branch string, force bool) (pushed bool, err error) {
	d, err := compareWithOrigin(repoDir, branch)
	if err != nil {
		return false, err
	}
	pull := "git pull --rebase origin " + branch
	switch {
	case d.Missing, len(d.Ahead) > 0 && !d.Behind:
		// fall through to the plain push below
	case len(d.Ahead) == 0 && !d.Behind:
		return false, nil
	case d.Behind && len(d.Ahead) == 0:
		return false, fmt.Errorf("origin/%s has commits local %s lacks — "+
			"run `%s` to bring it up to date, then re-run", branch, branch, pull)
	case !force:
		return false, fmt.Errorf("%s has diverged from origin/%s (each has commits the other lacks) — "+
			"run `%s` to reconcile, or re-run with `dross ship --force` to overwrite origin's copy; "+
			"refusing to push over commits made elsewhere", branch, branch, pull)
	}
	opts := []string{"-u"}
	if force && d.Behind {
		// --force-with-lease guards against clobbering a push that landed
		// after the fetch above, without asking the user for the remote SHA.
		opts = append(opts, "--force-with-lease")
	}
	if out, err := gitCombined(repoDir, gitRefArgs("push", opts, "origin", branch)...); err != nil {
		return false, fmt.Errorf("git push origin %s: %w\n%s", branch, err, out)
	}
	return true, nil
}
