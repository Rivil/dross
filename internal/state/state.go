// Package state handles .dross/state.json — fast-mutating position
// data (current milestone, phase, version, last activity).
//
// Stored as JSON because every command writes here; JSON round-trip
// is trivial and tooling-friendly.
package state

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

const File = "state.json"

type State struct {
	Version            string     `json:"version"` // 4-part major.minor.patch.internal
	CurrentMilestone   string     `json:"current_milestone,omitempty"`
	CurrentPhase       string     `json:"current_phase,omitempty"`
	CurrentPhaseStatus string     `json:"current_phase_status,omitempty"`
	LastActivity       time.Time  `json:"last_activity"`
	LastAction         string     `json:"last_action,omitempty"`
	History            []Activity `json:"history,omitempty"`
}

type Activity struct {
	At     time.Time `json:"at"`
	Action string    `json:"action"`
}

func New() *State {
	return &State{
		Version:      "0.1.0.0",
		LastActivity: time.Now().UTC(),
	}
}

func Load(path string) (*State, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var s State
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, fmt.Errorf("unmarshal state: %w", err)
	}
	return &s, nil
}

func (s *State) Save(path string) error {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// Touch updates LastActivity and appends an Activity row, capped at 50 entries.
func (s *State) Touch(action string) {
	now := time.Now().UTC()
	s.LastActivity = now
	s.LastAction = action
	s.History = append(s.History, Activity{At: now, Action: action})
	if len(s.History) > 50 {
		s.History = s.History[len(s.History)-50:]
	}
}
