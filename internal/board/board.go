// Package board manages .dross/board.json — the link registry that ties
// dross planning artefacts to their issue-tracker counterparts.
//
// It deliberately holds nothing but the cross-references (version ->
// milestone id, phase id -> issue number, quick ref -> issue number) plus a
// dismissed set and a last-pull marker. Keeping the links in one file means
// the existing milestone/phase TOML schemas stay untouched, and inbound
// triage has a single place to ask "is this issue already linked to dross
// work?".
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
	// Milestones maps a dross milestone version (e.g. "v0.2") to the forge
	// milestone id. Note: milestone ids live in a different number space
	// from issue numbers, so they never collide with Phases/Quicks values.
	Milestones map[string]int `json:"milestones"`
	// Phases maps a phase id (e.g. "02-auth") to its issue number.
	Phases map[string]int `json:"phases"`
	// Quicks maps a quick-task ref (e.g. the bumped version "0.2.3.5") to its
	// issue number.
	Quicks map[string]int `json:"quicks"`
	// Dismissed holds inbound issue numbers the user triaged away; they won't
	// resurface in /dross-inbox.
	Dismissed []int `json:"dismissed,omitempty"`
	// LastPull records when inbound issues were last fetched.
	LastPull time.Time `json:"last_pull,omitempty"`
}

// New returns an empty board with initialised maps.
func New() *Board {
	return &Board{
		Milestones: map[string]int{},
		Phases:     map[string]int{},
		Quicks:     map[string]int{},
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
		b.Milestones = map[string]int{}
	}
	if b.Phases == nil {
		b.Phases = map[string]int{}
	}
	if b.Quicks == nil {
		b.Quicks = map[string]int{}
	}
}

// --- links ---

// SetMilestone records the forge milestone id for a dross milestone version.
func (b *Board) SetMilestone(version string, id int) {
	b.ensureMaps()
	b.Milestones[version] = id
}

// MilestoneID returns the stored milestone id and whether it's linked.
func (b *Board) MilestoneID(version string) (int, bool) {
	id, ok := b.Milestones[version]
	return id, ok
}

// SetPhase records the issue number for a phase id.
func (b *Board) SetPhase(phaseID string, issue int) {
	b.ensureMaps()
	b.Phases[phaseID] = issue
}

// PhaseIssue returns the stored issue number for a phase and whether it's linked.
func (b *Board) PhaseIssue(phaseID string) (int, bool) {
	n, ok := b.Phases[phaseID]
	return n, ok
}

// SetQuick records the issue number for a quick-task ref.
func (b *Board) SetQuick(ref string, issue int) {
	b.ensureMaps()
	b.Quicks[ref] = issue
}

// QuickIssue returns the stored issue number for a quick ref and whether it's linked.
func (b *Board) QuickIssue(ref string) (int, bool) {
	n, ok := b.Quicks[ref]
	return n, ok
}

// --- inbound triage ---

// Dismiss marks an inbound issue number as triaged-away (idempotent).
func (b *Board) Dismiss(issue int) {
	if b.IsDismissed(issue) {
		return
	}
	b.Dismissed = append(b.Dismissed, issue)
}

// IsDismissed reports whether an issue number was dismissed.
func (b *Board) IsDismissed(issue int) bool {
	for _, n := range b.Dismissed {
		if n == issue {
			return true
		}
	}
	return false
}

// IsLinked reports whether an issue number is already tied to a phase or quick
// task. Milestone ids are excluded on purpose — they're not issue numbers.
// Used by inbound triage to skip issues dross already owns.
func (b *Board) IsLinked(issue int) bool {
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
