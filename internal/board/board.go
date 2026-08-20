// Package board manages .dross/board.json — the link registry that ties
// dross planning artefacts to their issue-tracker counterparts.
//
// It deliberately holds nothing but the cross-references (version ->
// milestone id, phase id -> issue id, quick ref -> issue id) plus a
// dismissed set and a last-pull marker. Keeping the links in one file means
// the existing milestone/phase TOML schemas stay untouched, and inbound
// triage has a single place to ask "is this issue already linked to dross
// work?".
//
// Links are keyed by the tracker's readable issue id (a string — e.g. a
// forge issue number "42" or a YouTrack idReadable "PROJ-7"), so the same
// registry serves every backend.
package board

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"regexp"
	"sort"
	"time"
)

// File is the canonical filename inside .dross/.
const File = "board.json"

// Board is the on-disk link registry.
type Board struct {
	// Milestones maps a dross milestone version (e.g. "v0.2") to the tracker
	// milestone entity id (a forge milestone id, or a YouTrack version/epic id).
	Milestones map[string]string `json:"milestones"`
	// Phases maps a phase id (e.g. "02-auth") to its readable issue id.
	Phases map[string]string `json:"phases"`
	// Quicks maps a quick-task ref (e.g. the bumped version "0.2.3.5") to its
	// readable issue id.
	Quicks map[string]string `json:"quicks"`
	// Backlog maps a milestone-backlog item key (e.g. "slug:future-x" or
	// "someday:02-auth#1") to its readable issue id, so backlog sync reconciles
	// the same items instead of duplicating them.
	Backlog map[string]string `json:"backlog,omitempty"`
	// Tasks maps "<phase-id>/<task-id>" to the mirror record for that plan
	// task. Keyed by the pair rather than by the bare task id because task ids
	// are only unique WITHIN a phase — every phase has a t-1, and a bare key
	// would make the second phase's t-1 overwrite the first's mapping and then
	// re-title its issue.
	//
	// The record carries the last-synced PAIR, not just the issue id, and that
	// is what makes an inbound sync possible at all: two current values cannot
	// distinguish "the board moved" from "the plan moved" from "both moved".
	// Recording what each side held when they last agreed makes all three
	// distinguishable. It lives here, in a git-tracked file, so the agreement
	// point travels with the branch and a second machine reaches the same
	// verdict.
	Tasks map[string]TaskLink `json:"tasks,omitempty"`
	// Dismissed holds inbound issue ids the user triaged away; they won't
	// resurface in /dross-inbox.
	Dismissed []string `json:"dismissed,omitempty"`
	// LastPull records when inbound issues were last fetched.
	LastPull time.Time `json:"last_pull,omitempty"`
}

// New returns an empty board with initialised maps.
func New() *Board {
	return &Board{
		Milestones: map[string]string{},
		Phases:     map[string]string{},
		Quicks:     map[string]string{},
	}
}

// Load reads board.json. A missing file is not an error — it returns a fresh
// empty board so callers can load-modify-save without a pre-existence check.
func Load(path string) (*Board, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return New(), nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var bd Board
	if err := json.Unmarshal(b, &bd); err != nil {
		return nil, fmt.Errorf("unmarshal board: %w", err)
	}
	bd.ensureMaps()
	return &bd, nil
}

// UnmarshalJSON accepts both shapes a tasks entry has ever had: the bare issue
// id string every board.json written before board-task-inbound carries, and the
// record written since.
//
// On the type rather than in Load, so no decoder can bypass it — board.json is
// git-tracked, so an old file arrives by pulling a branch, not only by being on
// disk, and a hard failure there would break `dross issue` for anyone who had
// ever synced a task.
//
// A migrated entry carries an issue and NO agreement point, which is the honest
// reading: the pair was never recorded, so nothing is known about what either
// side held when they last agreed. The inbound path treats that as "not yet
// synced with state" rather than inventing an agreement that never happened.
func (l *TaskLink) UnmarshalJSON(data []byte) error {
	var issue string
	if err := json.Unmarshal(data, &issue); err == nil {
		l.Issue = issue
		l.PlanStatus = ""
		l.BoardState = ""
		return nil
	}
	type plain TaskLink // avoid recursing into this method
	var p plain
	if err := json.Unmarshal(data, &p); err != nil {
		return err
	}
	*l = TaskLink(p)
	return nil
}

// Save writes board.json (pretty-printed, overwrites).
func (b *Board) Save(path string) error {
	b.ensureMaps()
	out, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal board: %w", err)
	}
	if err := os.WriteFile(path, out, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func (b *Board) ensureMaps() {
	if b.Milestones == nil {
		b.Milestones = map[string]string{}
	}
	if b.Phases == nil {
		b.Phases = map[string]string{}
	}
	if b.Quicks == nil {
		b.Quicks = map[string]string{}
	}
	if b.Backlog == nil {
		b.Backlog = map[string]string{}
	}
	if b.Tasks == nil {
		b.Tasks = map[string]TaskLink{}
	}
}

// --- links ---

// SetMilestone records the tracker milestone entity id for a dross version.
func (b *Board) SetMilestone(version, id string) {
	b.ensureMaps()
	b.Milestones[version] = id
}

// MilestoneID returns the stored milestone id and whether it's linked.
func (b *Board) MilestoneID(version string) (string, bool) {
	id, ok := b.Milestones[version]
	return id, ok
}

// SetPhase records the readable issue id for a phase id.
func (b *Board) SetPhase(phaseID, issue string) {
	b.ensureMaps()
	b.Phases[phaseID] = issue
}

// PhaseIssue returns the stored issue id for a phase and whether it's linked.
func (b *Board) PhaseIssue(phaseID string) (string, bool) {
	n, ok := b.Phases[phaseID]
	return n, ok
}

// DeletePhase drops a phase's cached issue link, leaving the issue itself
// alone. board.json is a cache, not the mapping's source of truth — the
// tracker is — so an entry that no longer resolves has to be droppable, or a
// stale key would keep shadowing the live issue on every sync.
func (b *Board) DeletePhase(phaseID string) {
	delete(b.Phases, phaseID)
}

// TaskKey is the board key for one plan task. Exported so callers cannot
// re-derive the "<phase>/<task>" shape slightly differently and orphan an
// existing mapping.
func TaskKey(phaseID, taskID string) string { return phaseID + "/" + taskID }

// TaskLink is one plan task's mirror record.
type TaskLink struct {
	// Issue is the readable tracker id.
	Issue string `json:"issue"`
	// PlanStatus and BoardState are what each side held the last time dross
	// synced this task — the agreement point every inbound comparison is made
	// against. Empty means "never synced with a state", which reads as "no
	// agreement recorded" rather than as an empty value either side holds.
	PlanStatus string `json:"plan_status,omitempty"`
	BoardState string `json:"board_state,omitempty"`
}

// SetTask records the readable issue id for one plan task, preserving any
// agreement point already recorded for it. A re-resolve of the issue id must
// not silently discard the snapshot — that would make the next inbound run
// treat a moved card as a first sync and apply it without a conflict check.
func (b *Board) SetTask(phaseID, taskID, issue string) {
	b.ensureMaps()
	key := TaskKey(phaseID, taskID)
	link := b.Tasks[key]
	link.Issue = issue
	b.Tasks[key] = link
}

// SetTaskSynced records the issue id AND the pair both sides held at this sync.
func (b *Board) SetTaskSynced(phaseID, taskID, issue, planStatus, boardState string) {
	b.ensureMaps()
	b.Tasks[TaskKey(phaseID, taskID)] = TaskLink{
		Issue:      issue,
		PlanStatus: planStatus,
		BoardState: boardState,
	}
}

// TaskLinkFor returns the full mirror record for a plan task.
func (b *Board) TaskLinkFor(phaseID, taskID string) (TaskLink, bool) {
	l, ok := b.Tasks[TaskKey(phaseID, taskID)]
	return l, ok
}

// TaskIssue returns the stored issue id for a plan task and whether it's linked.
func (b *Board) TaskIssue(phaseID, taskID string) (string, bool) {
	l, ok := b.Tasks[TaskKey(phaseID, taskID)]
	if !ok || l.Issue == "" {
		return "", false
	}
	return l.Issue, true
}

// SetQuick records the readable issue id for a quick-task ref.
func (b *Board) SetQuick(ref, issue string) {
	b.ensureMaps()
	b.Quicks[ref] = issue
}

// QuickIssue returns the stored issue id for a quick ref and whether it's linked.
func (b *Board) QuickIssue(ref string) (string, bool) {
	n, ok := b.Quicks[ref]
	return n, ok
}

// SetBacklog records the readable issue id for a milestone-backlog item key.
func (b *Board) SetBacklog(key, issue string) {
	b.ensureMaps()
	b.Backlog[key] = issue
}

// BacklogID returns the stored issue id for a backlog item key and whether it's
// linked.
func (b *Board) BacklogID(key string) (string, bool) {
	id, ok := b.Backlog[key]
	return id, ok
}

// BacklogKeys returns the recorded backlog item keys in sorted order. It exists
// for key migrations: a sync that re-keys its items needs to see what shape the
// existing links are in, which BacklogID (a point lookup) can't tell it.
func (b *Board) BacklogKeys() []string {
	keys := make([]string, 0, len(b.Backlog))
	for k := range b.Backlog {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// DeleteBacklog retires a backlog item key, leaving the issue itself alone. It
// is the second half of a re-key: the link is re-recorded under the new key,
// then the old key is dropped so the next sync doesn't consult it again.
func (b *Board) DeleteBacklog(key string) {
	delete(b.Backlog, key)
}

// --- inbound triage ---

// Dismiss marks an inbound issue id as triaged-away (idempotent).
func (b *Board) Dismiss(issue string) {
	if b.IsDismissed(issue) {
		return
	}
	b.Dismissed = append(b.Dismissed, issue)
}

// IsDismissed reports whether an issue id was dismissed.
func (b *Board) IsDismissed(issue string) bool {
	for _, n := range b.Dismissed {
		if n == issue {
			return true
		}
	}
	return false
}

// milestoneIssueShape matches a readable issue key: a project prefix, a dash,
// and a number (DRO-134). It is the shape gate on the Milestones namespace,
// which is the one link slot that does not always hold an issue id.
//
// Package board cannot read [board].milestone_mode — it holds links, not
// config — so the mode is inferred from the value itself. A YouTrack epic
// stores its idReadable and matches; a YouTrack version bundle stores the
// version NAME ("v1.5") and agile mode the board name, and neither does. The
// REST forges and GitHub store a NUMERIC milestone id ("7") that shares an id
// space with their own issue keys — issueResponse.toIssue sets
// Key: strconv.Itoa(r.Number) — so matching it unconditionally would suppress
// the human-filed issue #7 from the inbound feed.
var milestoneIssueShape = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*-[0-9]+$`)

// IsLinked reports whether an issue id is already mirrored by dross in ANY
// namespace — phase, quick, backlog item, plan task, or (when it holds an
// issue-shaped id) milestone. Used by inbound triage to skip issues dross
// itself authored: a mirror that reaches the triage feed asks the user to
// triage dross's own writing back into dross.
//
// An empty id never matches. board.json can hold an empty task issue id, and a
// forge issue can arrive with an empty Key; without the guard those two would
// meet and blank the entire feed.
func (b *Board) IsLinked(issue string) bool {
	if issue == "" {
		return false
	}
	for _, n := range b.Phases {
		if n == issue {
			return true
		}
	}
	for _, n := range b.Quicks {
		if n == issue {
			return true
		}
	}
	for _, n := range b.Backlog {
		if n == issue {
			return true
		}
	}
	// Tasks are matched by .Issue, not by the record: the record also carries
	// the agreement point, which is not an id.
	for _, l := range b.Tasks {
		if l.Issue == issue {
			return true
		}
	}
	for _, n := range b.Milestones {
		if n == issue && milestoneIssueShape.MatchString(n) {
			return true
		}
	}
	return false
}

// MarkPulled stamps LastPull with the current time.
func (b *Board) MarkPulled() {
	b.LastPull = time.Now().UTC()
}
