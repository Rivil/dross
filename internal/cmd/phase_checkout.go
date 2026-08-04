package cmd

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"
)

// phaseCheckout is the guarded replacement for a raw `git checkout phase/<id>`
// in prompts and by hand. Every branch switch dross performs goes through
// checkoutBranch so guardLiveState runs first (see switchbranch.go); a bare git
// checkout of a branch that still tracks .dross/state.json silently replays it
// over the live machine-local copy, because git overwrites an *ignored*
// working-tree file without complaint. Leaving one raw checkout in a prompt
// leaves one hole, and it only has to be walked through once.
//
// It refuses when the local ref is absent rather than creating it. `checkout -b`
// on a typo'd id would fork a branch off wherever HEAD happens to be and report
// success — the phase's real work would still be sitting on the branch the user
// meant. `dross phase create` is the only thing that makes phase branches.
func phaseCheckout() *cobra.Command {
	return &cobra.Command{
		Use:   "checkout <phase-id>",
		Short: "Switch to phase/<id>, guarding the live state.json",
		Long: `Switch to a phase's branch through dross's guarded checkout.

Identical to 'git checkout phase/<id>' except that it refuses when the target
branch carries a tracked copy of .dross/state.json that would overwrite the
live machine-local one — a branch cut before that file was untracked.

Refuses when phase/<id> does not exist locally; it never creates the branch.`,
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			root, err := FindRoot()
			if err != nil {
				return err
			}
			repoDir := filepath.Dir(root)
			// Validated as the argument the user passed, not as the prefixed
			// branch: "phase/" already makes a leading dash unreachable, so a
			// check on the composed name would silently pass a payload through
			// and report it as a missing branch several git calls later.
			if err := validateGitRef("phase id argument", args[0]); err != nil {
				return err
			}
			branch := "phase/" + args[0]

			if gitNoOut(repoDir, gitRefArgs("rev-parse", []string{"--verify"}, "refs/heads/"+branch)...) != nil {
				return fmt.Errorf("no local branch %s — `dross phase checkout` never creates one.\n"+
					"Check the phase id (`dross phase list`), or run `dross phase create` if the phase is new", branch)
			}
			if err := checkoutBranch(repoDir, branch); err != nil {
				return err
			}
			Printf("checked out %s\n", branch)
			return nil
		},
	}
}

// Checkout is the general-purpose sibling of phaseCheckout: the same guarded
// switch for a branch that is not a phase branch. `dross phase checkout` cannot
// serve those callers — it prefixes "phase/" — and every narration that had to
// name a non-phase target was therefore still handing the user the raw verb.
// `dross milestone prune`'s refusal ("switch to main first") was the last one,
// and a refusal that hands over `git checkout` reopens by hand the hole the
// guard exists to close.
//
// It refuses a missing local ref for the same reason phaseCheckout does: git
// would happily resolve `main` to `origin/main` and create a local branch, which
// is a different thing from switching to the branch the user named.
func Checkout() *cobra.Command {
	return &cobra.Command{
		Use:   "checkout <branch>",
		Short: "Switch to a branch, guarding the live state.json",
		Long: `Switch to any local branch through dross's guarded checkout.

Identical to 'git checkout <branch>' except that it refuses when the target
branch carries a tracked copy of .dross/state.json that would overwrite the
live machine-local one — a branch cut before that file was untracked.

Refuses when the branch does not exist locally; it never creates one. Use
'dross phase checkout <id>' for phase branches, which takes the phase id.`,
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			root, err := FindRoot()
			if err != nil {
				return err
			}
			repoDir := filepath.Dir(root)
			branch := args[0]
			// Unprefixed here, so the argument reaches git as a bare ref.
			if err := validateGitRef("branch argument", branch); err != nil {
				return err
			}

			if gitNoOut(repoDir, gitRefArgs("rev-parse", []string{"--verify"}, "refs/heads/"+branch)...) != nil {
				return fmt.Errorf("no local branch %s — `dross checkout` never creates one.\n"+
					"Check the name (`git branch --list`), or create it with git first", branch)
			}
			if err := checkoutBranch(repoDir, branch); err != nil {
				return err
			}
			Printf("checked out %s\n", branch)
			return nil
		},
	}
}
