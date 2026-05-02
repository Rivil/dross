// Package changes handles .dross/phases/<id>/changes.json — the
// append-only log of what was touched per task during execution.
//
// Stored as JSON (not TOML) because it's machine-written during
// execute, never hand-edited, and JSON round-trip is trivial.
package changes

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

const File = "changes.json"

// FilePath is .dross/phases/<phase-id>/changes.json
func FilePath(root, phaseID string) string {
	return filepath.Join(root, "phases", phaseID, File)
}

type Changes struct {
	Phase string                `json:"phase"`
	Tasks map[string]TaskRecord `json:"tasks"`
}

type TaskRecord struct {
	Files       []string  `json:"files"`
	Commit      string    `json:"commit,omitempty"`
	CompletedAt time.Time `json:"completed_at"`
	Notes       string    `json:"notes,omitempty"`
}

func New(phaseID string) *Changes {
	return &Changes{Phase: phaseID, Tasks: map[string]TaskRecord{}}
}

// Load reads the file. Missing file = empty Changes for the phase, no error.
// (Execute writes the first record on the first task; before that, the file
// legitimately does not exist.)
func Load(path string, phaseID string) (*Changes, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return New(phaseID), nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var c Changes
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("unmarshal changes: %w", err)
	}
	if c.Tasks == nil {
		c.Tasks = map[string]TaskRecord{}
	}
	if c.Phase == "" {
		c.Phase = phaseID
	}
	return &c, nil
}

func (c *Changes) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal changes: %w", err)
	}
	return os.WriteFile(path, b, 0o644)
}

// Record sets the entry for a task. Overwrites on re-execution
// (intentional — re-running a task replaces the prior record).
func (c *Changes) Record(taskID string, files []string, commit, notes string) {
	if c.Tasks == nil {
		c.Tasks = map[string]TaskRecord{}
	}
	c.Tasks[taskID] = TaskRecord{
		Files:       files,
		Commit:      commit,
		CompletedAt: time.Now().UTC(),
		Notes:       notes,
	}
}
