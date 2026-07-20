// Package watch backs `dross watch` — the read-only digest that surfaces what
// changed on the issue board and in the phase spine since the last tick.
//
// This file owns the delta half: watch.state.json (the seen-set of board
// issues from the previous run) plus the diff that labels each issue in the
// current feed as new (unseen, or reopened) or current (already seen in the
// same open/closed state). Drift classification lives in drift.go.
//
// The state file is the ONLY thing a watch run writes, and it is written
// atomically (temp file + rename, mirroring internal/cmd/env.go) so an
// interrupted or crashing run can never corrupt the baseline that delta
// correctness depends on.
package watch

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// File is the state-file name inside .dross/.
const File = "watch.state.json"

// FilePath is .dross/watch.state.json.
func FilePath(root string) string {
	return filepath.Join(root, File)
}

// Item is one board issue as the diff sees it. Identity is id + open/closed
// state, per the locked delta_identity decision. Title is carried for display
// only and never participates in the diff — a cosmetic retitle is not "new".
type Item struct {
	ID    string `json:"id"`
	State string `json:"state"` // "open" | "closed"
	Title string `json:"title,omitempty"`
}

// Diff is the new/carried split for one tick.
type Diff struct {
	New     []Item `json:"new"`
	Current []Item `json:"current"`
}

// State is the persisted seen-set: issue id -> open/closed state at the last
// run. present records whether a state file actually existed and parsed; it is
// false on the very first run (or after a corrupt file), which the diff reads
// as "seed the baseline, nothing is new yet".
type State struct {
	Issues  map[string]string `json:"issues"`
	present bool
}

// Load reads watch.state.json. A missing file is not an error — it returns an
// empty, not-present baseline so the first run seeds instead of flagging the
// whole backlog as new. A corrupt file degrades the same way rather than
// erroring, so a damaged state file can never wedge the command.
func Load(path string) (*State, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return &State{Issues: map[string]string{}}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var s State
	if err := json.Unmarshal(b, &s); err != nil {
		// Degrade: treat a corrupt baseline as a fresh first run rather than
		// erroring or re-flagging every issue as new.
		return &State{Issues: map[string]string{}}, nil
	}
	if s.Issues == nil {
		s.Issues = map[string]string{}
	}
	s.present = true
	return &s, nil
}

// Diff labels every feed item new or current against the seen-set. An item is
// new only when the baseline is present AND the issue is either unseen or seen
// in a different open/closed state (a reopen). On the first run (or after a
// corrupt baseline) nothing is new — everything is current.
func (s *State) Diff(feed []Item) Diff {
	var d Diff
	for _, it := range feed {
		prev, seen := s.Issues[it.ID]
		if s.present && (!seen || prev != it.State) {
			d.New = append(d.New, it)
		} else {
			d.Current = append(d.Current, it)
		}
	}
	return d
}

// Update replaces the seen-set with the current feed so the next run diffs
// against it, and marks the baseline present.
func (s *State) Update(feed []Item) {
	m := make(map[string]string, len(feed))
	for _, it := range feed {
		m[it.ID] = it.State
	}
	s.Issues = m
	s.present = true
}

// Save writes the state file atomically: marshal, write a temp sibling, then
// rename over the destination. A failure before the rename leaves any existing
// state file byte-identical, so an interrupted run never corrupts the baseline.
func (s *State) Save(path string) error {
	out, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal watch state: %w", err)
	}
	out = append(out, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, out, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("rename %s -> %s: %w", tmp, path, err)
	}
	return nil
}
