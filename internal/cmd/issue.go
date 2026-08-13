package cmd

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Rivil/dross/internal/board"
	"github.com/Rivil/dross/internal/configenum"
	"github.com/Rivil/dross/internal/forge"
	"github.com/Rivil/dross/internal/hostallow"
	"github.com/Rivil/dross/internal/milestone"
	"github.com/Rivil/dross/internal/phase"
	"github.com/Rivil/dross/internal/project"
)

// Board label vocabulary. A single marker label identifies dross-managed
// issues; one status label tracks lifecycle stage. Keeping it to two labels
// avoids cluttering the board's label list.
//
// Every status literal here is a configenum.LifecycleStatuses member — the same
// set both forge state maps key on. statusPlanned reads "planned", not
// "planning": the maps have always keyed "planned", it is what state.json's
// current_phase_status carries once plan.toml locks, and "planning" wrongly
// implies work in flight. Issues already synced re-label on the next
// phase-sync, which replaces the label set wholesale.
const (
	labelMarker = "dross"
	labelQuick  = "dross/quick"

	statusPlanned    = "planned"
	statusInProgress = "in-progress"
	statusVerifying  = "verifying"
)

func statusLabel(s string) string { return "dross/status:" + s }

// phaseLabel is the durable identity of a phase's board issue. It is what
// makes the TRACKER the source of truth for the phase→issue mapping: board.json
// is git-tracked, so a phase-sync writes it on phase/<id>, which `phase
// complete` then deletes — the mapping never reaches the base branch, and the
// next ship mints a duplicate. A label on the issue survives that, and survives
// a second machine or CI that never had the file.
func phaseLabel(phaseID string) string { return "dross/phase:" + phaseID }

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
		issueMilestoneSync(),
		issueBacklogSync(),
		issuePhaseSync(),
		issueQuick(),
		issuePull(),
		issueDismiss(),
		issueLink(),
		issueList(),
	)
	return c
}

// boardCtx bundles everything a board operation needs.
type boardCtx struct {
	client    forge.BoardClient
	board     *board.Board
	proj      *project.Project
	root      string
	boardPath string
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
	client, err := forge.NewBoard(boardConfig(proj.Board, proj.Remote.URL, extra))
	if err != nil {
		return nil, false, err
	}
	bd, err := board.Load(filepath.Join(root, board.File))
	if err != nil {
		return nil, false, err
	}
	return &boardCtx{
		client:    client,
		board:     bd,
		proj:      proj,
		root:      root,
		boardPath: filepath.Join(root, board.File),
	}, true, nil
}

// boardConfig maps a [board] block onto a forge.Config. base_url is the API
// base; project is the tracker-native identifier — a "owner/repo" path for
// forge backends, the numeric/path project ref for GitLab, the short-name for
// YouTrack. For the forge backends a synthetic URL carries owner/repo to the
// client (the real host is irrelevant — every call targets base_url).
// remoteURL is the derivation source for the API host allowlist, and it is
// [remote].url — NOT the synthetic "https://board.local/<project>" URL below.
// The synthetic URL exists only to carry owner/repo to the forge backends;
// deriving the allowlist from it would authorize a host nobody configured and
// make the policy self-satisfying.
func boardConfig(b project.Board, remoteURL string, extra []string) forge.Config {
	cfg := forge.Config{
		Provider: b.Provider,
		APIBase:  b.BaseURL,
		AuthEnv:  b.AuthEnv,
		AuthUser: b.AuthUser,
		Project:  b.Project,
		BoardID:  b.GitHubProject,
		URL:      "https://board.local/" + b.Project,
		Hosts:    hostallow.Derive(remoteURL, extra),
		Fields: forge.Fields{
			State:       b.Fields.State,
			Type:        b.Fields.Type,
			FixVersions: b.Fields.FixVersions,
		},
	}
	if configenum.Normalize(b.Provider) == "gitlab" {
		cfg.ProjectID = b.Project
	}
	return cfg
}

// wrapBoard tags operational forge errors so telemetry buckets them as
// "board" instead of generic network/other.
func wrapBoard(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("board: %w", err)
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
	return &cobra.Command{
		Use:   "milestone-sync <version>",
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
			id, err := ensureMilestoneLink(ctx, args[0])
			if err != nil {
				return err
			}
			if id == "" {
				return fmt.Errorf("milestone %q not found under .dross/milestones/", args[0])
			}
			if err := ctx.board.Save(ctx.boardPath); err != nil {
				return err
			}
			Printf("milestone %s -> board %s\n", args[0], id)
			return nil
		},
	}
}

// ensureMilestoneLink returns the board milestone id for a dross milestone
// version, creating the board milestone (and storing the link) if needed.
// Returns 0 (no error) when the milestone toml doesn't exist locally, so
// callers can treat "no milestone" as "skip assignment".
func ensureMilestoneLink(ctx *boardCtx, version string) (string, error) {
	if id, ok := ctx.board.MilestoneID(version); ok {
		return id, nil
	}
	m, err := milestone.Load(milestone.FilePath(ctx.root, version))
	if err != nil {
		return "", nil // not found locally — nothing to link
	}
	title := m.Milestone.Title
	if title == "" {
		title = version
	}
	desc := strings.Join(m.Scope.SuccessCriteria, "\n")
	var id string
	switch c := ctx.client.(type) {
	case *forge.YouTrackClient:
		// YouTrack maps a milestone to an entity per [board].milestone_mode
		// (version bundle / agile board / epic), not a forge-style milestone.
		id, err = c.EnsureMilestoneEntity(ctx.proj.Board.MilestoneMode, version, desc)
	case *forge.JiraClient:
		// Jira maps a milestone to a project VERSION (string id via the concrete
		// path). The returned id is numeric, so the phase-sync int-milestone path
		// still attaches it (as a Fix Version) downstream.
		id, err = c.EnsureMilestoneEntity(ctx.proj.Board.MilestoneMode, version, desc)
	default:
		id, err = ctx.client.EnsureMilestone(version, milestoneBody(title, desc))
	}
	if err != nil {
		return "", wrapBoard(err)
	}
	if id == "" {
		return "", nil // backend ensured no entity (e.g. youtrack mode dispatch lands later)
	}
	ctx.board.SetMilestone(version, id)
	return id, nil
}

func milestoneBody(title, criteria string) string {
	if criteria == "" {
		return title
	}
	return title + "\n\nSuccess criteria:\n" + criteria
}

// --- backlog sync ---

func issueBacklogSync() *cobra.Command {
	return &cobra.Command{
		Use:   "backlog-sync <version>",
		Short: "Sync the milestone backlog (unscaffolded slugs + someday ideas) to the board",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			ctx, enabled, err := openBoard()
			if err != nil {
				return err
			}
			if !enabled {
				return nil
			}
			return syncBacklog(ctx, args[0])
		},
	}
}

// backlogItem is one milestone-backlog entry to mirror onto the board.
//
// legacyKey is the pre-id positional key (`someday:<source>#<idx>`) a board.json
// written by an older dross may still hold this item's link under. It is
// consulted only when the id key has no link yet, and only after the stored
// issue's title is confirmed to still be this item's — see adoptLegacyBacklogKey.
type backlogItem struct{ key, title, body, legacyKey string }

// deferredBacklogKey is the board-link key for a deferred item. It keys on the
// item's stable id, never its position: a positional key re-points at whatever
// item slides into the index when an earlier sibling is removed, which silently
// re-titles a live issue to a different finding's text (c-8).
func deferredBacklogKey(id string) string { return "someday:id:" + id }

// legacyDeferredBacklogKey is the positional key dross used before ids existed.
func legacyDeferredBacklogKey(source string, idx int) string {
	return fmt.Sprintf("someday:%s#%d", source, idx)
}

// newDeferredID mints a stable id for a deferred item, retrying on collision
// with an already-assigned one. A collision would silently merge two items' board
// links, so it is checked rather than assumed away.
func newDeferredID(used map[string]bool) (string, error) {
	for attempt := 0; attempt < 8; attempt++ {
		var b [8]byte
		if _, err := rand.Read(b[:]); err != nil {
			return "", fmt.Errorf("generate deferred id: %w", err)
		}
		id := hex.EncodeToString(b[:])
		if !used[id] {
			return id, nil
		}
	}
	return "", fmt.Errorf("could not mint a unique deferred id after 8 attempts")
}

// ensureDeferredIDs backfills an id into every [[deferred]] item that lacks one,
// writing it back to the owning spec or the project store, and returns the
// re-collected entries. Specs authored before ids existed are id-less, so the
// first sync after an upgrade is what gives them a durable identity; a second
// run must find them already stamped and churn nothing.
func ensureDeferredIDs(root string) ([]deferredEntry, error) {
	entries, err := collectDeferred(root)
	if err != nil {
		return nil, err
	}
	used := map[string]bool{}
	missing := map[string][]int{}
	for _, e := range entries {
		if e.ID != "" {
			used[e.ID] = true
			continue
		}
		missing[e.Source] = append(missing[e.Source], e.Index)
	}
	if len(missing) == 0 {
		return entries, nil
	}
	for source, idxs := range missing {
		path, err := deferredStore(root, source)
		if err != nil {
			return nil, err
		}
		spec, err := phase.LoadSpec(path)
		if err != nil {
			return nil, err
		}
		for _, i := range idxs {
			if i < 0 || i >= len(spec.Deferred) {
				continue
			}
			id, err := newDeferredID(used)
			if err != nil {
				return nil, err
			}
			spec.Deferred[i].ID = id
			used[id] = true
		}
		if err := spec.Save(path); err != nil {
			return nil, fmt.Errorf("save %s: %w", path, err)
		}
	}
	return collectDeferred(root)
}

// adoptLegacyBacklogKey migrates a pre-id positional link onto the item's id
// key, so an upgrade re-uses the live issue instead of orphaning it and creating
// a duplicate.
//
// It adopts only after confirming the stored issue still carries this item's
// title. The positional key may already have drifted — if an earlier sibling was
// deleted before the upgrade run, `someday:<phase>#0` names the issue of an item
// that no longer exists, and adopting it blindly would inherit c-8's own fault
// permanently. A title mismatch means "not mine": the link is left alone and the
// item is created fresh.
func adoptLegacyBacklogKey(ctx *boardCtx, it backlogItem) bool {
	if it.legacyKey == "" {
		return false
	}
	issueKey, ok := ctx.board.BacklogID(it.legacyKey)
	if !ok {
		return false
	}
	iss, err := ctx.client.GetIssue(issueKey)
	if err != nil || iss == nil || iss.Title != it.title {
		return false
	}
	ctx.board.SetBacklog(it.key, issueKey)
	ctx.board.DeleteBacklog(it.legacyKey)
	return true
}

// syncBacklog mirrors a milestone's backlog — its unscaffolded roadmap phase
// slugs and unrouted `someday` deferred ideas — onto the board as Open issues
// attached to the milestone entity, recorded in board.json's backlog map.
// Idempotent: re-running updates the same items by their readable-id link.
func syncBacklog(ctx *boardCtx, version string) error {
	m, err := milestone.Load(milestone.FilePath(ctx.root, version))
	if err != nil {
		return fmt.Errorf("load milestone %q: %w", version, err)
	}

	var items []backlogItem
	// Unscaffolded roadmap slugs: in milestone.phases with no phase directory.
	for _, slug := range m.Phases {
		if _, err := os.Stat(phase.Dir(ctx.root, slug)); err == nil {
			continue // scaffolded — tracked by its own phase issue
		}
		items = append(items, backlogItem{
			key:   "slug:" + slug,
			title: "[backlog] " + slug,
			body:  fmt.Sprintf("Roadmap phase `%s` in milestone %s — not yet scaffolded.\n\n_Tracked by dross._", slug, version),
		})
	}
	// Unrouted `someday` deferred ideas (no target, not dismissed). Every item
	// is stamped with a stable id first: the board link keys on it, so an
	// id-less item would have no durable handle to key by.
	deferredItems, err := ensureDeferredIDs(ctx.root)
	if err != nil {
		return err
	}
	for _, d := range deferredItems {
		if d.Target != "" || d.Dismissed {
			continue
		}
		items = append(items, deferredBacklogItem(d))
	}

	created, updated, err := pushBacklogItems(ctx, version, items)
	if err != nil {
		return err
	}
	Printf("backlog %s -> %d created, %d updated\n", version, created, updated)
	return nil
}

// deferredBacklogItem renders one deferred entry as a board backlog item. It is
// shared by backlog-sync and `deferred add` so both produce byte-identical
// titles and bodies — a divergence would make an added item's issue flip its
// text on the first sync.
func deferredBacklogItem(d deferredEntry) backlogItem {
	return backlogItem{
		key:       deferredBacklogKey(d.ID),
		legacyKey: legacyDeferredBacklogKey(d.Source, d.Index),
		title:     "[someday] " + d.Text,
		body:      fmt.Sprintf("Someday idea (from phase `%s`): %s\n\n_Tracked by dross._", d.Source, d.Text),
	}
}

// pushBacklogItems creates or updates each item on the board and records its
// link, returning the created/updated counts. It is the single push seam:
// backlog-sync feeds it a whole milestone's items, `deferred add` feeds it the
// one item it just filed, and both attach to the milestone entity the same way.
func pushBacklogItems(ctx *boardCtx, version string, items []backlogItem) (created, updated int, err error) {
	// Ensure the milestone entity the backlog attaches to (version value / epic
	// / agile board). Version mode tags each item's Fix versions with it.
	entityID, err := ensureMilestoneLink(ctx, version)
	if err != nil {
		return 0, 0, err
	}
	// Per milestone_mode, attach each backlog item to the entity: version mode
	// sets the item's Fix versions to the bundle value; epic mode links it as a
	// subtask of the Epic; agile boards are query/project-based, so an item
	// created in the project already appears on the board (no per-item call).
	// Normalize, not a bare ToLower: doctor accepts a padded " version" now, and
	// an untrimmed read here would silently skip the fixVersion branch for a
	// value the validator just blessed.
	mode := configenum.Normalize(ctx.proj.Board.MilestoneMode)
	fixVersion := ""
	if mode == "" || mode == "version" {
		fixVersion = entityID
	}

	for _, it := range items {
		key, ok := ctx.board.BacklogID(it.key)
		if !ok && adoptLegacyBacklogKey(ctx, it) {
			key, ok = ctx.board.BacklogID(it.key)
		}
		if ok {
			title, body := it.title, it.body
			if _, err := ctx.client.UpdateIssue(key, forge.IssuePatch{Title: &title, Body: &body}); err != nil {
				return created, updated, wrapBoard(err)
			}
			updated++
			continue
		}
		var iss *forge.Issue
		if yt, ok := ctx.client.(*forge.YouTrackClient); ok {
			iss, err = yt.CreateBacklogItem(it.title, it.body, fixVersion)
			if err == nil && mode == "epic" && entityID != "" {
				// Attach to the Epic entity as a subtask.
				err = yt.LinkSubtask(entityID, iss.Key)
			}
		} else {
			ms, _ := strconv.Atoi(entityID)
			iss, err = ctx.client.CreateIssue(forge.IssueInput{
				Title:     it.title,
				Body:      it.body,
				Labels:    []string{labelMarker},
				Milestone: ms,
			})
		}
		if err != nil {
			return created, updated, wrapBoard(err)
		}
		ctx.board.SetBacklog(it.key, iss.Key)
		created++
	}
	if err := ctx.board.Save(ctx.boardPath); err != nil {
		return created, updated, err
	}
	return created, updated, nil
}

// --- phase sync ---

func issuePhaseSync() *cobra.Command {
	var status string
	var doClose bool
	c := &cobra.Command{
		Use:   "phase-sync <phase-id>",
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
				// Assign the normalized form back. syncPhase passes status raw
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
			return syncPhase(ctx, args[0], status, doClose)
		},
	}
	c.Flags().StringVar(&status, "status", "", "lifecycle status label ("+configenum.LifecycleStatuses.List()+"); derived from the plan if unset")
	c.Flags().BoolVar(&doClose, "close", false, "close the issue (use on ship)")
	return c
}

// hasMarker reports whether an issue carries the dross marker label. Every
// adoption path post-filters on it: the tracker query is by phase label alone
// (see resolvePhaseIssue), and a hand-made issue that happens to carry that
// label must not be silently taken over.
func hasMarker(iss forge.Issue) bool {
	for _, l := range iss.Labels {
		if l == labelMarker {
			return true
		}
	}
	return false
}

// resolvePhaseIssue finds the phase's existing board issue, or "" if there is
// none yet. Resolution order:
//
//  1. board.json's cached key, verified against the tracker. A key that no
//     longer resolves is dropped rather than trusted — the file is a cache.
//  2. A tracker query for the phase label. This is the durable mapping: it
//     survives the deleted phase branch that takes board.json with it.
//  3. A legacy issue carrying the marker and the exact `<id> — <title>`
//     summary, from before phase labels existed. Adopting it (the caller
//     back-fills the phase label on update) is what stops one duplicate per
//     pre-existing phase the first time this runs.
//
// The query filters on ONE label. Two would be OR'd now (c-1), which would
// adopt an arbitrary dross issue; and State must be "all", because an
// already-closed phase issue is exactly the one a re-ship must find.
func resolvePhaseIssue(ctx *boardCtx, phaseID, title string) (string, error) {
	if key, ok := ctx.board.PhaseIssue(phaseID); ok {
		if iss, err := ctx.client.GetIssue(key); err == nil && iss != nil && hasMarker(*iss) {
			return key, nil
		}
		fmt.Fprintf(os.Stderr, "warning: board.json points %s at %s, which no longer resolves — re-resolving from the tracker\n", phaseID, key)
		ctx.board.DeletePhase(phaseID)
	}

	byLabel, err := ctx.client.ListIssues(forge.IssueFilter{State: "all", Labels: []string{phaseLabel(phaseID)}})
	if err != nil {
		return "", wrapBoard(err)
	}
	var matches []string
	for _, iss := range byLabel {
		if hasMarker(iss) {
			matches = append(matches, iss.Key)
		}
	}
	if len(matches) > 0 {
		sort.Strings(matches)
		if len(matches) > 1 {
			fmt.Fprintf(os.Stderr, "warning: %d issues carry %s (%s) — updating %s and leaving the rest\n",
				len(matches), phaseLabel(phaseID), strings.Join(matches, ", "), matches[0])
		}
		return matches[0], nil
	}

	// Legacy adoption: issues synced before phase labels existed are only
	// identifiable by their summary.
	byMarker, err := ctx.client.ListIssues(forge.IssueFilter{State: "all", Labels: []string{labelMarker}})
	if err != nil {
		return "", wrapBoard(err)
	}
	for _, iss := range byMarker {
		if iss.Title == title && hasMarker(iss) {
			return iss.Key, nil
		}
	}
	return "", nil
}

func syncPhase(ctx *boardCtx, phaseID, status string, doClose bool) error {
	dir := phase.Dir(ctx.root, phaseID)
	spec, err := phase.LoadSpec(filepath.Join(dir, "spec.toml"))
	if err != nil {
		return fmt.Errorf("load spec for %s: %w", phaseID, err)
	}
	// plan.toml may not exist yet (sync at spec time) — tolerate it.
	plan, _ := phase.LoadPlan(filepath.Join(dir, "plan.toml"))

	if status == "" {
		status = derivePhaseStatus(plan)
	}

	title := fmt.Sprintf("%s — %s", phaseID, spec.Phase.Title)
	body := renderPhaseBody(phaseID, spec, plan)
	labels := []string{labelMarker, phaseLabel(phaseID), statusLabel(status)}

	// Assign to the milestone if the phase declares one and it's syncable.
	// IssueInput.Milestone is the forge int id; the board stores it as a string.
	milestoneID := 0
	if spec.Phase.Milestone != "" {
		id, err := ensureMilestoneLink(ctx, spec.Phase.Milestone)
		if err != nil {
			return err
		}
		milestoneID, _ = strconv.Atoi(id) // "" / non-numeric (youtrack entity) → 0, unassigned
	}

	key, err := resolvePhaseIssue(ctx, phaseID, title)
	if err != nil {
		return err
	}
	created := key == ""
	if created {
		iss, err := ctx.client.CreateIssue(forge.IssueInput{
			Title:     title,
			Body:      body,
			Labels:    labels,
			Milestone: milestoneID,
		})
		if err != nil {
			return wrapBoard(err)
		}
		ctx.board.SetPhase(phaseID, iss.Key)
		key = iss.Key
	} else {
		patch := forge.IssuePatch{Title: &title, Body: &body, Labels: &labels}
		if milestoneID > 0 {
			patch.Milestone = &milestoneID
		}
		if doClose {
			closed := "closed"
			patch.State = &closed
		}
		if _, err := ctx.client.UpdateIssue(key, patch); err != nil {
			return wrapBoard(err)
		}
		// Re-record: the key may have come from the tracker rather than the
		// cache (an adopted or re-resolved issue), and board.json has to catch
		// up or the next run pays for the lookup again.
		ctx.board.SetPhase(phaseID, key)
	}
	// YouTrack tracks lifecycle on the State custom field (not a status label),
	// mapped via the default map overridden by [board].state_map. An unmapped
	// state warns and skips inside SetState without failing the sync.
	if yt, ok := ctx.client.(*forge.YouTrackClient); ok && status != "" {
		if err := yt.SetState(key, status, ctx.proj.Board.StateMap); err != nil {
			return wrapBoard(err)
		}
	}
	// Jira tracks lifecycle by moving the issue through a workflow transition,
	// mapped via the default map overridden by [board].state_map. An unmapped
	// state (or a target with no available transition) warns and skips inside
	// SetState without failing the sync.
	if jr, ok := ctx.client.(*forge.JiraClient); ok && status != "" {
		if err := jr.SetState(key, status, ctx.proj.Board.StateMap); err != nil {
			return wrapBoard(err)
		}
	}
	// Close-on-create edge: created above then asked to close.
	if doClose && created {
		if err := ctx.client.CloseIssue(key); err != nil {
			return wrapBoard(err)
		}
	}
	if err := ctx.board.Save(ctx.boardPath); err != nil {
		return err
	}

	state := status
	if doClose {
		state = "closed"
	}
	Printf("phase %s -> board %s (%s)\n", phaseID, key, state)
	return nil
}

// derivePhaseStatus maps plan progress onto a lifecycle label. Its return
// values are the emitted half of configenum.LifecycleStatuses — the other half
// comes from the `--status <literal>` calls in assets/prompts/*.md.
func derivePhaseStatus(plan *phase.Plan) string {
	if plan == nil || len(plan.Task) == 0 {
		return statusPlanned
	}
	_, inProgress, done, failed := plan.Summary()
	if done == 0 && inProgress == 0 && failed == 0 {
		return statusPlanned
	}
	return statusInProgress
}

// renderPhaseBody builds the issue body: a header, then a task checklist
// mirroring plan.toml task statuses, then a "managed by dross" footer.
func renderPhaseBody(phaseID string, spec *phase.Spec, plan *phase.Plan) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("**Phase:** `%s`", phaseID))
	if spec.Phase.Milestone != "" {
		b.WriteString(fmt.Sprintf("  ·  **Milestone:** %s", spec.Phase.Milestone))
	}
	b.WriteString("\n")

	if len(spec.Criteria) > 0 {
		b.WriteString("\n### Acceptance criteria\n")
		for _, cr := range spec.Criteria {
			b.WriteString(fmt.Sprintf("- %s\n", cr.Text))
		}
	}

	if plan != nil && len(plan.Task) > 0 {
		b.WriteString("\n### Tasks\n")
		for _, t := range plan.Task {
			box := " "
			if t.Status == phase.StatusDone {
				box = "x"
			}
			b.WriteString(fmt.Sprintf("- [%s] %s — %s\n", box, t.ID, t.Title))
		}
	}

	b.WriteString("\n---\n_Tracked by dross. Edit the phase artefacts under ")
	b.WriteString(fmt.Sprintf("`.dross/phases/%s/`, not this issue body._\n", phaseID))
	return b.String()
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
				key, ok := ctx.board.QuickIssue(ref)
				if !ok {
					return fmt.Errorf("no board issue linked to quick ref %q", ref)
				}
				if err := ctx.client.CloseIssue(key); err != nil {
					return wrapBoard(err)
				}
				Printf("quick %s -> board %s (closed)\n", ref, key)
				return nil
			}

			if len(args) < 2 {
				return fmt.Errorf("a title is required to open a quick issue")
			}
			iss, err := ctx.client.CreateIssue(forge.IssueInput{
				Title:  args[1],
				Body:   fmt.Sprintf("Quick task `%s`.\n\n_Tracked by dross._", ref),
				Labels: []string{labelMarker, labelQuick},
			})
			if err != nil {
				return wrapBoard(err)
			}
			ctx.board.SetQuick(ref, iss.Key)
			if err := ctx.board.Save(ctx.boardPath); err != nil {
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

// collectInbound returns the board's inbound triage feed: issues matching the
// filter, minus those already linked to a phase/quick or dismissed. It is
// deliberately MARK-FREE — it never stamps last_pull — so read-only callers
// (dross watch, dross status) share one filter path with `issue pull` and can
// never re-introduce a board mutation. The filter's State scopes the feed;
// callers wanting reopen-resurfaces semantics pass State:"open".
func collectInbound(ctx *boardCtx, filter forge.IssueFilter) ([]forge.Issue, error) {
	issues, err := ctx.client.ListIssues(filter)
	if err != nil {
		return nil, wrapBoard(err)
	}
	var inbound []forge.Issue
	for _, iss := range issues {
		if ctx.board.IsLinked(iss.Key) || ctx.board.IsDismissed(iss.Key) {
			continue
		}
		inbound = append(inbound, iss)
	}
	return inbound, nil
}

// pullEnvelope is the `issue pull --json` shape. It exists so a board that
// could not be reached is distinguishable from a board with nothing on it: a
// bare array collapses both onto `[]`, and every prompt then reports zero
// inbound issues for a tracker that is simply down.
//
// Issues is never null — an empty feed is `[]` — and Error is null on success.
type pullEnvelope struct {
	Issues []forge.Issue `json:"issues"`
	Error  *string       `json:"error"`
}

// emitPullEnvelope marshals and prints the envelope. Issues is normalised to a
// non-nil slice so consumers can index it without a null check.
func emitPullEnvelope(issues []forge.Issue, boardErr error) error {
	env := pullEnvelope{Issues: issues}
	if env.Issues == nil {
		env.Issues = []forge.Issue{}
	}
	if boardErr != nil {
		msg := boardErr.Error()
		env.Error = &msg
	}
	out, err := json.Marshal(env)
	if err != nil {
		return err
	}
	Print(string(out))
	return nil
}

func issuePull() *cobra.Command {
	var labels, state string
	var asJSON, mark bool
	c := &cobra.Command{
		Use:   "pull",
		Short: "List open board issues not yet linked to dross work (inbound triage)",
		RunE: func(_ *cobra.Command, _ []string) error {
			ctx, enabled, err := openBoard()
			if err != nil {
				return err
			}
			if !enabled {
				if asJSON {
					return emitPullEnvelope(nil, nil)
				}
				return nil
			}
			filter := forge.IssueFilter{State: state}
			if labels != "" {
				filter.Labels = splitCSV(labels)
			}
			inbound, boardErr := collectInbound(ctx, filter)
			if boardErr != nil {
				// A fetch failure is reported, not raised: the workflow
				// prompts call `dross issue …` unconditionally on the promise
				// that it is a safe no-op, so a non-zero exit would break
				// that contract. The signal travels in the payload instead.
				if asJSON {
					return emitPullEnvelope(nil, boardErr)
				}
				Printf("board unreachable: %v\n", boardErr)
				return nil
			}

			// Read-only by default so /dross-status can poll without
			// mutating .dross. --mark stamps last_pull (used by /dross-inbox).
			// Only a pull that actually happened is worth stamping — marking
			// a failed fetch is the silent-zero fault wearing a different hat.
			if mark {
				ctx.board.MarkPulled()
				if err := ctx.board.Save(ctx.boardPath); err != nil {
					return err
				}
			}

			if asJSON {
				return emitPullEnvelope(inbound, nil)
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
