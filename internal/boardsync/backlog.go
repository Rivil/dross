package boardsync

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/Rivil/dross/internal/configenum"
	"github.com/Rivil/dross/internal/deferred"
	"github.com/Rivil/dross/internal/forge"
	"github.com/Rivil/dross/internal/milestone"
	"github.com/Rivil/dross/internal/phase"
)

// BacklogItem is one milestone-backlog entry to mirror onto the board.
//
// legacyKey is the pre-id positional key (`someday:<source>#<idx>`) a board.json
// written by an older dross may still hold this item's link under. It is
// consulted only when the id key has no link yet, and only after the stored
// issue's title is confirmed to still be this item's — see adoptLegacyBacklogKey.
type BacklogItem struct {
	Key, Title, Body, LegacyKey string

	// Labels is the issue's full label set. The marker is always present; a
	// deferred item adds its identity label, and a routed one its destination.
	Labels []string
	// Identity, when set, is the label the tracker is queried by to resolve
	// this item's existing issue — the same trick resolvePhaseIssue uses, and
	// for the same reason: board.json dies with the phase branch.
	Identity string
	// Target is the destination phase slug of a routed item, "" otherwise. It
	// drives the issue-link attempt; the label carries the routing either way.
	Target string
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
func adoptLegacyBacklogKey(ctx *Ctx, it BacklogItem) bool {
	if it.LegacyKey == "" {
		return false
	}
	issueKey, ok := ctx.Board.BacklogID(it.LegacyKey)
	if !ok {
		return false
	}
	iss, err := ctx.Client.GetIssue(issueKey)
	if err != nil || iss == nil || iss.Title != it.Title {
		return false
	}
	ctx.Board.SetBacklog(it.Key, issueKey)
	ctx.Board.DeleteBacklog(it.LegacyKey)
	return true
}

// syncBacklog mirrors a milestone's backlog — its unscaffolded roadmap phase
// slugs and its deferred ideas, routed or not — onto the board as Open issues
// attached to the milestone entity, recorded in board.json's backlog map.
// Idempotent: re-running updates the same items by their readable-id link.
func SyncBacklog(ctx *Ctx, version string) error {
	m, err := milestone.Load(milestone.FilePath(ctx.Root, version))
	if err != nil {
		return fmt.Errorf("load milestone %q: %w", version, err)
	}

	var items []BacklogItem
	// Unscaffolded roadmap slugs: in milestone.phases with no phase directory.
	for _, slug := range m.Phases {
		if _, err := os.Stat(phase.Dir(ctx.Root, slug)); err == nil {
			continue // scaffolded — tracked by its own phase issue
		}
		items = append(items, BacklogItem{
			Key:    "slug:" + slug,
			Title:  "[backlog] " + slug,
			Body:   fmt.Sprintf("Roadmap phase `%s` in milestone %s — not yet scaffolded.\n\n_Tracked by dross._", slug, version),
			Labels: []string{LabelMarker},
		})
	}
	// Deferred ideas (everything not dismissed). Every item
	// is stamped with a stable id first: the board link keys on it, so an
	// id-less item would have no durable handle to key by.
	deferredItems, err := deferred.EnsureIDs(ctx.Root)
	if err != nil {
		return err
	}
	// Routed items are included: c-6 is precisely that a routed item gets a
	// board issue and stays current. Only a dismissed item has nothing to
	// mirror.
	for _, d := range deferredItems {
		if d.Dismissed {
			continue
		}
		items = append(items, DeferredBacklogItem(d))
	}

	created, updated, err := PushBacklogItems(ctx, version, items)
	if err != nil {
		return err
	}
	closed, err := ReconcileBacklog(ctx, items, deferredItems)
	if err != nil {
		return err
	}
	if err := ctx.Board.Save(ctx.BoardPath); err != nil {
		return err
	}
	fmt.Fprintf(ctx.out(), "backlog %s -> %d created, %d updated, %d closed\n", version, created, updated, closed)
	return nil
}

// BacklogVerdict is what the reconcile pass concluded about one recorded
// mirror. The third value is the load-bearing one: a key this milestone's live
// set does not explain is NOT thereby resolved — it may belong to another
// milestone, or to a slug someone renamed — and closing by set-difference alone
// would resolve work nobody finished.
type BacklogVerdict int

const (
	BacklogStillOpen BacklogVerdict = iota
	BacklogResolved
	BacklogUnattributable
)

// reconcileBacklog is the inbound half of backlog sync: having pushed the live
// set, it walks the recorded mirrors and resolves the ones whose artefact is
// provably done. Without it a backlog issue stays Submitted forever — the slug
// it stood for gets scaffolded into a real phase, that phase ships, and the
// mirror still sits on the board describing work that finished months ago.
//
// It returns the number of mirrors closed. A close that fails is warned about
// and its board.json key KEPT: an unlinked-but-open mirror is unreachable by
// every later run, so dropping the link on failure would strand exactly the
// issue the run was trying to close.
func ReconcileBacklog(ctx *Ctx, live []BacklogItem, items []deferred.Entry) (int, error) {
	liveByKey := make(map[string]BacklogItem, len(live))
	for _, it := range live {
		liveByKey[it.Key] = it
	}
	byDeferredKey := make(map[string]deferred.Entry, len(items))
	for _, d := range items {
		if d.ID != "" {
			byDeferredKey[DeferredBacklogKey(d.ID)] = d
		}
		byDeferredKey[deferred.LegacyBacklogKey(d.Source, d.Index)] = d
	}

	closed := 0
	for _, key := range ctx.Board.BacklogKeys() {
		issue, ok := ctx.Board.BacklogID(key)
		if !ok || issue == "" {
			continue
		}
		verdict := BacklogVerdictFor(ctx, key, liveByKey, byDeferredKey)
		switch verdict {
		case BacklogStillOpen:
			continue
		case BacklogUnattributable:
			// Named rather than swallowed: an unexplained mirror is a real
			// loose end, and the survivor-drain habit is to surface it, not to
			// guess at it.
			fmt.Fprintf(os.Stderr, "warning: backlog mirror %s (%s) is not in this milestone's live set and cannot be shown resolved — leaving it open\n", issue, key)
			continue
		}
		// Idempotence: a mirror the tracker already holds resolved is skipped
		// rather than closed again. A routed item stays in the live set after
		// its target ships, so without this the next run would close it a
		// second time on every sync.
		if done, err := IssueIsDone(ctx, issue); err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not read %s back (%v) — leaving it open\n", issue, err)
			continue
		} else if done {
			continue
		}
		if err := CloseIssue(ctx, issue, ""); err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not close backlog mirror %s (%s): %v — the link is kept so the next run retries\n", issue, key, err)
			continue
		}
		closed++
		// The key is dropped only for a mirror that has LEFT the live set: a
		// live routed item is still backlog, and dropping its link would make
		// the next push mint a duplicate.
		if _, stillLive := liveByKey[key]; !stillLive {
			ctx.Board.DeleteBacklog(key)
		}
	}
	return closed, nil
}

// backlogVerdictFor decides one recorded mirror's fate. Every branch answers
// the same question: can this mirror's artefact be SHOWN to have resolved?
func BacklogVerdictFor(ctx *Ctx, key string, live map[string]BacklogItem, deferred map[string]deferred.Entry) BacklogVerdict {
	if it, ok := live[key]; ok {
		// Still backlog — unless it is routed and its destination has landed,
		// which is the one way a live item's work can already be done.
		if it.Target == "" {
			return BacklogStillOpen
		}
		target := lookupPhaseIssue(ctx, it.Target)
		if target == "" {
			return BacklogStillOpen
		}
		if done, err := IssueIsDone(ctx, target); err == nil && done {
			return BacklogResolved
		}
		return BacklogStillOpen
	}
	if slug, ok := strings.CutPrefix(key, "slug:"); ok {
		// A roadmap slug leaves the live set the moment it is scaffolded, and
		// the phase directory is the proof. A slug that left the set WITHOUT a
		// phase directory was renamed or belongs to another milestone — not
		// resolved.
		if _, err := os.Stat(phase.Dir(ctx.Root, slug)); err == nil {
			return BacklogResolved
		}
		return BacklogUnattributable
	}
	if d, ok := deferred[key]; ok {
		// A dismissed idea is a decision, not a loose end: it left the live set
		// because someone closed it out, so the mirror follows.
		if d.Dismissed {
			return BacklogResolved
		}
		return BacklogStillOpen
	}
	return BacklogUnattributable
}

// boardIssueIsDone reads an issue back and reports the tracker's own verdict,
// using the same split closeBoardIssue verifies on: YouTrack and Jira populate
// Resolved, every other backend populates State.
func IssueIsDone(ctx *Ctx, key string) (bool, error) {
	iss, err := ctx.Client.GetIssue(key)
	if err != nil {
		return false, Wrap(err)
	}
	if iss == nil {
		return false, nil
	}
	return iss.Resolved || iss.State == "closed", nil
}

// deferredBacklogItem renders one deferred entry as a board backlog item. It is
// shared by backlog-sync and `deferred add` so both produce byte-identical
// titles and bodies — a divergence would make an added item's issue flip its
// text on the first sync.
func DeferredBacklogItem(d deferred.Entry) BacklogItem {
	it := BacklogItem{
		Key:       DeferredBacklogKey(d.ID),
		LegacyKey: deferred.LegacyBacklogKey(d.Source, d.Index),
		Title:     "[someday] " + d.Text,
		Body:      fmt.Sprintf("Someday idea (from phase `%s`): %s\n\n_Tracked by dross._", d.Source, d.Text),
		Labels:    []string{LabelMarker},
	}
	if d.ID != "" {
		it.Identity = DeferredLabel(d.ID)
		it.Labels = append(it.Labels, it.Identity)
	}
	if d.Target != "" {
		// A routed item is no longer "someday" — it has a destination, and the
		// title has to say so or the board keeps describing it as unplanned.
		it.Title = "[routed] " + d.Text
		it.Body = fmt.Sprintf("Deferred item routed to `%s` (from phase `%s`): %s\n\n_Tracked by dross._", d.Target, d.Source, d.Text)
		it.Labels = append(it.Labels, TargetLabel(d.Target))
		it.Target = d.Target
	}
	return it
}

// lookupPhaseIssue returns the board issue for a phase, or "" when there is
// none yet. It is resolvePhaseIssue's read-only cousin: no cache healing, no
// legacy summary adoption, and above all no create — a link attempt must never
// mint the very issue it wanted to point at, or a typo'd target would conjure
// a phase issue for a phase that does not exist.
func lookupPhaseIssue(ctx *Ctx, phaseID string) string {
	if key, ok := ctx.Board.PhaseIssue(phaseID); ok {
		return key
	}
	found, err := ctx.Client.ListIssues(forge.IssueFilter{State: "all", Labels: []string{PhaseLabel(phaseID)}})
	if err != nil {
		return ""
	}
	var matches []string
	for _, iss := range found {
		if HasMarker(iss) {
			matches = append(matches, iss.Key)
		}
	}
	if len(matches) == 0 {
		return ""
	}
	sort.Strings(matches)
	return matches[0]
}

// resolveBacklogIssue finds a backlog item's existing issue, or "" if there is
// none. Same three-step shape as resolvePhaseIssue, and for the same reason:
// `deferred add` mirrors the issue at file time, on phase/<id>, whose board.json
// dies with the branch — so after a ship the cache is empty while the issue is
// very much alive, and a create here would duplicate it.
//
//  1. board.json's cached key.
//  2. A tracker query for the item's identity label.
//  3. The pre-id positional link (adoptLegacyBacklogKey), title-verified.
func resolveBacklogIssue(ctx *Ctx, it BacklogItem) (string, error) {
	if key, ok := ctx.Board.BacklogID(it.Key); ok {
		return key, nil
	}
	if it.Identity != "" {
		found, err := ctx.Client.ListIssues(forge.IssueFilter{State: "all", Labels: []string{it.Identity}})
		if err != nil {
			return "", Wrap(err)
		}
		var matches []string
		for _, iss := range found {
			// Same marker post-filter as the phase resolver: an issue that
			// happens to carry the label but is not dross's is not adoptable.
			if HasMarker(iss) {
				matches = append(matches, iss.Key)
			}
		}
		if len(matches) > 0 {
			sort.Strings(matches)
			return matches[0], nil
		}
	}
	if adoptLegacyBacklogKey(ctx, it) {
		key, _ := ctx.Board.BacklogID(it.Key)
		return key, nil
	}
	return "", nil
}

// pushBacklogItems creates or updates each item on the board and records its
// link, returning the created/updated counts. It is the single push seam:
// backlog-sync feeds it a whole milestone's items, `deferred add` feeds it the
// one item it just filed, and both attach to the milestone entity the same way.
func PushBacklogItems(ctx *Ctx, version string, items []BacklogItem) (created, updated int, err error) {
	// Ensure the milestone entity the backlog attaches to (version value / epic
	// / agile board). Version mode tags each item's Fix versions with it.
	entityID, err := EnsureMilestoneLink(ctx, version)
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
	mode := configenum.Normalize(ctx.Proj.Board.MilestoneMode)
	fixVersion := ""
	if mode == "" || mode == "version" {
		fixVersion = entityID
	}

	// c-7: a routed item's issue is linked to its target phase's issue where
	// the provider can express a link. Where it cannot — no IssueLinker, no
	// link type, or the target has no issue yet — the run warns and continues,
	// leaving the dross/target label as the relationship and the item
	// relinkable on a later sync. None of this may fail the backlog sync: the
	// items themselves are the deliverable, the link is an enrichment.
	linker, canLink := ctx.Client.(forge.IssueLinker)
	warnedNoLinker := false
	linkRouted := func(it BacklogItem, itemKey string) {
		if it.Target == "" {
			return
		}
		if !canLink {
			// Once per run, not once per item — a GitHub board would otherwise
			// emit one warning per routed item on every sync.
			if !warnedNoLinker {
				warnedNoLinker = true
				fmt.Fprintf(os.Stderr, "warning: this board provider cannot link issues — routed items keep their dross/target label only\n")
			}
			return
		}
		targetKey := lookupPhaseIssue(ctx, it.Target)
		if targetKey == "" {
			fmt.Fprintf(os.Stderr, "warning: phase %q has no board issue yet — %s keeps its dross/target label and links on a later sync\n", it.Target, itemKey)
			return
		}
		if err := linker.LinkIssues(itemKey, targetKey); err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not link %s to %s: %v\n", itemKey, targetKey, err)
		}
	}

	for _, it := range items {
		key, err := resolveBacklogIssue(ctx, it)
		if err != nil {
			return created, updated, err
		}
		if key != "" {
			title, body, labels := it.Title, it.Body, it.Labels
			patch := forge.IssuePatch{Title: &title, Body: &body}
			if len(labels) > 0 {
				// Sent on every update, not only on create: a re-routed item
				// has to LOSE its old dross/target label, and a label set that
				// is only ever added to would leave the issue claiming both
				// destinations forever.
				patch.Labels = &labels
			}
			if _, err := ctx.Client.UpdateIssue(key, patch); err != nil {
				return created, updated, Wrap(err)
			}
			// Re-record: the key may have come from the tracker rather than
			// the cache, and board.json has to catch up.
			ctx.Board.SetBacklog(it.Key, key)
			linkRouted(it, key)
			updated++
			continue
		}
		labels := it.Labels
		if len(labels) == 0 {
			labels = []string{LabelMarker}
		}
		var iss *forge.Issue
		if yt, ok := ctx.Client.(*forge.YouTrackClient); ok {
			iss, err = yt.CreateBacklogItem(it.Title, it.Body, fixVersion)
			if err == nil && mode == "epic" && entityID != "" {
				// Attach to the Epic entity as a subtask.
				err = yt.LinkSubtask(entityID, iss.Key)
			}
			if err == nil {
				// CreateBacklogItem takes no labels — YouTrack tags are entity
				// writes, applied through the patch path.
				_, err = ctx.Client.UpdateIssue(iss.Key, forge.IssuePatch{Labels: &labels})
			}
		} else {
			ms, _ := strconv.Atoi(entityID)
			iss, err = ctx.Client.CreateIssue(forge.IssueInput{
				Title:     it.Title,
				Body:      it.Body,
				Labels:    labels,
				Milestone: ms,
			})
		}
		if err != nil {
			return created, updated, Wrap(err)
		}
		ctx.Board.SetBacklog(it.Key, iss.Key)
		linkRouted(it, iss.Key)
		created++
	}
	if err := ctx.Board.Save(ctx.BoardPath); err != nil {
		return created, updated, err
	}
	return created, updated, nil
}
