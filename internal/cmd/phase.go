package cmd

import (
	"encoding/json"
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
	"github.com/Rivil/dross/internal/project"
	"github.com/Rivil/dross/internal/ship"
	"github.com/Rivil/dross/internal/state"
	"github.com/Rivil/dross/internal/verify"
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
				branchBase, milestoneActive, err = forkPhaseBranch(repoDir, root, id, branchName)
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
	var baseFlag string
	c := &cobra.Command{
		Use:   "complete [phase-id]",
		Short: "Finalize a phase after squash-merge: ff the reconcile branch, delete phase/<id>",
		Long: `Run after the PR for this phase has been squash-merged upstream.

  1. fetch origin and confirm the phase's merge
  2. switch to the reconcile branch — milestone/<version> when a milestone
     is active, else the configured main branch
  3. fast-forward the reconcile branch from origin/<branch>
  4. delete the local phase/<id> branch (and the remote one)

The switch happens only after every check has passed, so a refused
completion leaves HEAD on phase/<id> with no local ref moved — re-run it
once the reason for the refusal is fixed.

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

The base is the branch this phase was forked from, read back from the
record its own changes.json carries — never inferred from the active
milestone, which picks a stale local branch the phase never forked from.
A phase with no recorded base refuses; pass --base <branch> to name it.

On an already-diverged base the fast-forward aborts. Re-run with
--recover to reset that base to origin and restore the cumulative
.dross/ tree in one shot (the same heal as 'dross ship recover'). It
works for whichever branch the record names — main or
milestone/<version> — and resets only that one. --recover is a
destructive reset of the local base branch; read the abort first.`,
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

			// Reconcile against the branch this phase was actually forked
			// from, read back from its phase-scoped record. Deriving it from
			// current_milestone instead — as this used to — silently picks a
			// stale milestone branch whenever one is sitting in the local repo,
			// and then fast-forwards it with work that was never merged there.
			// Resolved BEFORE anything mutating (the verify heal, the .dross
			// auto-commit, the fetch), so a refusal here changes nothing.
			reconcileBranch, err := resolveCompleteBase(repoDir, root, p, s, phaseID, baseFlag)
			if err != nil {
				return err
			}
			phaseBranch := "phase/" + phaseID

			// Heal-before-gate (mirrors ship's verify gate): a resolved
			// verdict that was never finalized gets its outcome event
			// recorded before completion proceeds. Runs before the branch
			// switch — the phase's verify.toml lives on phase/<id> — and
			// before autoCommitDrossDirt, which folds the marker write in.
			// A missing verify.toml or unresolved verdict is left alone:
			// complete doesn't gate on verify, and healing must never
			// invent a verdict.
			_, vToml := verify.FilePaths(root, phaseID)
			if v, verr := verify.LoadVerify(vToml); verr == nil && v != nil && !v.Verify.Finalized {
				switch v.Verify.Verdict {
				case "pass", "partial", "fail":
					recorded, verdict, herr := finalizeVerify(root, phaseID)
					if herr != nil {
						return fmt.Errorf("auto-finalize verify for %s: %w", phaseID, herr)
					}
					if recorded {
						Printf("auto-finalized verify verdict=%s (was resolved but unrecorded)\n", verdict)
					}
				}
			}

			// Working tree must be clean — checkout and branch -D both
			// behave better on a clean tree. Bookkeeping-only dirt under
			// .dross/ (a pause state-touch) is auto-committed and never
			// blocks; any real code dirt still refuses.
			committed, err := autoCommitDrossDirt(repoDir, "completing")
			if err != nil {
				return err
			}
			if committed {
				Printf("auto-committed .dross-only bookkeeping\n")
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

			if out, err := gitCombined(repoDir, "fetch", "origin"); err != nil {
				return fmt.Errorf("git fetch: %w\n%s", err, out)
			}

			// Safety net (c-2): .dross-only chores sitting unpushed on the
			// local base (pause auto-snapshot, gate auto-commits) re-seed
			// divergence at the next squash-merge. Complete already requires
			// network, so it absorbs the push; a code-ahead base or a failed
			// push is a hard refusal.
			basePushed, err := pushBaseIfAheadDrossOnly(repoDir, reconcileBranch)
			if err != nil {
				return err
			}
			if basePushed {
				Printf("pushed unpushed .dross chores on %s to origin\n", reconcileBranch)
			}
			quickPushed, quickBase, err := pushQuickBaseIfRecorded(repoDir, root, reconcileBranch)
			if err != nil {
				return err
			}
			if quickPushed {
				Printf("pushed unpushed .dross chores on %s (recorded quick_base) to origin\n", quickBase)
			}

			// Origin-side fallback for the recorded PR (c-3): post-squash-merge
			// the local working tree can be stale — ship committed the PR
			// record onto phase/<id> and the squash carried it to origin/<base>,
			// while the local read above saw a pre-record (or absent) file.
			// Now that origin is fetched, read the record from origin/<base>'s
			// tree so mergeGate can still take the authoritative provider path
			// instead of the ancestry fallback.
			if recordedPR == 0 {
				recordedPR = originRecordedPR(repoDir, reconcileBranch, phaseID)
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

			// Switch to the reconcile branch only now — every refusal above
			// (fetch failure, a code-ahead or unpushable base, an unmerged PR)
			// returns with HEAD still on phase/<id> and no local ref moved, so
			// a refused completion is a no-op the user can simply re-run.
			// Nothing above needs HEAD: fetch, pushBaseIfAheadDrossOnly,
			// originRecordedPR and mergeGate all take branch names. The
			// ff-only merge below is the first step that does.
			//
			// There is deliberately no restore-HEAD-on-error path past this
			// point: a compensating checkout is itself a branch switch, and one
			// that fails mid-refusal leaves a worse state than not trying.
			// Capture the phase branch's tip before leaving it. --recover
			// restores the .dross/ tree from THIS commit; taking it after the
			// checkout would source the tree from the branch the command just
			// moved to, which is the branch being reset.
			phaseTipSHA, _ := gitTrim(repoDir, "rev-parse", "refs/heads/"+phaseBranch)

			cur, err := gitTrim(repoDir, "symbolic-ref", "--short", "HEAD")
			if err != nil {
				return fmt.Errorf("git symbolic-ref failed (read current branch): %w", err)
			}
			if cur != reconcileBranch {
				if out, err := gitCombined(repoDir, "checkout", reconcileBranch); err != nil {
					return fmt.Errorf("git checkout %s: %w\n%s", reconcileBranch, err, out)
				}
			}

			if out, err := gitCombined(repoDir, "merge", "--ff-only", "origin/"+reconcileBranch); err != nil {
				// The ff abort IS the divergence signal: local <branch> holds
				// commits origin/<branch> doesn't. The clean-tree guard above
				// already ran, so no uncommitted work is at risk. The merge
				// gate above already verified the PR is merged, so recovery
				// (a destructive reset) never runs on an unverified merge.
				//
				// Without --recover, refuse and point at the fix, changing
				// nothing destructive.
				if !recoverFlag {
					return fmt.Errorf("fast-forward of %s from origin failed — local %s has diverged.\n%s\n"+
						"Re-run `dross phase complete --recover` to reset %s to origin and restore .dross/ "+
						"(or use `dross ship recover`). Recovery is a destructive reset of local %s — read the abort first.",
						reconcileBranch, reconcileBranch, out, reconcileBranch, reconcileBranch)
				}
				// --recover: the heal is a tree-from-phase-tip,
				// state.json-from-base split.
				//
				// state comes from the base: reload it from the (now
				// checked-out) base working tree so the recovery commit
				// carries the base's cumulative .dross/ state, not the phase
				// branch's stale copy loaded at the top of this RunE.
				// runDrossRecovery's own s.Save(rs) writes that copy last, so
				// it wins over whatever the tree restore put there.
				//
				// The tree comes from the phase tip: passing phaseTipSHA makes
				// `checkout <sha> -- .dross/` restore the phase branch's
				// .dross/ — the only place this phase's own records live.
				// Passing "" would default to HEAD, which is the branch we just
				// checked out and are about to reset, so the phase's records
				// would simply be absent from the restore.
				//
				// The reset target is the RECORDED base, not an inferred one:
				// resetting a stale milestone branch a phase never forked from
				// is the incident this phase exists to prevent.
				rs, lerr := state.Load(filepath.Join(root, state.File))
				if lerr != nil {
					return fmt.Errorf("reload state for recovery: %w", lerr)
				}
				if rerr := runDrossRecovery(repoDir, root, rs, phaseID, phaseTipSHA, reconcileBranch); rerr != nil {
					return fmt.Errorf("recover diverged %s during complete: %w", reconcileBranch, rerr)
				}
				// Healed: base reset to origin with .dross/ restored. Fall
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
		"on a diverged base (main or milestone/<version>), reset to origin and restore .dross/ in one shot instead of aborting")
	c.Flags().StringVar(&baseFlag, "base", "",
		"the branch this phase was forked from, when no base is recorded (a pre-check phase, or one created with --no-branch)")
	return c
}

// resolveCompleteBase answers "which branch was this phase forked from?" for
// `dross phase complete`, in priority order:
//
//  1. an explicit --base, validated against the local refs;
//  2. the base recorded in the phase's changes.json in the working tree;
//  3. the same record read off refs/heads/phase/<id>, for the post-squash
//     state where the working tree predates the record;
//  4. a refusal.
//
// There is deliberately no inference step. The milestone-derived base that
// used to sit here is exactly what made a stale local milestone/<version>
// branch the reconcile target for a phase forked off main — so when there is
// no record, the answer is a refusal naming --base, not a guess. A branch the
// user types is a conscious act; an inferred one is the bug.
func resolveCompleteBase(repoDir, root string, p *project.Project, s *state.State, phaseID, baseFlag string) (string, error) {
	if baseFlag != "" {
		if err := gitNoOut(repoDir, "rev-parse", "--verify", "refs/heads/"+baseFlag); err != nil {
			return "", fmt.Errorf("--base %s: no such local branch", baseFlag)
		}
		return baseFlag, nil
	}
	if ch, err := changes.Load(changes.FilePath(root, phaseID), phaseID); err == nil && ch.Base != "" {
		return ch.Base, nil
	}
	if base := phaseRefRecordedBase(repoDir, phaseID); base != "" {
		return base, nil
	}
	return "", fmt.Errorf("phase %q has no recorded forked-from base — refusing to guess one.\n"+
		"Completion reconciles against the branch the phase was forked from; inferring it from the "+
		"active milestone is what lets a stale local branch become the target.\n"+
		"Candidates here: %s.\n"+
		"Re-run naming the branch this phase was forked from, e.g. `dross phase complete %s --base %s`.\n"+
		"(Phases created before this check, and phases created with --no-branch, carry no record — "+
		"--base is the permanent answer for them.)",
		phaseID, strings.Join(completeBaseCandidates(repoDir, p, s), ", "), phaseID, p.Repo.GitMainBranch)
}

// phaseRefRecordedBase reads the recorded base off refs/heads/phase/<id>'s
// committed tree. It covers the post-squash-merge state where the local
// working tree predates ship's record commit but the phase branch carries it.
// Best-effort: any git or parse failure yields "" and the caller refuses.
func phaseRefRecordedBase(repoDir, phaseID string) string {
	ref := "refs/heads/phase/" + phaseID + ":.dross/phases/" + phaseID + "/" + changes.File
	out, err := exec.Command("git", "-C", repoDir, "show", ref).Output()
	if err != nil {
		return ""
	}
	var ch changes.Changes
	if err := json.Unmarshal(out, &ch); err != nil {
		return ""
	}
	return ch.Base
}

// completeBaseCandidates lists the branches a base-less phase plausibly forked
// from — the configured main branch, plus the active milestone's integration
// branch when its ref exists. Naming them turns the refusal into something the
// user can act on without going and reading the reflog.
func completeBaseCandidates(repoDir string, p *project.Project, s *state.State) []string {
	cands := []string{p.Repo.GitMainBranch}
	if s.CurrentMilestone != "" {
		ms := "milestone/" + s.CurrentMilestone
		if gitNoOut(repoDir, "rev-parse", "--verify", "refs/heads/"+ms) == nil {
			cands = append(cands, ms)
		}
	}
	return cands
}

// originRecordedPR reads the phase's recorded PR number out of
// origin/<base>'s committed tree (post-fetch). It exists for the stale-tree
// completion state: the squash-merge landed .dross/phases/<id>/changes.json —
// PR number included — on origin/<base>, but the local checkout predates the
// record so the working-tree read comes up empty. Best-effort: any git or
// parse failure (ref missing, file absent on that ref, malformed JSON) returns
// 0 and the caller's ancestry fallback stands.
func originRecordedPR(repoDir, base, phaseID string) int {
	ref := "origin/" + base + ":" + ".dross/phases/" + phaseID + "/changes.json"
	out, err := exec.Command("git", "-C", repoDir, "show", ref).Output()
	if err != nil {
		return 0
	}
	var ch changes.Changes
	if err := json.Unmarshal(out, &ch); err != nil {
		return 0
	}
	return ch.PR
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
// anywhere. Side effects are the checkout plus at most one chore(dross)
// auto-commit of .dross-only dirt (autoCommitDrossDirt); the caller owns
// directory creation and rollback. Returns the resolved base and whether a
// milestone branch was used (so create can tailor the no-milestone nudge).
//
// It also records the base it ACTUALLY forked from into the phase's
// changes.json — the create-time half of the base_write_timing decision, so
// every phase carries a base immediately, including one that never ships.
// phaseID is passed in rather than derived by trimming "phase/" off
// branchName: the id is what the record is keyed by, and reconstructing it
// from a branch name would silently mis-key any phase whose branch name
// stopped matching.
func forkPhaseBranch(repoDir, root, phaseID, branchName string) (base string, milestoneActive bool, err error) {
	committed, err := autoCommitDrossDirt(repoDir, "starting a phase")
	if err != nil {
		return "", false, err
	}
	if committed {
		Printf("auto-committed .dross-only bookkeeping\n")
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
	// Only after the fork succeeded: a failed checkout must leave no
	// changes.json behind, because the caller's rollback is os.Remove(dir),
	// which only removes an empty directory — a record written up front would
	// leak the phase id on every retry.
	if err := changes.SetBase(root, phaseID, base); err != nil {
		return "", false, fmt.Errorf("record forked-from base for %s: %w", phaseID, err)
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

// phaseDocument is the `phase show --json` payload: the phase's two documents
// under the keys they are named by on disk (locked json_shape). A missing file
// is JSON null — present and explicitly empty, which is a different statement
// from an absent key, and the same "(missing)" the toml rendering prints.
type phaseDocument struct {
	Spec *phase.Spec `json:"spec"`
	Plan *phase.Plan `json:"plan"`
}

func phaseShow() *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "show <phase-id>",
		Short: "Print the spec.toml and plan.toml for a phase",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			root, err := FindRoot()
			if err != nil {
				return err
			}
			dir := phase.Dir(root, args[0])
			// Ahead of both renderings: an unknown id used to exit 0 after
			// printing two "(missing)" lines, which reads as "this phase has
			// no spec yet" rather than "there is no such phase".
			if !isDir(dir) {
				return fmt.Errorf("unknown phase %q — no directory at %s (run `dross phase list` to see ids)", args[0], dir)
			}
			if asJSON {
				// LoadSpec / LoadPlan rather than the raw bytes: JSON has to be
				// the decoded document. Anything the structs do not model is
				// therefore visible in the toml rendering and absent here,
				// which the round-trip test in json_show_phase_test.go gates.
				doc := phaseDocument{}
				if spec, err := phase.LoadSpec(filepath.Join(dir, "spec.toml")); err == nil {
					doc.Spec = spec
				}
				if plan, err := phase.LoadPlan(filepath.Join(dir, "plan.toml")); err == nil {
					doc.Plan = plan
				}
				return emitJSON(doc)
			}
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
	c.Flags().BoolVar(&asJSON, "json", false, jsonFlagUsage)
	return c
}
