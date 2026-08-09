package cmd

import (
	"fmt"
	"strings"

	"github.com/Rivil/dross/internal/verify"
)

// phaseScope derives the verify.Scope for a phase: what git says the phase
// changed, unioned with what changes.json recorded.
//
// It never returns an error. Every git step that can fail degrades to the
// changes.json side with the reason recorded on the Scope, because the
// alternative — aborting verify because a base ref went missing — turns a
// bookkeeping gap into a blocked phase. What must never happen is the quiet
// version of the same thing: a scope that silently narrowed and produced a
// clean-looking pass. That is what Scope.Degraded exists to make visible.
//
// The diff is taken between the resolved merge-base and HEAD, not the working
// tree. A scope that shifted with unsaved edits could not be reproduced from
// the recorded base and the commit history, and c-6 is exactly the requirement
// that a mis-scoped run stays diagnosable after the fact.
func phaseScope(repoDir, base string, recorded []string) *verify.Scope {
	in := verify.ScopeInput{
		Root:     repoDir,
		Recorded: recorded,
	}

	if strings.TrimSpace(base) == "" {
		in.Degraded = append(in.Degraded,
			"changes.json records no base branch, so no git diff could be taken")
		return verify.NewScope(in)
	}

	sha, err := gitTrim(repoDir, gitRefArgs("merge-base", nil, base, "HEAD")...)
	if err != nil || sha == "" {
		in.Degraded = append(in.Degraded,
			fmt.Sprintf("could not resolve merge-base of %q and HEAD: %v", base, gitReason(err)))
		return verify.NewScope(in)
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
		return verify.NewScope(in)
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
		return verify.NewScope(in)
	}
	hunks, degraded := verify.ParseHunks(patch)
	in.Hunks = hunks
	in.Degraded = append(in.Degraded, degraded...)

	return verify.NewScope(in)
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
