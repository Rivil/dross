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
	"strings"
	"time"
)

const File = "changes.json"

// FilePath is .dross/phases/<phase-id>/changes.json
func FilePath(root, phaseID string) string {
	return filepath.Join(root, "phases", phaseID, File)
}

type Changes struct {
	Phase string                `json:"phase"`
	PR    int                   `json:"pr,omitempty"`
	Tasks map[string]TaskRecord `json:"tasks"`
}

type TaskRecord struct {
	Files       []string   `json:"files"`
	Commit      string     `json:"commit,omitempty"`
	CompletedAt time.Time  `json:"completed_at"`
	Notes       string     `json:"notes,omitempty"`
	Landmarks   []Landmark `json:"landmarks,omitempty"`
}

// Landmark is the durable "what shipped here" for a task, aligned to the
// ARCHITECTURE.md entry template (feature + symbol-link + one-line what).
// /dross-ship merges these into the doc by feature. Replaces the old practice
// of encoding the landmark inside the free-form Notes string.
type Landmark struct {
	Feature string `json:"feature,omitempty"`
	Symbol  string `json:"symbol,omitempty"`
	Loc     string `json:"loc,omitempty"`
	What    string `json:"what,omitempty"`
}

// landmarkKeys is the closed set of recognised landmark fields.
var landmarkKeys = map[string]func(*Landmark, string){
	"feature": func(l *Landmark, v string) { l.Feature = v },
	"symbol":  func(l *Landmark, v string) { l.Symbol = v },
	"loc":     func(l *Landmark, v string) { l.Loc = v },
	"what":    func(l *Landmark, v string) { l.What = v },
}

// ParseLandmark parses one --landmark value: comma-separated key=value pairs
// (feature/symbol/loc/what). Each pair splits on its FIRST '=' only, so a value
// may itself contain '=' or '·'. A pair with no '=' or an empty/unknown key is
// an error — never a silent empty-key entry.
func ParseLandmark(s string) (Landmark, error) {
	var lm Landmark
	seen := false
	for _, pair := range strings.Split(s, ",") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		kv := strings.SplitN(pair, "=", 2)
		if len(kv) != 2 {
			return Landmark{}, fmt.Errorf("landmark pair %q has no '=' (want key=value)", pair)
		}
		key := strings.TrimSpace(kv[0])
		val := strings.TrimSpace(kv[1])
		if key == "" {
			return Landmark{}, fmt.Errorf("landmark pair %q has an empty key", pair)
		}
		set, ok := landmarkKeys[key]
		if !ok {
			return Landmark{}, fmt.Errorf("unknown landmark key %q (want feature/symbol/loc/what)", key)
		}
		set(&lm, val)
		seen = true
	}
	if !seen {
		return Landmark{}, fmt.Errorf("empty landmark %q", s)
	}
	return lm, nil
}

func New(phaseID string) *Changes {
	return &Changes{Phase: phaseID, Tasks: map[string]TaskRecord{}}
}

// SetPR records the opened PR number for a phase in its phase-scoped
// changes.json, loading the existing file (or starting fresh), setting the
// phase-level PR field, and saving. A phase-scoped record can't be dragged
// forward in the cumulative state history the way the "completed <id>"
// completion breadcrumb is, so `dross phase complete` can identify THIS
// phase's PR and gate on its authoritative merge status.
func SetPR(root, phaseID string, pr int) error {
	path := FilePath(root, phaseID)
	c, err := Load(path, phaseID)
	if err != nil {
		return err
	}
	c.PR = pr
	return c.Save(path)
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
func (c *Changes) Record(taskID string, files []string, commit, notes string, landmarks []Landmark) {
	if c.Tasks == nil {
		c.Tasks = map[string]TaskRecord{}
	}
	c.Tasks[taskID] = TaskRecord{
		Files:       files,
		Commit:      commit,
		CompletedAt: time.Now().UTC(),
		Notes:       notes,
		Landmarks:   landmarks,
	}
}
