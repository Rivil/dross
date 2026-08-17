package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Rivil/dross/internal/changes"
	"github.com/Rivil/dross/internal/hostallow"
	"github.com/Rivil/dross/internal/phase"
	"github.com/Rivil/dross/internal/project"
	"github.com/Rivil/dross/internal/ship"
	"github.com/Rivil/dross/internal/state"
	"github.com/Rivil/dross/internal/verify"
)

// remotePolicy derives the API host allowlist for [remote] calls: the host of
// [remote].url, the built-in SaaS defaults, and the machine-local additions
// from .dross/local.toml.
//
// It returns an error — rather than an empty extras list — when git reports
// local.toml tracked, because a committed local.toml is a repo authorizing its
// own exfiltration host through the one input the derivation trusts.
func remotePolicy(root, repoDir string, p *project.Project) (hostallow.Policy, error) {
	extra, err := readAllowHosts(root, repoDir)
	if err != nil {
		return hostallow.Policy{}, err
	}
	return hostallow.Derive(p.Remote.URL, extra), nil
}

// buildOpenOpts maps a project's [remote] config onto ship.OpenOpts. Extracted
// from the inline ship literal so the provider / auth_user / auth_scheme /
// project_id wiring is unit-testable — a dropped field (e.g. GitLab silently
// using default auth or a derived project id even when the user overrode them,
// or Bitbucket losing the user half of its Basic credential and 401ing on every
// ship) is caught by ship_test.go.
func buildOpenOpts(p *project.Project, hosts hostallow.Policy) ship.OpenOpts {
	return ship.OpenOpts{
		Hosts:      hosts,
		Provider:   p.Remote.Provider,
		URL:        p.Remote.URL,
		APIBase:    p.Remote.APIBase,
		AuthEnv:    p.Remote.AuthEnv,
		AuthUser:   p.Remote.AuthUser,
		AuthScheme: p.Remote.AuthScheme,
		ProjectID:  p.Remote.ProjectID,
		Reviewers:  p.Remote.Reviewers,
	}
}

// buildCommentOpts maps a project's [remote] config onto ship.CommentOpts,
// carrying the same provider / auth / project fields as buildOpenOpts.
func buildCommentOpts(p *project.Project, hosts hostallow.Policy) ship.CommentOpts {
	return ship.CommentOpts{
		Hosts:      hosts,
		Provider:   p.Remote.Provider,
		URL:        p.Remote.URL,
		APIBase:    p.Remote.APIBase,
		AuthEnv:    p.Remote.AuthEnv,
		AuthUser:   p.Remote.AuthUser,
		AuthScheme: p.Remote.AuthScheme,
		ProjectID:  p.Remote.ProjectID,
	}
}

// Ship orchestrates /dross-ship: pushes the current phase/<id> branch
// and opens a provider-aware PR with auto-assigned human reviewers.
// The provider's squash-merge collapses the per-task commits on the
// branch into a single commit on main — no client-side squash needed.
func Ship() *cobra.Command {
	var (
		title           string
		body            string
		bodyFile        string
		noPush          bool
		draft           bool
		forceUnverified bool
		forcePush       bool
		printBody       bool
		auto            bool
		jsonOut         bool
	)
	c := &cobra.Command{
		Use:   "ship [phase-id]",
		Short: "Push phase/<id> and open a PR via the project's provider",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			root, err := FindRoot()
			if err != nil {
				return err
			}
			repoDir := filepath.Dir(root)

			// narrate is the human-facing progress channel. Under --json it
			// goes silent so stdout carries exactly one machine-readable JSON
			// object (emitted at the end); otherwise it prints as usual.
			narrate := func(format string, a ...any) {
				if !jsonOut {
					Printf(format, a...)
				}
			}
			p, _, err := loadProject()
			if err != nil {
				return err
			}
			s, err := state.Load(filepath.Join(root, state.File))
			if err != nil {
				return err
			}

			// 1) Resolve phase id.
			phaseID := ""
			if len(args) == 1 {
				phaseID = args[0]
			} else {
				phaseID = s.CurrentPhase
			}
			if phaseID == "" {
				return errors.New("no phase id given and state has no current_phase")
			}
			phaseBranch := "phase/" + phaseID

			// 2) Load phase artefacts.
			phaseDir := phase.Dir(root, phaseID)
			spec, err := phase.LoadSpec(filepath.Join(phaseDir, "spec.toml"))
			if err != nil {
				return fmt.Errorf("load spec: %w", err)
			}
			vTests, vToml := verify.FilePaths(root, phaseID)
			_ = vTests
			vrf, err := verify.LoadVerify(vToml)
			if err != nil {
				return fmt.Errorf("load verify for %s: %w\n\nNext: run /dross-verify to write verify.toml, then `dross verify finalize %s`, then re-run ship",
					phaseID, err, phaseID)
			}

			// 3) Pre-flight gates.
			if p.Remote.URL == "" || p.Remote.Provider == "" {
				return errors.New("project has no [remote].url or .provider — run /dross-options or /dross-onboard")
			}
			// Heal-before-gate: a resolved verdict that was never
			// finalized gets its outcome event recorded here, BEFORE the
			// pass-only refusal — a partial/fail verdict is recorded,
			// then still refused. Keeps the forget-to-finalize hole from
			// leaking pending events into stats. The marker write dirties
			// verify.toml; the pre-stage autoCommitDrossDirt below folds
			// it into the push.
			switch vrf.Verify.Verdict {
			case "pass", "partial", "fail":
				if !vrf.Verify.Finalized {
					recorded, verdict, err := finalizeVerify(root, phaseID)
					if err != nil {
						return fmt.Errorf("auto-finalize verify for %s: %w", phaseID, err)
					}
					if recorded {
						narrate("auto-finalized verify verdict=%s (was resolved but unrecorded)\n", verdict)
					}
					vrf.Verify.Finalized = true
				}
			}
			if vrf.Verify.Verdict != "pass" && !forceUnverified {
				switch vrf.Verify.Verdict {
				case "pending":
					return fmt.Errorf("verify verdict is \"pending\" for %s — finalize with `dross verify finalize %s`, or pass --force-unverified to override",
						phaseID, phaseID)
				case "fail", "partial":
					return fmt.Errorf("verify verdict is %q for %s — fix the failing criteria and re-run /dross-verify, or pass --force-unverified to override",
						vrf.Verify.Verdict, phaseID)
				default:
					return fmt.Errorf("verify verdict is %q (need \"pass\"); use --force-unverified to override",
						vrf.Verify.Verdict)
				}
			}

			// Must be on the phase branch. Pushing from anywhere else is
			// almost certainly a mistake — phase work belongs on phase/<id>.
			//
			// The suggestion names the guarded verb, not `git checkout`: a raw
			// checkout of a branch that still tracks .dross/state.json replays
			// it over the live machine-local copy without complaint, which is
			// what destroyed a live history on the state-json-branch-safety
			// ship. A refusal that hands the user the unguarded form reopens
			// that hole by hand, one obedient copy-paste at a time.
			cur, err := gitTrim(repoDir, "symbolic-ref", "--short", "HEAD")
			if err != nil {
				return fmt.Errorf("read current branch: %w", err)
			}
			if cur != phaseBranch {
				return fmt.Errorf("must be on %s to ship (currently on %s); switch with `dross phase checkout %s`",
					phaseBranch, cur, phaseID)
			}

			// 4) Title + body.
			if title == "" {
				title = fmt.Sprintf("phase %s: %s", phaseID, spec.Phase.Title)
			}
			if bodyFile != "" {
				b, err := os.ReadFile(bodyFile)
				if err != nil {
					return fmt.Errorf("read --body-file: %w", err)
				}
				body = string(b)
			}
			if body == "" {
				body = ship.BuildPRBody(spec, vrf)
			}

			if printBody {
				Print(body)
				return nil
			}

			if noPush {
				narrate("--no-push set; not pushing or opening PR.\n")
				return nil
			}

			// c-7: repair any red-proof pin held up only by this phase's own
			// remote-tracking branch, BEFORE the merge that deletes it. Run
			// ahead of the clean-tree gate on purpose — the repair rewrites a
			// tracked doc, so it commits its own two files rather than leaving
			// dirt the gate would refuse, and the rewrite then rides this
			// phase's PR where it is reviewed alongside the code.
			if repointed, err := repointDoomedRedProofs(root, repoDir, phaseID); err != nil {
				return err
			} else if repointed {
				narrate("repointed a red proof that this merge would have orphaned\n")
			}

			// Pre-stage gate: the tree must be clean before ship starts
			// staging its own commits. Bookkeeping-only dirt under .dross/
			// is auto-committed so it rides the push; any other dirt refuses
			// before anything is staged or pushed.
			drossCommitted, err := autoCommitDrossDirt(repoDir, "shipping")
			if err != nil {
				return err
			}
			if drossCommitted {
				narrate("auto-committed .dross-only bookkeeping\n")
			}

			// 5) Mark the phase shipped. Ship deliberately does NOT write
			//    the completed-state transition: a phase is not complete
			//    until its PR is merged, and ship runs before that is known.
			//    `dross phase complete` is the sole writer of the completion
			//    record, behind its merge gate. What ship records is the
			//    intermediate truth — current_phase stays set, status becomes
			//    "shipped" — so status surfaces can name the open PR instead
			//    of claiming a completion nothing confirmed.
			//
			//    Idempotent: a re-ship after review edits re-writes the same
			//    status, the `shipped <id>` entry is history-scan-guarded so
			//    it never doubles up, and the commit below only runs when
			//    something actually staged.
			s.CurrentPhaseStatus = "shipped"
			if !historyHasAction(s, "shipped "+phaseID) {
				s.Touch(fmt.Sprintf("shipped %s", phaseID))
			}
			if err := s.Save(filepath.Join(root, state.File)); err != nil {
				return fmt.Errorf("save state: %w", err)
			}
			// The write stays local: state.json is gitignored (locked
			// state_tracking), so there is nothing to stage — an explicit
			// `git add .dross/state.json` here hard-fails on "paths are
			// ignored by one of your .gitignore files". Anything else the
			// auto-commit above left staged still gets its commit.
			if err := gitNoOut(repoDir, "diff", "--cached", "--quiet"); err != nil {
				// Non-nil err means there IS a staged change to commit.
				shipMsg := fmt.Sprintf("chore(dross): ship %s", phaseID)
				if out, err := gitCombined(repoDir, "commit", "-m", shipMsg); err != nil {
					return fmt.Errorf("git commit: %w\n%s", err, out)
				}
			}

			// 6) Resolve the PR base: the active milestone's integration
			//    branch when it exists, else main (rollout_cutover /
			//    no_milestone_fallback via the shared resolver). Resolved
			//    before the push so the remote-base guard can run first.
			baseBranch, milestoneActive, err := resolveNewWorkBase(repoDir, root)
			if err != nil {
				return err
			}
			// Guard only the milestone case: milestone/<version> is pushed at
			// scope time, so its absence on origin means scoping was
			// incomplete — refuse rather than open a PR against a base the
			// provider can't see. main is the always-present default.
			if milestoneActive {
				if err := gitNoOut(repoDir, gitRefArgs("ls-remote", []string{"--exit-code", "--heads"}, "origin", baseBranch)...); err != nil {
					return fmt.Errorf("base branch %q is not on origin — it is pushed when the milestone is scoped; re-scope or push it before shipping", baseBranch)
				}
			}
			// Nudge (never require) scoping a milestone when falling back to
			// main with none active — mirrors phase create / base-branch.
			// Silent in the cutover case (a milestone is set but predates the
			// branch model).
			if !milestoneActive && s.CurrentMilestone == "" {
				narrate("no milestone active — PR targets %s; scope one with `dross milestone <version>` for a staging branch\n", baseBranch)
			}

			// Safety net (c-2): .dross-only chores sitting unpushed on the
			// local base re-seed divergence at the next squash-merge. Ship
			// already requires network, so it absorbs the push; a code-ahead
			// base or a failed push is a hard refusal.
			basePushed, err := pushBaseIfAheadDrossOnly(repoDir, baseBranch)
			if err != nil {
				return err
			}
			if basePushed {
				narrate("pushed unpushed .dross chores on %s to origin\n", baseBranch)
			}
			quickPushed, quickBase, err := pushQuickBaseIfRecorded(repoDir, root, baseBranch)
			if err != nil {
				return err
			}
			if quickPushed {
				narrate("pushed unpushed .dross chores on %s (recorded quick_base) to origin\n", quickBase)
			}

			// 7) Push phase/<id> directly. The provider's squash-merge will
			//    collapse the per-task commits into one on the base; no
			//    client-side synthetic branch needed.
			pushArgs := gitRefArgs("push", []string{"-u"}, "origin", phaseBranch)
			if forcePush {
				// --force-with-lease guards against clobbering a concurrent
				// push from another machine without requiring the user to
				// know the remote SHA.
				pushArgs = gitRefArgs("push", []string{"-u", "--force-with-lease"}, "origin", phaseBranch)
			}
			pushOut, perr := gitCombined(repoDir, pushArgs...)
			if perr != nil {
				return fmt.Errorf("git push: %w\n%s", perr, pushOut)
			}
			narrate("Pushed %s to origin\n", phaseBranch)

			// 8) Open the PR via the provider (base resolved in step 6).
			hosts, herr := remotePolicy(root, repoDir, p)
			if herr != nil {
				return herr
			}
			opts := buildOpenOpts(p, hosts)
			opts.HeadBranch = phaseBranch
			opts.BaseBranch = baseBranch
			opts.Title = title
			opts.Body = body
			opts.Draft = draft
			if auto {
				// Per-invocation, non-destructive: request zero reviewers
				// for this run without mutating remote.reviewers config.
				// --auto governs prompts/defaults only — the generated body
				// stays the default and explicit --body/--body-file/--draft
				// still win (they were already applied above). Downstream
				// narration + telemetry read opts.Reviewers, so clearing it
				// here is the single source that suppresses the "Reviewers
				// requested" line and zeroes the telemetry count too.
				opts.Reviewers = nil
			}
			res, err := ship.OpenPR(opts)
			if err != nil && res == nil {
				return fmt.Errorf("open PR: %w", err)
			}
			if res != nil {
				narrate("PR opened: %s (#%d)\n", res.URL, res.Number)
				// Read opts.Reviewers, not p.Remote.Reviewers: under --auto
				// the former is cleared, so no reviewers were actually
				// requested and this line must stay silent.
				if len(opts.Reviewers) > 0 {
					narrate("Reviewers requested: %s\n", strings.Join(opts.Reviewers, ", "))
				}
			}
			if err != nil {
				// Non-fatal post-PR errors (e.g. reviewer add failed).
				narrate("Warning: %v\n", err)
			}
			narrate("Marked %s shipped — once the PR merges, `dross phase complete %s` writes the completion record\n", phaseID, phaseID)

			// Persist the opened PR number into the phase-scoped changes.json
			// so `dross phase complete` can gate on THIS phase's authoritative
			// merge status (a phase-scoped record can't be dragged forward in
			// cumulative state history the way the completion breadcrumb is).
			// Only when a real PR number is known — never write PR:0. Commit it
			// onto phase/<id> AND push it: the PR number is known only after the
			// PR is opened (post first-push), so this record can't ride the
			// initial push — it needs a second push so the open PR (and thus its
			// squash-merge) carries the recorded number onto the base branch's
			// changes.json, where mergeGate reads it. A local-only post-push
			// commit (the old behaviour) never reached the base, so mergeGate's
			// authoritative recorded-PR path was unreachable and every
			// squash-merged completion fell back to the ancestry check and
			// refused. The --no-push path returned earlier (no PR), so this is
			// naturally skipped there.
			if res != nil && res.Number > 0 {
				if err := changes.SetPR(root, phaseID, res.Number); err != nil {
					return fmt.Errorf("persist PR number: %w", err)
				}
				// The authoritative half of base_write_timing: what the PR was
				// actually opened against wins over the value create recorded,
				// if the two ever diverge (a milestone scoped after the fork is
				// the ordinary way that happens). It rides the same commit and
				// push as the PR number, so the squash carries base and pr onto
				// the base branch together and `dross phase complete` reads a
				// consistent pair.
				if err := changes.SetBase(root, phaseID, baseBranch); err != nil {
					return fmt.Errorf("persist PR base branch: %w", err)
				}
				// Rides the same write as pr and base: a phase with an open PR
				// is shipped, and that is the first durable point anything can
				// tell how far the phase got without reading state history.
				if err := changes.SetStatus(root, phaseID, changes.StatusShipped); err != nil {
					return fmt.Errorf("persist phase status: %w", err)
				}
				changesRel := filepath.Join(".dross", "phases", phaseID, changes.File)
				if out, err := gitCombined(repoDir, gitPathArgs("add", nil, changesRel)...); err != nil {
					return fmt.Errorf("git add changes.json: %w\n%s", err, out)
				}
				// Only commit + push when the add actually staged a change, so a
				// re-ship that records the same PR number doesn't error on
				// "nothing to commit" and doesn't fire a redundant push.
				if err := gitNoOut(repoDir, "diff", "--cached", "--quiet"); err != nil {
					prMsg := fmt.Sprintf("chore(dross): record PR #%d for %s", res.Number, phaseID)
					if out, err := gitCombined(repoDir, "commit", "-m", prMsg); err != nil {
						return fmt.Errorf("git commit PR number: %w\n%s", err, out)
					}
					// Push the record onto the phase branch so it reaches the
					// open PR / squash / base. Mirror the initial push's
					// force-with-lease handling under --force.
					recordPushArgs := gitRefArgs("push", nil, "origin", phaseBranch)
					if forcePush {
						recordPushArgs = gitRefArgs("push", []string{"--force-with-lease"}, "origin", phaseBranch)
					}
					if out, err := gitCombined(repoDir, recordPushArgs...); err != nil {
						return fmt.Errorf("git push PR record: %w\n%s", err, out)
					}
					narrate("Pushed PR record to %s\n", phaseBranch)
				}
			}

			// 8) Telemetry — capture shape of this ship without leaking
			//    repo URL, body content, or reviewer names.
			tags := map[string]string{
				"provider": p.Remote.Provider,
				"result":   shipResultTag(res, err),
			}
			if draft {
				tags["draft"] = "true"
			}
			if forceUnverified || forcePush {
				tags["force"] = "true"
			}
			if auto {
				tags["auto"] = "true"
			}
			counts := map[string]int{
				// opts.Reviewers is post-auto-clearing: --auto records 0,
				// matching what was actually requested.
				"reviewers":   len(opts.Reviewers),
				"body_chars":  len(body),
				"title_chars": len(title),
			}
			RecordOutcomeEvent("ship", counts, nil, tags)

			// --json: emit a single machine-readable object on stdout (the
			// only thing printed under --json, since narration was suppressed).
			// Composable with --auto — result is the same shipResultTag bucket.
			if jsonOut {
				out := struct {
					URL    string `json:"url"`
					Number int    `json:"number"`
					Result string `json:"result"`
				}{Result: shipResultTag(res, err)}
				if res != nil {
					out.URL = res.URL
					out.Number = res.Number
				}
				b, mErr := json.Marshal(out)
				if mErr != nil {
					return fmt.Errorf("marshal --json output: %w", mErr)
				}
				Print(string(b))
			}
			return nil
		},
	}
	c.Flags().StringVar(&title, "title", "", "PR title (default: 'phase <id>: <spec title>')")
	c.Flags().StringVar(&body, "body", "", "PR body override (overrides generated body)")
	c.Flags().StringVar(&bodyFile, "body-file", "", "read PR body from file")
	c.Flags().BoolVar(&noPush, "no-push", false, "don't push the phase branch or open a PR")
	c.Flags().BoolVar(&draft, "draft", false, "open the PR as draft")
	c.Flags().BoolVar(&forceUnverified, "force-unverified", false, "skip the 'verify must be pass' gate")
	c.Flags().BoolVar(&forcePush, "force", false,
		"force-with-lease the push (use when re-pushing after rewriting phase/<id>)")
	c.Flags().BoolVar(&printBody, "print-body", false, "print the generated PR body and exit (no push, no PR)")
	c.Flags().BoolVar(&auto, "auto", false,
		"non-interactive: request zero reviewers for this run (without mutating remote.reviewers) and use the generated body; for scripts and loops")
	c.Flags().BoolVar(&jsonOut, "json", false,
		"emit a single JSON object {url, number, result} on stdout and suppress human narration; composable with --auto for scripts and loops")
	c.AddCommand(shipComment())
	c.AddCommand(shipRecover())
	return c
}

// shipComment posts a markdown comment to an existing PR via the
// project's provider. Used by /dross-review to publish the aggregated
// subagent panel findings as a single consolidated comment.
func shipComment() *cobra.Command {
	var (
		prNumber int
		body     string
		bodyFile string
	)
	c := &cobra.Command{
		Use:   "comment --pr <n> (--body \"...\" | --body-file <path>)",
		Short: "Post a comment to a PR via the project's provider",
		RunE: func(_ *cobra.Command, _ []string) error {
			if prNumber <= 0 {
				return errors.New("--pr is required")
			}
			if body == "" && bodyFile == "" {
				return errors.New("either --body or --body-file is required")
			}
			if bodyFile != "" {
				b, err := os.ReadFile(bodyFile)
				if err != nil {
					return fmt.Errorf("read --body-file: %w", err)
				}
				body = string(b)
			}
			p, _, err := loadProject()
			if err != nil {
				return err
			}
			if p.Remote.URL == "" || p.Remote.Provider == "" {
				return errors.New("project has no [remote].url or .provider — run /dross-options or /dross-onboard")
			}
			root, rerr := FindRoot()
			if rerr != nil {
				return rerr
			}
			hosts, herr := remotePolicy(root, filepath.Dir(root), p)
			if herr != nil {
				return herr
			}
			co := buildCommentOpts(p, hosts)
			co.PRNumber = prNumber
			co.Body = body
			if err := ship.PostComment(co); err != nil {
				return fmt.Errorf("post comment: %w", err)
			}
			Printf("Posted comment to PR #%d\n", prNumber)
			return nil
		},
	}
	c.Flags().IntVar(&prNumber, "pr", 0, "PR number to comment on (required)")
	c.Flags().StringVar(&body, "body", "", "comment body (markdown)")
	c.Flags().StringVar(&bodyFile, "body-file", "", "read comment body from file")
	return c
}

// shipResultTag classifies a ship's outcome into a single token. Used
// for the telemetry "result" tag so ship outcomes are easy to bucket.
func shipResultTag(res *ship.OpenResult, err error) string {
	switch {
	case err != nil && res == nil:
		return "failed"
	case err != nil && res != nil:
		return "partial" // PR opened, post-step (reviewers etc.) failed
	case res != nil:
		return "opened"
	default:
		return "noop"
	}
}
