package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Rivil/dross/internal/board"
	"github.com/Rivil/dross/internal/boardsync"
	"github.com/Rivil/dross/internal/configenum"
	"github.com/Rivil/dross/internal/forge"
	"github.com/Rivil/dross/internal/phase"
)

// boardCtx is the in-package name for boardsync.Ctx — the bundle every board
// operation takes. openBoard builds it; the verbs below hand it to the
// package.
type boardCtx = boardsync.Ctx

// Issue registers `dross issue …` — mirroring dross planning artefacts onto
// the repo's issue-tracker board, and pulling inbound issues for triage.
func Issue() *cobra.Command {
	c := &cobra.Command{
		Use:   "issue",
		Short: "Mirror planning onto the issue board and pull inbound issues",
	}
	c.AddCommand(
		issueEnable(),
		issueDisable(),
		issueMilestone(),
		issueBacklog(),
		issuePhase(),
		issueTask(),
		issueQuick(),
		issueReap(),
		issuePull(),
		issueDismiss(),
		issueLink(),
		issueList(),
	)
	return c
}

// The five mirror verbs are nested subcommands — `issue phase sync`, `issue
// task sync`, `issue task pull`, `issue milestone sync`, `issue backlog sync`
// — not hyphenated compounds. The parent names the artefact class being
// mirrored; the child names the direction. There are no aliases for the old
// spellings: cobra's unknown-subcommand guard is the migration signal.

func issuePhase() *cobra.Command {
	c := &cobra.Command{
		Use:   "phase",
		Short: "Mirror a phase onto the board",
	}
	c.AddCommand(issuePhaseSync())
	return c
}

func issueTask() *cobra.Command {
	c := &cobra.Command{
		Use:   "task",
		Short: "Mirror plan tasks onto the board, and pull board moves back",
	}
	c.AddCommand(issueTaskSync(), issueTaskPull())
	return c
}

func issueMilestone() *cobra.Command {
	c := &cobra.Command{
		Use:   "milestone",
		Short: "Mirror a milestone onto the board",
	}
	c.AddCommand(issueMilestoneSync())
	return c
}

func issueBacklog() *cobra.Command {
	c := &cobra.Command{
		Use:   "backlog",
		Short: "Mirror the milestone backlog onto the board",
	}
	c.AddCommand(issueBacklogSync())
	return c
}

// openBoard loads project + board.json + a board client, resolved SOLELY from
// the [board] config block (never [remote] — a repo can ship code to one host
// and track issues on another). When board sync is disabled it returns
// enabled=false and no error, so the workflow prompts can call `dross issue …`
// unconditionally and have it be a silent no-op for anyone who hasn't opted in.
func openBoard() (ctx *boardCtx, enabled bool, err error) {
	proj, _, err := loadProject()
	if err != nil {
		return nil, false, err
	}
	if !proj.Board.Enabled {
		return nil, false, nil
	}
	root, err := FindRoot()
	if err != nil {
		return nil, false, err
	}
	// The machine-local allowlist additions. readAllowHosts errors — rather
	// than returning an empty list — when git reports .dross/local.toml
	// tracked, so a repo that committed one cannot authorize its own board
	// host through it.
	extra, err := readAllowHosts(root, filepath.Dir(root))
	if err != nil {
		return nil, false, err
	}
	client, err := forge.NewBoard(boardsync.Config(proj.Board, proj.Remote.URL, extra))
	if err != nil {
		return nil, false, err
	}
	bd, err := board.Load(filepath.Join(root, board.File))
	if err != nil {
		return nil, false, err
	}
	return &boardCtx{
		Client:    client,
		Board:     bd,
		Proj:      proj,
		Root:      root,
		BoardPath: filepath.Join(root, board.File),
	}, true, nil
}

// --- enable / disable ---

func issueEnable() *cobra.Command {
	return &cobra.Command{
		Use:   "enable",
		Short: "Turn on issue-board sync for this project",
		RunE: func(_ *cobra.Command, _ []string) error {
			p, path, err := loadProject()
			if err != nil {
				return err
			}
			p.Board.Enabled = true
			if err := p.Save(path); err != nil {
				return err
			}
			Print("board sync enabled")
			switch {
			case configenum.BoardProviders.Has(p.Board.Provider):
				// recognised
			case configenum.Normalize(p.Board.Provider) == "":
				Printf("note: [board].provider is unset — set it to %s\n", configenum.BoardProviders.List())
			default:
				Printf("note: provider %q has no board backend (%s)\n", p.Board.Provider, configenum.BoardProviders.List())
			}
			// Only nag about base_url where the backend has no default address
			// — a github board resolves to api.github.com on its own.
			if p.Board.BaseURL == "" && configenum.BoardRequiresBaseURL(p.Board.Provider) {
				Print("note: [board].base_url is unset — needed for the board API")
			}
			if p.Board.AuthEnv == "" {
				Print("note: [board].auth_env is unset — needed for the board token")
			}
			if p.Board.Project == "" {
				Print("note: [board].project is unset — needed to scope board issues")
			}
			return nil
		},
	}
}

func issueDisable() *cobra.Command {
	return &cobra.Command{
		Use:   "disable",
		Short: "Turn off issue-board sync for this project",
		RunE: func(_ *cobra.Command, _ []string) error {
			p, path, err := loadProject()
			if err != nil {
				return err
			}
			p.Board.Enabled = false
			if err := p.Save(path); err != nil {
				return err
			}
			Print("board sync disabled")
			return nil
		},
	}
}

// --- milestone sync ---

func issueMilestoneSync() *cobra.Command {
	var doClose bool
	c := &cobra.Command{
		Use:   "sync <version>",
		Short: "Ensure a board milestone exists for a dross milestone",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			ctx, enabled, err := openBoard()
			if err != nil {
				return err
			}
			if !enabled {
				return nil // no-op when board sync is off
			}
			// Gated BEFORE the ensure, so a board whose milestone entity has
			// no close verb makes no tracker write at all on a --close run.
			if doClose {
				if err := boardsync.CheckMilestoneClosable(ctx); err != nil {
					return err
				}
			}
			id, err := boardsync.EnsureMilestoneLink(ctx, args[0])
			if err != nil {
				return err
			}
			if id == "" {
				return fmt.Errorf("milestone %q not found under .dross/milestones/", args[0])
			}
			if err := ctx.Board.Save(ctx.BoardPath); err != nil {
				return err
			}
			if doClose {
				if err := boardsync.CloseIssue(ctx, id, ""); err != nil {
					return err
				}
				Printf("milestone %s -> board %s (closed)\n", args[0], id)
				return nil
			}
			Printf("milestone %s -> board %s\n", args[0], id)
			return nil
		},
	}
	c.Flags().BoolVar(&doClose, "close", false, "resolve the milestone's epic (use at milestone finalize; epic mode only)")
	return c
}

// --- backlog sync ---

func issueBacklogSync() *cobra.Command {
	var fromPhase string
	c := &cobra.Command{
		Use:   "sync [version]",
		Short: "Sync the milestone backlog (unscaffolded slugs + someday ideas) to the board",
		Args:  cobra.RangeArgs(0, 1),
		RunE: func(_ *cobra.Command, args []string) error {
			version, run, err := resolveBacklogVersion(args, fromPhase)
			if err != nil || !run {
				return err
			}
			ctx, enabled, err := openBoard()
			if err != nil {
				return err
			}
			if !enabled {
				return nil
			}
			return boardsync.SyncBacklog(ctx, version)
		},
	}
	c.Flags().StringVar(&fromPhase, "phase", "", "resolve the version from this phase's spec.toml [phase].milestone (use at ship finalize)")
	return c
}

// resolveBacklogVersion picks the milestone version to reconcile: the literal
// positional argument, or the one the named phase's spec records.
//
// The --phase form exists because ship.md's finalize steps carry only a phase
// id. Resolving the version in Go rather than asking the prompt to dig it out
// of spec.toml keeps it greppable and testable; prose in a prompt is neither.
//
// run is false when there is nothing to do — a phase with no milestone — which
// is a clean exit, not an error: a phase outside any milestone has no backlog
// to reconcile.
func resolveBacklogVersion(args []string, fromPhase string) (version string, run bool, err error) {
	if (len(args) == 1) == (fromPhase != "") {
		return "", false, fmt.Errorf("`backlog sync` takes either a <version> argument or --phase <phase-id>, not both and not neither")
	}
	if len(args) == 1 {
		return args[0], true, nil
	}
	root, err := FindRoot()
	if err != nil {
		return "", false, err
	}
	spec, err := phase.LoadSpec(filepath.Join(phase.Dir(root, fromPhase), "spec.toml"))
	if err != nil {
		return "", false, fmt.Errorf("resolve milestone for phase %s: %w", fromPhase, err)
	}
	if spec.Phase.Milestone == "" {
		Printf("phase %s belongs to no milestone — no backlog to reconcile\n", fromPhase)
		return "", false, nil
	}
	return spec.Phase.Milestone, true, nil
}

// --- phase sync ---

func issuePhaseSync() *cobra.Command {
	var status string
	var doClose bool
	c := &cobra.Command{
		Use:   "sync <phase-id>",
		Short: "Create or update the board issue for a phase (idempotent)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			// Validated ahead of openBoard on purpose: the enabled check below
			// returns nil, so a typo'd --status on a repo that hasn't opted
			// into board sync would exit 0 as a silent no-op and only surface
			// on the machine where sync is on.
			if status != "" {
				if !configenum.LifecycleStatuses.Has(status) {
					return fmt.Errorf("unknown --status %q; expected %s", status, configenum.LifecycleStatuses.List())
				}
				// Assign the normalized form back. boardsync.SyncPhase passes status raw
				// to statusLabel and to both SetState lookups, so validating
				// without reassigning would accept " Verifying", emit the label
				// "dross/status: Verifying" and then miss the state map — the
				// exact unmapped-state warning this phase exists to close.
				status = configenum.Normalize(status)
			}
			ctx, enabled, err := openBoard()
			if err != nil {
				return err
			}
			if !enabled {
				return nil
			}
			return boardsync.SyncPhase(ctx, args[0], status, doClose)
		},
	}
	c.Flags().StringVar(&status, "status", "", "lifecycle status label ("+configenum.LifecycleStatuses.List()+"); derived from the plan if unset")
	c.Flags().BoolVar(&doClose, "close", false, "close the issue (use on ship)")
	return c
}

// --- quick ---

func issueQuick() *cobra.Command {
	var doClose bool
	c := &cobra.Command{
		Use:   "quick <ref> [title]",
		Short: "Open (or --close) a standalone issue for a quick task",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(_ *cobra.Command, args []string) error {
			ctx, enabled, err := openBoard()
			if err != nil {
				return err
			}
			if !enabled {
				return nil
			}
			ref := args[0]

			if doClose {
				key, ok := ctx.Board.QuickIssue(ref)
				if !ok {
					return fmt.Errorf("no board issue linked to quick ref %q", ref)
				}
				// Through boardsync.CloseIssue, not CloseIssue: the quick lane
				// gets the same mapped write and verified read-back as the
				// phase lane, so it can no longer report a close over an
				// issue the tracker still holds unresolved (c-4).
				if err := boardsync.CloseIssue(ctx, key, ""); err != nil {
					return err
				}
				Printf("quick %s -> board %s (closed)\n", ref, key)
				return nil
			}

			if len(args) < 2 {
				return fmt.Errorf("a title is required to open a quick issue")
			}
			iss, err := ctx.Client.CreateIssue(forge.IssueInput{
				Title:  args[1],
				Body:   fmt.Sprintf("Quick task `%s`.\n\n_Tracked by dross._", ref),
				Labels: []string{boardsync.LabelMarker, boardsync.LabelQuick},
			})
			if err != nil {
				return boardsync.Wrap(err)
			}
			ctx.Board.SetQuick(ref, iss.Key)
			if err := ctx.Board.Save(ctx.BoardPath); err != nil {
				return err
			}
			Printf("quick %s -> board %s\n", ref, iss.Key)
			return nil
		},
	}
	c.Flags().BoolVar(&doClose, "close", false, "close the quick issue linked to <ref>")
	return c
}

// --- pull (inbound triage feed) ---

func issuePull() *cobra.Command {
	var labels, state string
	var asJSON, mark bool
	c := &cobra.Command{
		Use:   "pull",
		Short: "List open board issues not yet linked to dross work (inbound triage)",
		RunE: func(_ *cobra.Command, _ []string) error {
			ctx, enabled, err := openBoard()
			if err != nil {
				// A SETUP failure is reported the same way a fetch failure is,
				// for the same reason: --json promises an envelope and exit 0,
				// so a consumer doing `jq .issues` dies on a parse error rather
				// than reading .error and reporting the board as unreachable.
				//
				// This covers the whole class openBoard can raise — an unset
				// auth variable, an unreadable project.toml, a directory that
				// is not a dross repo, a tracked local.toml refusal, an unknown
				// provider, missing board config, a refused host, a corrupt
				// board.json — because fixing only the case that got reported
				// leaves the identical crash waiting behind every sibling, and
				// a consumer cannot tell which one it got.
				//
				// Deliberately NOT fixed at openBoard or the forge
				// constructors: seven of the eight openBoard callers are
				// human-facing commands that should keep exiting non-zero on
				// exactly these conditions, and degrading them into silent
				// no-ops is the fault this trades against.
				return boardsync.ReportBoardFailure(os.Stdout, asJSON, true, err)
			}
			if !enabled {
				if asJSON {
					return boardsync.EmitPullEnvelope(os.Stdout, nil, nil)
				}
				return nil
			}
			filter := forge.IssueFilter{State: state}
			if labels != "" {
				filter.Labels = splitCSV(labels)
			}
			inbound, boardErr := boardsync.CollectInbound(ctx, filter)
			if boardErr != nil {
				// A fetch failure is reported, not raised: the workflow
				// prompts call `dross issue …` unconditionally on the promise
				// that it is a safe no-op, so a non-zero exit would break
				// that contract. The signal travels in the payload instead.
				return boardsync.ReportBoardFailure(os.Stdout, asJSON, false, boardErr)
			}

			// Read-only by default so /dross-status can poll without
			// mutating .dross. --mark stamps last_pull (used by /dross-inbox).
			// Only a pull that actually happened is worth stamping — marking
			// a failed fetch is the silent-zero fault wearing a different hat.
			if mark {
				ctx.Board.MarkPulled()
				if err := ctx.Board.Save(ctx.BoardPath); err != nil {
					// The pull itself succeeded, but the stamp did not, and a
					// consumer told nothing would treat the next run's results
					// as already-seen. Reported through the envelope like every
					// other failure rather than raised past it.
					return boardsync.ReportBoardFailure(os.Stdout, asJSON, true, err)
				}
			}

			if asJSON {
				return boardsync.EmitPullEnvelope(os.Stdout, inbound, nil)
			}
			if len(inbound) == 0 {
				Print("no new issues on the board")
				return nil
			}
			Printf("%d new issue(s) to triage:\n", len(inbound))
			for _, iss := range inbound {
				labelStr := ""
				if len(iss.Labels) > 0 {
					labelStr = "  [" + strings.Join(iss.Labels, ", ") + "]"
				}
				Printf("  %s %s%s\n", iss.Key, iss.Title, labelStr)
			}
			return nil
		},
	}
	c.Flags().StringVar(&labels, "labels", "", "only issues with these labels (csv, e.g. bug,enhancement)")
	c.Flags().StringVar(&state, "state", "open", "issue state: open|closed|all")
	c.Flags().BoolVar(&asJSON, "json", false, "emit a JSON envelope {issues, error} (for prompt consumption)")
	c.Flags().BoolVar(&mark, "mark", false, "record the pull time in board.json (otherwise read-only)")
	return c
}

// --- dismiss ---

func issueDismiss() *cobra.Command {
	return &cobra.Command{
		Use:   "dismiss <issue-id>",
		Short: "Stop an inbound issue from resurfacing in triage",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			id := args[0]
			root, err := FindRoot()
			if err != nil {
				return err
			}
			path := filepath.Join(root, board.File)
			bd, err := board.Load(path)
			if err != nil {
				return err
			}
			bd.Dismiss(id)
			if err := bd.Save(path); err != nil {
				return err
			}
			Printf("dismissed %s\n", id)
			return nil
		},
	}
}

// --- link (adopt an existing board issue as a phase's tracking issue) ---

func issueLink() *cobra.Command {
	return &cobra.Command{
		Use:   "link <phase-id> <issue-id>",
		Short: "Adopt an existing board issue as a phase's tracking issue",
		Long: "Used by /dross-inbox triage: when an inbound bug/feature issue " +
			"becomes a dross phase, link it so the next `phase-sync` updates that " +
			"issue in place instead of opening a duplicate.",
		Args: cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			id := args[1]
			root, err := FindRoot()
			if err != nil {
				return err
			}
			path := filepath.Join(root, board.File)
			bd, err := board.Load(path)
			if err != nil {
				return err
			}
			bd.SetPhase(args[0], id)
			if err := bd.Save(path); err != nil {
				return err
			}
			Printf("linked phase %s -> issue %s\n", args[0], id)
			return nil
		},
	}
}

// --- list (local link introspection) ---

func issueList() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "Show the current artefact<->issue links from board.json",
		RunE: func(_ *cobra.Command, _ []string) error {
			root, err := FindRoot()
			if err != nil {
				return err
			}
			bd, err := board.Load(filepath.Join(root, board.File))
			if err != nil {
				return err
			}
			if len(bd.Milestones) == 0 && len(bd.Phases) == 0 && len(bd.Quicks) == 0 {
				Print("(no board links yet)")
				return nil
			}
			for v, id := range bd.Milestones {
				Printf("milestone %s -> board %s\n", v, id)
			}
			for p, n := range bd.Phases {
				Printf("phase %s -> issue %s\n", p, n)
			}
			for ref, n := range bd.Quicks {
				Printf("quick %s -> issue %s\n", ref, n)
			}
			if len(bd.Dismissed) > 0 {
				Printf("dismissed: %v\n", bd.Dismissed)
			}
			return nil
		},
	}
}
