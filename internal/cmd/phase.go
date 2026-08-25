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
	"github.com/Rivil/dross/internal/hostallow"
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
	c.AddCommand(phaseList(), phaseCreate(), phaseCheckout(), phaseShow(), phaseComplete(), phaseReconcile(), phaseNumber(), phaseMigrate(), phaseMove(), phaseInsert(), phaseRename(), phaseRedProof(), phaseBackfill())
	return c
}

// historyHasAction reports whether state history already carries an entry
// containing action. Both lifecycle breadcrumbs — ship's `shipped <id>` and
// complete's `completed <id>` — are guarded by it, so re-running either
// command over the same phase re-asserts the state without appending a second
// row. History is cumulative and capped at 50 entries; a doubled breadcrumb
// costs a real one.
func historyHasAction(s *state.State, action string) bool {
	for _, a := range s.History {
		if strings.Contains(a.Action, action) {
			return true
		}
	}
	return false
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
	var milestoneVersion string
	c := &cobra.Command{
		Use:   "list",
		Short: "List phases, marking the done ones",
		RunE: func(_ *cobra.Command, _ []string) error {
			root, err := FindRoot()
			if err != nil {
				return err
			}
			// One doneness reader for the whole tool (phasedone.go): this
			// listing, `dross status` and `dross milestone progress` count the
			// same phases done, off the completion record rather than a verify
			// verdict.
			if milestoneVersion != "" {
				return listMilestoneRoadmap(root, milestoneVersion)
			}
			ids, err := phase.List(root)
			if err != nil {
				return err
			}
			if len(ids) == 0 {
				Print("(no phases)")
				return nil
			}
			done := 0
			for _, id := range phase.Ordered(milestonePhaseOrder(root), ids) {
				if phaseDone(root, id) {
					done++
					Printf("✓ %s\n", id)
				} else {
					Printf("  %s\n", id)
				}
			}
			// The denominator is phase.List's directory count, which is unique
			// by construction — not the rendered line count, which comes
			// through an order array a slug can appear on twice.
			Printf("%d/%d done\n", done, len(ids))
			return nil
		},
	}
	c.Flags().StringVar(&milestoneVersion, "milestone", "",
		"list this milestone's roadmap in array order, including slugs with no phase directory")
	return c
}

// listMilestoneRoadmap renders one milestone's phases array in roadmap order.
//
// It is a different question from the bare listing, which is every phase
// directory in the repo: this one is "what did this milestone sign up for, and
// how much of it is done", so a slug on the array with no directory is listed
// too — it is outstanding work, not an absence — and the footer's denominator
// is the roadmap's length. Dropping the unscaffolded entries would report a
// half-built milestone as finished.
func listMilestoneRoadmap(root, version string) error {
	m, err := milestone.Load(milestone.FilePath(root, version))
	if err != nil {
		return fmt.Errorf("unknown milestone %q: %w (run `dross milestone list` to see options)", version, err)
	}
	if len(m.Phases) == 0 {
		Printf("(no phases on %s)\n", version)
		return nil
	}
	done := 0
	for _, slug := range m.Phases {
		switch {
		case phaseDone(root, slug):
			done++
			Printf("✓ %s\n", slug)
		case !phaseDirExists(root, slug):
			Printf("  %s (not scaffolded)\n", slug)
		default:
			Printf("  %s\n", slug)
		}
	}
	Printf("%d/%d done\n", done, len(m.Phases))
	return nil
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
	var adoptFlag bool
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

			id, disposition := phase.CreateSlug(root, title)
			branchName := "phase/" + id

			// Refused BEFORE anything is created, cut or written: a refusal
			// that had already made a directory or a branch would leave the
			// repo in a state neither outcome asked for, and the user would
			// have to clean up after a command that declined to act.
			if disposition == phase.SlugOccupied && !adoptFlag {
				return fmt.Errorf("phase %s already exists and has been started (it holds a spec, plan or recorded changes).\n"+
					"dross will not retitle work in flight, and it no longer invents %s-2 for you.\n\n"+
					"  work on it:      dross phase checkout %s\n"+
					"  rename it:       dross phase rename %s \"<new title>\"\n"+
					"  take it over:    dross phase create \"<title>\" --adopt\n"+
					"  or pick a title that does not slugify to %s",
					id, id, id, id, id)
			}
			// takingOver is the deliberate opt-in: the slug holds started work
			// and the user said so. It is a strictly wider action than adopting
			// an empty placeholder, so it inherits rename's guard — a phase
			// whose branch is live on the remote is somebody's open PR, and
			// taking it over here would be the way around a refusal rename
			// already makes.
			takingOver := disposition == phase.SlugOccupied
			if takingOver {
				if err := refuseIfShipped(repoDir, id); err != nil {
					return err
				}
			}
			adopted := disposition == phase.SlugAdopt || takingOver

			hasGit := isDir(filepath.Join(repoDir, ".git"))

			dir := phase.Dir(root, id)
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return err
			}

			var branchBase string
			var milestoneActive bool
			enteredExisting := false
			switch {
			case hasGit && !noBranch && takingOver && localBranchExists(repoDir, branchName):
				// The branch is where the adopted phase's commits live. Forking
				// would refuse (branch exists) and re-forking would abandon
				// them, so enter it instead. The recorded fork point is left
				// exactly as it is: it scopes the phase's mutation diff, and
				// rewriting it to today's tip would silently drop every commit
				// the phase already made out of its own scope.
				if err := checkoutBranch(repoDir, branchName); err != nil {
					return err
				}
				enteredExisting = true
				Printf("%s %s\n", createdOrAdopted(adopted), dir)
				Printf("checked out %s (existing branch; recorded fork point left as it was)\n", branchName)
			case hasGit && !noBranch:
				// Fork phase/<id> off the resolved new-work base
				// (milestone/<version> when active, else main). On any
				// failure roll back the empty dir so a retry doesn't leak
				// a phase number.
				branchBase, milestoneActive, err = forkPhaseBranch(repoDir, root, id, branchName)
				if err != nil {
					_ = os.Remove(dir)
					return err
				}
				Printf("%s %s\n", createdOrAdopted(adopted), dir)
				Printf("checked out %s (rooted on %s)\n", branchName, branchBase)
			default:
				Printf("%s %s\n", createdOrAdopted(adopted), dir)
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
			// "created" is a lie about a phase that already holds a plan, and
			// the status carried in state belongs to whatever phase was current
			// before — not to this one. Derive it from what the adopted phase
			// actually has on disk instead of preserving or inventing one.
			s.CurrentPhaseStatus = "created"
			action := fmt.Sprintf("created %s", id)
			if takingOver {
				if isFile(filepath.Join(dir, "plan.toml")) {
					s.CurrentPhaseStatus = "planned"
				}
				action = fmt.Sprintf("adopted %s", id)
			}
			s.Touch(action)
			if err := s.Save(filepath.Join(root, state.File)); err != nil {
				return fmt.Errorf("save state: %w", err)
			}

			// Register the phase in the current milestone's ordered phases
			// array — that array is the single source of phase order, so a new
			// phase joins it at the tail. appendUnique keeps this idempotent
			// when /dross-spec --new scaffolds a phase the milestone already
			// listed as intent, and it is what keeps an ADOPTED phase in the
			// slot the roadmap put it in: re-appending would move it to the
			// tail and renumber every phase between, silently re-ordering an
			// arrangement someone chose.
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
			if hasGit && !noBranch && !enteredExisting && !milestoneActive && s.CurrentMilestone == "" {
				Printf("no milestone active — rooted on %s; scope one with `dross milestone <version>` for integration branching\n", branchBase)
			}

			// Pointing an adopted phase at /dross-spec would send the user to
			// rewrite a spec it already has.
			switch {
			case takingOver && isFile(filepath.Join(dir, "plan.toml")):
				Print("Next: /dross-execute — this phase already has a plan")
			case takingOver && isFile(filepath.Join(dir, "spec.toml")):
				Print("Next: /dross-plan — this phase already has a spec")
			default:
				Print("Next: /dross-spec to write spec.toml, then /dross-plan")
			}
			RecordOutcomeEvent("phase_create", map[string]int{"ordinal": ordinal}, nil, nil)
			return nil
		},
	}
	c.Flags().BoolVar(&noBranch, "no-branch", false,
		"skip creating/checking out the phase/<id> git branch (advanced)")
	c.Flags().BoolVar(&adoptFlag, "adopt", false,
		"take over a slug that has already been started (refused without this)")
	return c
}

// localBranchExists reports whether refs/heads/<branch> is present. Adoption
// needs the answer before deciding to enter a branch rather than fork one;
// forkPhaseBranch asks the same question to refuse, which is the behaviour
// --adopt is opting out of.
func localBranchExists(repoDir, branch string) bool {
	return gitNoOut(repoDir, gitRefArgs("rev-parse", []string{"--verify"}, "refs/heads/"+branch)...) == nil
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
  4. write the completed-state transition
  5. delete the phase/<id> branch, locally and on origin

The switch happens only after every check has passed, so a refused
completion leaves HEAD on phase/<id> with no local ref moved — re-run it
once the reason for the refusal is fixed.

This command writes the completed-state transition — the cleared
current_phase plus a "completed <id>" history entry — into the
machine-local, gitignored .dross/state.json, and it is the sole writer of
it. 'dross ship' only marks the phase shipped and leaves current_phase
set: a phase is not complete until its PR is merged, and only this run
sits behind that gate. The write is machine-local, so it rides no commit
and no squash anywhere; complete creates no commit of its own, which is
what eliminates the completion-chore divergence. It lands before the
branch teardown, so a failed deletion still leaves the confirmed merge
recorded, and it is idempotent on a re-run.

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

			// Resolve the phase id. `dross ship` leaves current_phase set
			// (it only marks the phase shipped), so state normally supplies
			// it — but a phase completed from a fresh clone, or after the
			// state was cleared by hand, still resolves off the phase branch
			// we're sitting on.
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
			hosts, herr := remotePolicy(root, repoDir, p)
			if herr != nil {
				return herr
			}
			if err := mergeGate(repoDir, buildOpenOpts(p, hosts), phaseID, phaseBranch, reconcileBranch, recordedPR); err != nil {
				return err
			}

			// c-7's second half, read HERE — still on the phase branch, with
			// origin/phase/<id> still alive — because this is the last point
			// where the pin's own record is guaranteed to be in the working
			// tree and the doomed ref still exists to be judged against. A
			// WARNING, never a refusal: the PR is already merged, and leaving
			// a merged phase uncompletable over bookkeeping would push the
			// operator back to hand-editing the record.
			warnDoomedRedProofs(root, repoDir, phaseID)

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
			// --verify is not decoration here: bare `git rev-parse` ECHOES any
			// argument it does not recognise, so it prints "--end-of-options"
			// on its own line ahead of the sha and the caller reads the
			// separator as the answer. --verify makes it resolve-or-fail, which
			// is what this read wanted anyway.
			phaseTipSHA, _ := gitTrim(repoDir, gitRefArgs("rev-parse", []string{"--verify"}, "refs/heads/"+phaseBranch)...)

			cur, err := gitTrim(repoDir, "symbolic-ref", "--short", "HEAD")
			if err != nil {
				return fmt.Errorf("git symbolic-ref failed (read current branch): %w", err)
			}
			if cur != reconcileBranch {
				if err := checkoutBranch(repoDir, reconcileBranch); err != nil {
					return err
				}
			}

			if out, err := guardedFF(repoDir, "origin/"+reconcileBranch); err != nil {
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
				// --recover: the heal restores the tree from the phase tip and
				// leaves state.json alone.
				//
				// state is reloaded from disk here because the copy loaded at
				// the top of this RunE predates the checkout and the reset, and
				// a stale in-memory State would have its history written back
				// over the live file by runDrossRecovery's own s.Save.
				//
				// It is NOT sourced from the base working tree, and s.Save no
				// longer "wins over the tree restore": state.json is
				// machine-local and gitignored, so the base has no copy to
				// supply and the restore excludes the path entirely (t-6). The
				// live file simply persists across the whole operation.
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

			// Write the completion record: clear current_phase/status and
			// append one `completed <id>` history entry. This command is its
			// sole writer — `dross ship` only marks the phase shipped, because
			// a phase is not complete until its PR is merged and only this run
			// sits behind that gate.
			//
			// Written BEFORE the branch teardown, deliberately: the record
			// describes the confirmed merge, not the cleanup, so a teardown
			// that fails (a protected ref, a network drop on the remote
			// delete) must still leave the merge recorded rather than
			// discarding a fact that was already true.
			//
			// Re-loaded from disk rather than reusing the `s` from the top of
			// this RunE: that copy predates autoCommitDrossDirt, the checkout,
			// the fast-forward and runDrossRecovery — whose own Save appends
			// `merged <id>`. Saving the stale copy over it would truncate live
			// history, the exact clobber this command exists to avoid.
			//
			// No `git add`, no commit: state.json is machine-local and
			// gitignored, so the record rides no commit anywhere.
			cs, err := state.Load(filepath.Join(root, state.File))
			if err != nil {
				// The live file is gone: switching off a branch that tracked
				// it — the pre-untrack shape some repos still carry — takes it
				// with the checkout. Fall back to the copy loaded at the top of
				// this run. It is the last known-good history, so writing it
				// back restores the file git removed rather than leaving the
				// machine with no state at all. The reload still wins whenever
				// it succeeds: on the --recover path it is the only copy
				// carrying recovery's `merged <id>`.
				cs = s
			}
			// Only clear when THIS phase is the current one: completing an
			// older phase must not blank a different phase already in flight.
			if cs.CurrentPhase == phaseID {
				cs.CurrentPhase = ""
				cs.CurrentPhaseStatus = ""
			}
			// Idempotent: a re-run over an already-recorded phase appends
			// nothing, so the breadcrumb count stays at one.
			if !historyHasAction(cs, "completed "+phaseID) {
				cs.Touch(fmt.Sprintf("completed %s", phaseID))
			}
			if err := cs.Save(filepath.Join(root, state.File)); err != nil {
				return fmt.Errorf("save completion record: %w", err)
			}
			// The durable half of the same fact. The breadcrumb above scrolls
			// out of a 50-entry history; this one is phase-scoped and stays.
			//
			// Unlike state.json, changes.json is tracked, so the write has to
			// be committed here or complete would hand back a dirty tree and
			// the zero-manual-git contract would be a manual `git commit`. Same
			// auto-commit the entry gate at the top of this RunE uses; we are
			// on the base branch by now, so the record lands there.
			if err := changes.SetStatus(root, phaseID, changes.StatusComplete); err != nil {
				return fmt.Errorf("record phase status: %w", err)
			}
			if _, err := autoCommitDrossDirt(repoDir, "recording phase completion"); err != nil {
				return fmt.Errorf("commit completion record: %w", err)
			}
			// …and publish it, through the same .dross-only safety net that
			// handles unpushed chores on the way in. Complete's contract is
			// that the base ends level with origin; a local-only record commit
			// would leave it one ahead, which is the divergence the whole
			// reconcile path exists to prevent.
			if _, err := pushBaseIfAheadDrossOnly(repoDir, reconcileBranch); err != nil {
				return fmt.Errorf("publish completion record: %w", err)
			}

			// Delete the local phase branch (best-effort: only if it exists).
			localDeleted := false
			if err := gitNoOut(repoDir, gitRefArgs("rev-parse", []string{"--verify"}, "refs/heads/"+phaseBranch)...); err == nil {
				if out, err := gitCombined(repoDir, gitRefArgs("branch", []string{"-D"}, phaseBranch)...); err != nil {
					return fmt.Errorf("git branch -D %s: %w\n%s", phaseBranch, err, out)
				}
				localDeleted = true
			}

			// Delete the remote phase branch too, so completing a phase
			// leaves nothing behind on origin. Idempotent: the branch may
			// already be gone (a provider that deleted it, or one that was
			// never pushed), so we only push --delete when the ref still
			// exists. ls-remote queries origin directly rather than trusting
			// possibly-stale remote-tracking refs left by the earlier fetch.
			remoteRef, err := gitTrim(repoDir, gitRefArgs("ls-remote", []string{"--heads"}, "origin", phaseBranch)...)
			if err != nil {
				return fmt.Errorf("git ls-remote origin %s: %w", phaseBranch, err)
			}
			if remoteRef != "" {
				// --delete moves ahead of the separator so the remote and the branch are
				// both plain positionals behind it; git accepts either ordering.
				if out, err := gitCombined(repoDir, gitRefArgs("push", []string{"--delete"}, "origin", phaseBranch)...); err != nil {
					return fmt.Errorf("git push origin --delete %s: %w\n%s", phaseBranch, err, out)
				}
			}

			RecordOutcomeEvent("phase_complete",
				map[string]int{},
				nil,
				map[string]string{"result": "completed"},
			)

			// State the resulting topology rather than a bare "done". Under
			// the milestone-branch model a correct mid-milestone state — the
			// phase merged into milestone/<version>, nothing on main yet —
			// looks exactly like a stuck one unless the run says so out loud.
			//
			// The work branch is the reconcile branch this run actually used,
			// passed in as the authoritative override. branchTopology must not
			// re-infer it: resolveCompleteBase's doc comment records that
			// milestone-derived inference is what made a stale local
			// milestone/<version> the reconcile target, and a completion
			// statement naming an inferred branch would retell the lie the
			// previous phase removed.
			top, terr := branchTopology(repoDir, root, reconcileBranch)
			Printf("completed %s — %s\n", phaseID, describeTeardown(phaseBranch, localDeleted, remoteRef != ""))
			if terr == nil {
				// Same renderer, same "not yet on main" condition, as `dross
				// status` — one truth stated in two places rather than two
				// half-truths that drift.
				Printf("branch: %s\n", renderTopologyLine(top))
			}
			return nil
		},
	}
	c.Flags().BoolVar(&recoverFlag, "recover", false,
		"on a diverged base (main or milestone/<version>), reset to origin and restore .dross/ in one shot instead of aborting")
	c.Flags().StringVar(&baseFlag, "base", "",
		"the branch this phase was forked from, when no base is recorded (a pre-check phase, or one created with --no-branch)")
	return c
}

// describeTeardown states what completion actually removed, per side. Claiming
// a deletion that did not happen is worse than saying nothing: the user reads
// it as "origin is clean" and stops looking.
func describeTeardown(phaseBranch string, local, remote bool) string {
	switch {
	case local && remote:
		return "deleted " + phaseBranch + " (local + origin)"
	case local:
		return "deleted " + phaseBranch + " (local; already gone on origin)"
	case remote:
		return "deleted " + phaseBranch + " (origin; local branch already gone)"
	default:
		return phaseBranch + " was already gone (local + origin)"
	}
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
	// Checked up front, ahead of every arm: git_main_branch reaches git through
	// completeBaseCandidates below and through the refusal message, and both are
	// on the path a hostile config takes. Every source of a ref here is
	// validated — the flag the user typed, the record in the working tree, and
	// the record read off the phase ref — because "committed data" and "typed
	// by the user" are the same thing to git's argument parser.
	if err := validateGitRef("repo.git_main_branch", p.Repo.GitMainBranch); err != nil {
		return "", err
	}
	if baseFlag != "" {
		if err := validateGitRef("--base", baseFlag); err != nil {
			return "", err
		}
		if err := gitNoOut(repoDir, gitRefArgs("rev-parse", []string{"--verify"}, "refs/heads/"+baseFlag)...); err != nil {
			return "", fmt.Errorf("--base %s: no such local branch", baseFlag)
		}
		return baseFlag, nil
	}
	if ch, err := changes.Load(changes.FilePath(root, phaseID), phaseID); err == nil && ch.Base != "" {
		if err := validateGitRef("recorded base in changes.json", ch.Base); err != nil {
			return "", err
		}
		return ch.Base, nil
	}
	if base := phaseRefRecordedBase(repoDir, phaseID); base != "" {
		if err := validateGitRef("recorded base on refs/heads/phase/"+phaseID, base); err != nil {
			return "", err
		}
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
	out, err := exec.Command("git", append([]string{"-C", repoDir}, gitRefArgs("show", nil, ref)...)...).Output()
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
		if gitNoOut(repoDir, gitRefArgs("rev-parse", []string{"--verify"}, "refs/heads/"+ms)...) == nil {
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
	out, err := exec.Command("git", append([]string{"-C", repoDir}, gitRefArgs("show", nil, ref)...)...).Output()
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
// number is recorded and the provider answers, its BaseRef is checked against
// reconcileBranch before anything else — a retargeted base refuses completion
// even when the PR is merged (see checkBaseRetarget). Absent a retarget,
// merged proceeds and unmerged refuses. When there is no recorded PR, or the
// provider can't answer (ErrMergeStatusUnsupported) or errors, it falls back to
// `git merge-base --is-ancestor origin/phase/<id> origin/<base>`: a git error
// (ref missing — squash-deleted) AND a false ancestry result BOTH map to the
// same guided refusal. It never trusts the "completed <id>" breadcrumb, never
// false-completes, and never crashes offline.
func mergeGate(repoDir string, opts ship.OpenOpts, phaseID, phaseBranch, reconcileBranch string, recordedPR int) error {
	if recordedPR > 0 {
		opts.PRNumber = recordedPR
		opts.BaseBranch = reconcileBranch
		prStatus, err := ship.PRStatusFunc(opts)
		switch {
		case err == nil:
			if retargetErr := checkBaseRetarget(prStatus.BaseRef, reconcileBranch, phaseID); retargetErr != nil {
				return retargetErr
			}
			if prStatus.Merged {
				return nil // authoritatively merged, base unchanged — proceed
			}
			return fmt.Errorf("PR #%d for %s is not merged upstream — refusing to complete so the phase branch isn't lost.\n"+
				"Merge the PR first and re-run; or if it really merged, use `dross phase complete --recover` / verify the merge manually.",
				recordedPR, phaseID)
		case errors.Is(err, hostallow.ErrRefused):
			// NOT a degrade. Every other arm here treats "couldn't ask the
			// provider" as a transient fact of life and falls through to git
			// ancestry — which is right for a flaky network and exactly wrong
			// for this: a host refusal means the repo's committed api_base
			// points somewhere the user never authorized. Falling back would
			// turn an active attack into an invisible capability downgrade,
			// which is what the locked refusal_behaviour decision forbids.
			return err
		case errors.Is(err, ship.ErrMergeStatusUnsupported):
			// Provider can't answer yet — announce and fall through to the
			// ancestry fallback rather than swallow the degrade silently.
			Printf("merge-status check skipped (%s can't answer authoritatively) — falling back to git ancestry\n", opts.Provider)
		default:
			// Network/API error — announce and fall through rather than block
			// on a transient failure.
			Printf("merge-status check skipped (%s) — falling back to git ancestry\n", err)
		}
	}
	// Fallback: git ancestry. A missing origin/phase/<id> ref (squash-deleted)
	// OR a non-ancestor result both mean "can't confirm the merge" — refuse
	// with guidance rather than trust the breadcrumb or false-complete.
	if err := gitNoOut(repoDir, gitRefArgs("merge-base", []string{"--is-ancestor"}, "origin/"+phaseBranch, "origin/"+reconcileBranch)...); err != nil {
		return fmt.Errorf("cannot confirm %s has merged into %s — no merged-PR status was available and origin/%s is not an ancestor of origin/%s "+
			"(the phase branch may have been squash-deleted, or the PR isn't merged yet).\n"+
			"Refusing so the phase branch isn't lost. If the PR really merged, use `dross phase complete --recover` or verify the merge manually.",
			phaseID, reconcileBranch, phaseBranch, reconcileBranch)
	}
	return nil
}

// checkBaseRetarget compares the provider's reported base (baseRef) against
// reconcileBranch — mergeGate's own parameter, already the fully-resolved
// recorded base per resolveCompleteBase's order (--base flag → changes.Base →
// phaseRefRecordedBase). No separate re-read of changes.Base: reconcileBranch
// already carries that resolution, and re-reading changes.Base would bypass
// --base and drop the phaseRefRecordedBase stale-tree tier.
//
// An empty baseRef — a 2xx PRStatus response whose payload omitted the base
// field — announces a skipped check and proceeds: a false retarget alarm
// would block every completion on that provider, which is worse than the gap
// this check targets. Ref names are normalized (a leading refs/heads/
// stripped) before an exact, case-sensitive compare.
func checkBaseRetarget(baseRef, reconcileBranch, phaseID string) error {
	if baseRef == "" {
		Printf("base-retarget check skipped (provider reported no base ref) — proceeding\n")
		return nil
	}
	got := strings.TrimPrefix(baseRef, "refs/heads/")
	want := strings.TrimPrefix(reconcileBranch, "refs/heads/")
	if got == want {
		return nil
	}
	return fmt.Errorf("PR for %s was opened against %q but the forge now reports its base as %q — refusing to complete against a stale base.\n"+
		"Re-point the PR on the forge to %q, or re-run `dross ship` so the record is rewritten (--recover does not bypass this check).",
		phaseID, want, got, want)
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
	if err := gitNoOut(repoDir, gitRefArgs("rev-parse", []string{"--verify"}, "refs/heads/"+branchName)...); err == nil {
		return "", false, fmt.Errorf("branch %s already exists locally; delete it first or pass --no-branch", branchName)
	}
	base, milestoneActive, err = resolveNewWorkBase(repoDir, root)
	if err != nil {
		return "", false, err
	}
	if e := checkoutBranchNew(repoDir, branchName, base); e != nil {
		return "", false, e
	}
	// Only after the fork succeeded: a failed checkout must leave no
	// changes.json behind, because the caller's rollback is os.Remove(dir),
	// which only removes an empty directory — a record written up front would
	// leak the phase id on every retry.
	//
	// The base's tip is read here, at fork time, and stored with the branch
	// name in one write. Read later it would be whatever the base has moved on
	// to, which is not this phase's fork point. A rev-parse that fails is not
	// fatal: the branch exists and the base is recorded, and the backfill
	// resolver covers a missing fork point on demand.
	tip, tipErr := gitTrim(repoDir, gitRefArgs("rev-parse", []string{"--verify"}, base)...)
	if tipErr != nil {
		tip = ""
	}
	if err := changes.SetFork(root, phaseID, base, tip); err != nil {
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
	gitArgvTap(args)
	full := append([]string{"-C", repoDir}, args...)
	return exec.Command("git", full...).Run()
}

// gitArgvTap records every argv dross hands to git. It is nil in production and
// costs one nil check; tests install a recorder and assert on ORDERING —
// specifically that a config-derived positional never precedes its separator.
//
// This is the only way to test the property that matters. Asserting on the
// builders in gitargs.go proves the builders work; it says nothing about a call
// site that quietly went back to a bare literal list, which is the regression
// this phase exists to prevent. All three exec helpers feed it, so a new call
// site is visible whichever one it picks.
var gitArgvRecorder func([]string)

func gitArgvTap(args []string) {
	if gitArgvRecorder != nil {
		gitArgvRecorder(args)
	}
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

// createdOrAdopted is the verb `dross phase create` reports.
//
// Adoption is never silent. A user who typed a title expecting a new phase and
// got an existing one has to be able to see that from the output — inferring it
// from a directory that already had contents is not the same thing.
func createdOrAdopted(adopted bool) string {
	if adopted {
		return "adopted existing"
	}
	return "created"
}
