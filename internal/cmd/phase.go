package cmd

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Rivil/dross/internal/changes"
	"github.com/Rivil/dross/internal/milestone"
	"github.com/Rivil/dross/internal/phase"
	"github.com/Rivil/dross/internal/ship"
	"github.com/Rivil/dross/internal/state"
)

func Phase() *cobra.Command {
	c := &cobra.Command{
		Use:   "phase",
		Short: "Manage phase directories under .dross/phases/",
	}
	c.AddCommand(phaseList(), phaseCreate(), phaseShow(), phaseComplete(), phaseNumber(), phaseMigrate(), phaseMove(), phaseInsert(), phaseRename())
	return c
}

// phaseNumber prints the 1-based position of a phase within the current
// milestone's phases array — the single source of truth for phase ordinals.
// This is the ordinal slash-command prompts use for the version patch digit,
// so it's derived from array position (and recomputes after a reorder) rather
// than counted from directory names. Prints 0 when there's no current
// milestone or the phase isn't in its array.
func phaseNumber() *cobra.Command {
	return &cobra.Command{
		Use:   "number <phase-id>",
		Short: "Print a phase's 1-based ordinal within its milestone",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			root, err := FindRoot()
			if err != nil {
				return err
			}
			s, err := state.Load(filepath.Join(root, state.File))
			if err != nil {
				return err
			}
			n := 0
			if s.CurrentMilestone != "" {
				if m, err := milestone.Load(milestone.FilePath(root, s.CurrentMilestone)); err == nil {
					n = phase.DisplayNumber(m.Phases, args[0])
				}
			}
			Printf("%d\n", n)
			return nil
		},
	}
}

func phaseList() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List phases",
		RunE: func(_ *cobra.Command, _ []string) error {
			root, err := FindRoot()
			if err != nil {
				return err
			}
			ids, err := phase.List(root)
			if err != nil {
				return err
			}
			if len(ids) == 0 {
				Print("(no phases)")
				return nil
			}
			for _, id := range phase.Ordered(milestonePhaseOrder(root), ids) {
				Print(id)
			}
			return nil
		},
	}
}

// milestonePhaseOrder concatenates every milestone's phases array in
// version order, producing the canonical phase sequence the milestones
// define. phase.Ordered uses it to order the listing; phases in no array
// (orphans) sort after it. A milestone that fails to load is skipped — a
// best-effort ordering hint, never a hard dependency for `phase list`.
func milestonePhaseOrder(root string) []string {
	versions, err := milestone.List(root)
	if err != nil {
		return nil
	}
	var order []string
	for _, v := range versions {
		m, err := milestone.Load(milestone.FilePath(root, v))
		if err != nil {
			continue
		}
		order = append(order, m.Phases...)
	}
	return order
}

// phaseCreate makes the directory NN-slug and (when the repo has git
// and --no-branch isn't passed) switches to a phase/<id> branch so all
// phase work commits land off main. Keeping phase work off main means
// the squash-merge on origin doesn't diverge from local main — the
// reason every prior ship needed an explicit reconcile commit.
//
// Spec/plan are written by /dross-spec and /dross-plan slash commands.
func phaseCreate() *cobra.Command {
	var noBranch bool
	c := &cobra.Command{
		Use:   "create <title>",
		Short: "Create the next phase directory and switch to phase/<id>",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			title := strings.Join(args, " ")
			root, err := FindRoot()
			if err != nil {
				return err
			}
			repoDir := filepath.Dir(root)

			id := phase.UniqueSlug(root, title)
			branchName := "phase/" + id

			hasGit := isDir(filepath.Join(repoDir, ".git"))

			dir := phase.Dir(root, id)
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return err
			}

			var branchBase string
			var milestoneActive bool
			if hasGit && !noBranch {
				// Fork phase/<id> off the resolved new-work base
				// (milestone/<version> when active, else main). On any
				// failure roll back the empty dir so a retry doesn't leak
				// a phase number.
				branchBase, milestoneActive, err = forkPhaseBranch(repoDir, root, branchName)
				if err != nil {
					_ = os.Remove(dir)
					return err
				}
				Printf("created %s\n", dir)
				Printf("checked out %s (rooted on %s)\n", branchName, branchBase)
			} else {
				Printf("created %s\n", dir)
				if !hasGit {
					Print("(no .git/ found — skipping phase branch creation)")
				}
			}

			// Mark this as the active phase so downstream commands can
			// resolve "no args = current phase" cleanly. Done after the
			// branch op so a failed checkout doesn't leave state pointing
			// at a phase whose branch wasn't created.
			s, err := state.Load(filepath.Join(root, state.File))
			if err != nil {
				return err
			}
			s.CurrentPhase = id
			s.CurrentPhaseStatus = "created"
			s.Touch(fmt.Sprintf("created %s", id))
			if err := s.Save(filepath.Join(root, state.File)); err != nil {
				return fmt.Errorf("save state: %w", err)
			}

			// Register the phase in the current milestone's ordered phases
			// array — that array is the single source of phase order, so a new
			// phase joins it at the tail. appendUnique keeps this idempotent
			// when /dross-spec --new scaffolds a phase the milestone already
			// listed as intent.
			ordinal := 0
			if s.CurrentMilestone != "" {
				mPath := milestone.FilePath(root, s.CurrentMilestone)
				if m, err := milestone.Load(mPath); err == nil {
					m.Phases = appendUnique(m.Phases, id)
					if err := m.Save(mPath); err != nil {
						return fmt.Errorf("register phase in milestone %s: %w", s.CurrentMilestone, err)
					}
					ordinal = phase.DisplayNumber(m.Phases, id)
				}
			}

			// Nudge (never require) the user to scope a milestone when there's
			// none active and we fell back to main — the no_milestone_fallback
			// locked decision. Silent in the cutover case (a milestone is set
			// but predates the branch model), where milestoneActive is false
			// yet CurrentMilestone is not empty.
			if hasGit && !noBranch && !milestoneActive && s.CurrentMilestone == "" {
				Printf("no milestone active — rooted on %s; scope one with `dross milestone <version>` for integration branching\n", branchBase)
			}

			Print("Next: /dross-spec to write spec.toml, then /dross-plan")
			RecordOutcomeEvent("phase_create", map[string]int{"ordinal": ordinal}, nil, nil)
			return nil
		},
	}
	c.Flags().BoolVar(&noBranch, "no-branch", false,
		"skip creating/checking out the phase/<id> git branch (advanced)")
	return c
}

// phaseComplete finalizes a phase after its PR has been squash-merged
// upstream. It switches back to main, fast-forwards from origin, deletes
// the local phase branch, and clears state.CurrentPhase with an audit
// entry. This is the post-merge counterpart to `dross phase create` —
// together they keep phase work fully off main.
func phaseComplete() *cobra.Command {
	var recoverFlag bool
	c := &cobra.Command{
		Use:   "complete [phase-id]",
		Short: "Finalize a phase after squash-merge: ff the reconcile branch, delete phase/<id>",
		Long: `Run after the PR for this phase has been squash-merged upstream.

  1. switch to the reconcile branch — milestone/<version> when a milestone
     is active, else the configured main branch
  2. fetch origin
  3. fast-forward the reconcile branch from origin/<branch>
  4. delete the local phase/<id> branch (and the remote one)

'dross ship' folds the cleared current_phase + "completed <id>" record
into the PR squash, so the fast-forward above already brings the
completion onto the reconcile branch — complete writes no commit of its
own. This is what eliminates the completion-chore divergence.

Refuses on a dirty tree, or unless the phase's merge is authoritatively
confirmed. The gate is the provider's "is PR #N merged?" status, looked
up via the PR number ship recorded in the phase's changes.json — the
"completed <id>" history string is only a corroborating hint, never the
gate, because it rides forward in cumulative history and a later merged
phase can drag it onto the base. When no PR number is recorded or the
provider can't answer, complete falls back to a git-ancestry check
(merge-base --is-ancestor) and refuses-when-inconclusive rather than
false-completing.

On an already-diverged branch the fast-forward aborts. For main, re-run
with --recover to reset it to origin and restore the cumulative .dross/
tree in one shot (the same heal as 'dross ship recover'); --recover is a
destructive reset of local main. Under a milestone, --recover is not yet
supported, so a diverged milestone branch aborts non-destructively and
points at a manual reconcile.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			root, err := FindRoot()
			if err != nil {
				return err
			}
			repoDir := filepath.Dir(root)
			p, _, err := loadProject()
			if err != nil {
				return err
			}
			s, err := state.Load(filepath.Join(root, state.File))
			if err != nil {
				return err
			}

			// Resolve the phase id. `dross ship` now folds the completion
			// record into the squash and clears current_phase, so the
			// post-ship state can't supply it — fall back to the phase
			// branch we're sitting on.
			phaseID := ""
			switch {
			case len(args) == 1:
				phaseID = args[0]
			case s.CurrentPhase != "":
				phaseID = s.CurrentPhase
			default:
				if cur, err := gitTrim(repoDir, "symbolic-ref", "--short", "HEAD"); err == nil {
					if rest, ok := strings.CutPrefix(cur, "phase/"); ok {
						phaseID = rest
					}
				}
			}
			if phaseID == "" {
				return errors.New("no phase id given, state has no current_phase, and not on a phase/<id> branch")
			}

			// Reconcile against the active milestone's integration branch
			// (milestone/<version>) when one exists, else main — the same base
			// resolver create/ship use. Under a milestone, complete ff's the
			// milestone branch so main only advances at the milestone boundary.
			reconcileBranch, milestoneActive, err := resolveNewWorkBase(repoDir, root)
			if err != nil {
				return err
			}
			phaseBranch := "phase/" + phaseID

			// Working tree must be clean — checkout and branch -D both
			// behave better on a clean tree, and a dirty one usually
			// signals the user hasn't finished the previous step.
			status, err := gitTrim(repoDir, "status", "--porcelain")
			if err != nil {
				return fmt.Errorf("git status: %w", err)
			}
			if status != "" {
				return dirtyTreeError("completing", status)
			}

			// Read THIS phase's recorded PR number from its phase-scoped
			// changes.json BEFORE switching branches — the reconcile branch
			// may not carry the phase's changes.json, and the record is
			// drag-proof (unlike the "completed <id>" breadcrumb in cumulative
			// state history). ship writes and commits it onto phase/<id>
			// post-push. A missing/empty file is fine (recordedPR stays 0 → the
			// gate falls back to git ancestry).
			recordedPR := 0
			if ch, cerr := changes.Load(changes.FilePath(root, phaseID), phaseID); cerr == nil {
				recordedPR = ch.PR
			}

			// Switch to the reconcile branch if we aren't already there.
			cur, err := gitTrim(repoDir, "symbolic-ref", "--short", "HEAD")
			if err != nil {
				return fmt.Errorf("git symbolic-ref failed (read current branch): %w", err)
			}
			if cur != reconcileBranch {
				if out, err := gitCombined(repoDir, "checkout", reconcileBranch); err != nil {
					return fmt.Errorf("git checkout %s: %w\n%s", reconcileBranch, err, out)
				}
			}

			if out, err := gitCombined(repoDir, "fetch", "origin"); err != nil {
				return fmt.Errorf("git fetch: %w\n%s", err, out)
			}

			// Authoritative merge gate. Prefer the provider's "is PR #N
			// merged?" status (via the recorded PR number): squash-merge
			// rewrites SHAs so git ancestry can't confirm a squash-merged
			// phase, and the "completed <id>" breadcrumb rides forward in
			// cumulative history so a later merged phase can drag it onto the
			// base — it can't be trusted as the gate. When we can't get an
			// authoritative answer (no PR number, provider can't answer, or an
			// error), fall back to a git-ancestry check and
			// refuse-when-inconclusive rather than false-complete. Runs before
			// anything destructive.
			if err := mergeGate(repoDir, buildOpenOpts(p), phaseID, phaseBranch, reconcileBranch, recordedPR); err != nil {
				return err
			}

			if out, err := gitCombined(repoDir, "merge", "--ff-only", "origin/"+reconcileBranch); err != nil {
				// The ff abort IS the divergence signal: local <branch> holds
				// commits origin/<branch> doesn't. The clean-tree guard above
				// already ran, so no uncommitted work is at risk.
				if milestoneActive {
					// --recover's reconcile branch is still hardcoded to main
					// (deferred), so it must NOT be pointed at a milestone
					// branch — that would reset the wrong branch. Abort
					// non-destructively and steer to a manual reconcile.
					return fmt.Errorf("fast-forward of %s from origin failed — local %s has diverged.\n%s\n"+
						"Reconcile it manually (save any local commits, then `git reset --hard origin/%s`). "+
						"`--recover` does not yet support milestone branches, so nothing was reset.",
						reconcileBranch, reconcileBranch, out, reconcileBranch)
				}
				// main path. Without --recover, refuse and point at the fix,
				// changing nothing destructive.
				if !recoverFlag {
					return fmt.Errorf("fast-forward of %s from origin failed — local %s has diverged.\n%s\n"+
						"Re-run `dross phase complete --recover` to reset %s to origin and restore .dross/ "+
						"(or use `dross ship recover`). Recovery is a destructive reset of local %s — read the abort first.",
						reconcileBranch, reconcileBranch, out, reconcileBranch, reconcileBranch)
				}
				// --recover: reload state from the (now checked-out) main
				// working tree so the recovery commit carries main's .dross/
				// state, not the phase branch's stale copy loaded at the top of
				// this RunE. Then delegate to the shared routine — the same heal
				// `dross ship recover` runs — which resets to origin and restores
				// the cumulative .dross/ tree in one shot.
				rs, lerr := state.Load(filepath.Join(root, state.File))
				if lerr != nil {
					return fmt.Errorf("reload state for recovery: %w", lerr)
				}
				if rerr := runDrossRecovery(repoDir, root, p, rs, phaseID, ""); rerr != nil {
					return fmt.Errorf("recover diverged %s during complete: %w", reconcileBranch, rerr)
				}
				// Healed: main reset to origin with .dross/ restored. Fall
				// through to the branch teardown below.
			}

			// Delete the local phase branch (best-effort: only if it exists).
			if err := gitNoOut(repoDir, "rev-parse", "--verify", "refs/heads/"+phaseBranch); err == nil {
				if out, err := gitCombined(repoDir, "branch", "-D", phaseBranch); err != nil {
					return fmt.Errorf("git branch -D %s: %w\n%s", phaseBranch, err, out)
				}
			}

			// Delete the remote phase branch too, so completing a phase
			// leaves nothing behind on origin. Idempotent: the provider's
			// PR --delete-branch may have already removed it (or it was
			// never pushed), so we only push --delete when the ref still
			// exists. ls-remote queries origin directly rather than trusting
			// possibly-stale remote-tracking refs left by the earlier fetch.
			remoteRef, err := gitTrim(repoDir, "ls-remote", "--heads", "origin", phaseBranch)
			if err != nil {
				return fmt.Errorf("git ls-remote origin %s: %w", phaseBranch, err)
			}
			if remoteRef != "" {
				if out, err := gitCombined(repoDir, "push", "origin", "--delete", phaseBranch); err != nil {
					return fmt.Errorf("git push origin --delete %s: %w\n%s", phaseBranch, err, out)
				}
			}

			// No state write here: `dross ship` already folded the cleared
			// current_phase + "completed <id>" record into the squash, and
			// the ff above brought it onto local main. Writing (and
			// committing) it again is exactly the standalone unpushed commit
			// that used to re-seed main divergence on every completion.

			RecordOutcomeEvent("phase_complete",
				map[string]int{},
				nil,
				map[string]string{"result": "completed"},
			)
			Printf("completed %s — %s is at origin, phase/%s deleted\n", phaseID, reconcileBranch, phaseID)
			return nil
		},
	}
	c.Flags().BoolVar(&recoverFlag, "recover", false,
		"on a diverged main, reset to origin and restore .dross/ in one shot instead of aborting")
	return c
}

// mergeGate is the authoritative completion gate for `dross phase complete`.
// Primary signal: the provider's "is PR #N merged?" status, looked up via the
// phase-recorded PR number (opts carries the provider/url wiring). When a PR
// number is recorded and the provider answers, that answer is decisive —
// merged proceeds, unmerged refuses. When there is no recorded PR, or the
// provider can't answer (ErrMergeStatusUnsupported) or errors, it falls back to
// `git merge-base --is-ancestor origin/phase/<id> origin/<base>`: a git error
// (ref missing — squash-deleted) AND a false ancestry result BOTH map to the
// same guided refusal. It never trusts the "completed <id>" breadcrumb, never
// false-completes, and never crashes offline.
func mergeGate(repoDir string, opts ship.OpenOpts, phaseID, phaseBranch, reconcileBranch string, recordedPR int) error {
	if recordedPR > 0 {
		opts.PRNumber = recordedPR
		merged, err := ship.PRMergedFunc(opts)
		switch {
		case err == nil && merged:
			return nil // authoritatively merged — proceed
		case err == nil && !merged:
			return fmt.Errorf("PR #%d for %s is not merged upstream — refusing to complete so the phase branch isn't lost.\n"+
				"Merge the PR first and re-run; or if it really merged, use `dross phase complete --recover` / verify the merge manually.",
				recordedPR, phaseID)
		case errors.Is(err, ship.ErrMergeStatusUnsupported):
			// Provider can't answer yet — fall through to the ancestry fallback.
		default:
			// Network/API error — fall through rather than block on a transient failure.
		}
	}
	// Fallback: git ancestry. A missing origin/phase/<id> ref (squash-deleted)
	// OR a non-ancestor result both mean "can't confirm the merge" — refuse
	// with guidance rather than trust the breadcrumb or false-complete.
	if err := gitNoOut(repoDir, "merge-base", "--is-ancestor", "origin/"+phaseBranch, "origin/"+reconcileBranch); err != nil {
		return fmt.Errorf("cannot confirm %s has merged into %s — no merged-PR status was available and origin/%s is not an ancestor of origin/%s "+
			"(the phase branch may have been squash-deleted, or the PR isn't merged yet).\n"+
			"Refusing so the phase branch isn't lost. If the PR really merged, use `dross phase complete --recover` or verify the merge manually.",
			phaseID, reconcileBranch, phaseBranch, reconcileBranch)
	}
	return nil
}

// forkPhaseBranch creates and checks out branchName rooted on the base returned
// by resolveNewWorkBase (milestone/<version> when its ref exists, else main).
// It keeps the clean-tree and no-existing-ref guards but — unlike the old
// preflight — does NOT require being on main first, because under the v0.7
// branch topology the base may be a milestone integration branch reached from
// anywhere. The checkout is the only side effect; the caller owns directory
// creation and rollback. Returns the resolved base and whether a milestone
// branch was used (so create can tailor the no-milestone nudge).
func forkPhaseBranch(repoDir, root, branchName string) (base string, milestoneActive bool, err error) {
	status, err := gitTrim(repoDir, "status", "--porcelain")
	if err != nil {
		return "", false, fmt.Errorf("git status: %w", err)
	}
	if status != "" {
		return "", false, dirtyTreeError("starting a phase", status)
	}
	if err := gitNoOut(repoDir, "rev-parse", "--verify", "refs/heads/"+branchName); err == nil {
		return "", false, fmt.Errorf("branch %s already exists locally; delete it first or pass --no-branch", branchName)
	}
	base, milestoneActive, err = resolveNewWorkBase(repoDir, root)
	if err != nil {
		return "", false, err
	}
	if out, e := gitCombined(repoDir, "checkout", "-b", branchName, base); e != nil {
		return "", false, fmt.Errorf("git checkout -b %s %s: %w\n%s", branchName, base, e, out)
	}
	return base, milestoneActive, nil
}

// dirtyTreeError builds an actionable dirty-tree error. It keeps the
// "working tree is dirty" prefix (so telemetry still buckets it as
// dirty_tree) and appends the offending paths from `git status
// --porcelain`, so the caller sees exactly what to commit or stash
// instead of re-running git status to find out.
func dirtyTreeError(action, status string) error {
	lines := strings.Split(strings.TrimRight(status, "\n"), "\n")
	for i, l := range lines {
		lines[i] = "  " + l
	}
	return fmt.Errorf("working tree is dirty; commit or stash before %s:\n%s",
		action, strings.Join(lines, "\n"))
}

// gitNoOut runs git silently, discarding output. Used when only the
// exit status matters (e.g. ref-exists probes).
func gitNoOut(repoDir string, args ...string) error {
	full := append([]string{"-C", repoDir}, args...)
	return exec.Command("git", full...).Run()
}

// isDir reports whether path exists and is a directory.
func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func phaseShow() *cobra.Command {
	return &cobra.Command{
		Use:   "show <phase-id>",
		Short: "Print the spec.toml and plan.toml for a phase",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			root, err := FindRoot()
			if err != nil {
				return err
			}
			dir := phase.Dir(root, args[0])
			for _, name := range []string{"spec.toml", "plan.toml"} {
				path := filepath.Join(dir, name)
				b, err := os.ReadFile(path)
				if err != nil {
					Printf("# %s — (missing)\n\n", path)
					continue
				}
				Printf("# %s\n%s\n", path, string(b))
			}
			return nil
		},
	}
}
