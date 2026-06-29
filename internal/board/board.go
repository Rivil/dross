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

// IsLinked reports whether an issue id is already tied to a phase or quick
// task. Milestone ids are excluded on purpose — they live in a different id
// space. Used by inbound triage to skip issues dross already owns.
func (b *Board) IsLinked(issue string) bool {
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
	return false
}

// MarkPulled stamps LastPull with the current time.
func (b *Board) MarkPulled() {
	b.LastPull = time.Now().UTC()
}
