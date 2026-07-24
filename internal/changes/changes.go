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

// ParseLandmark parses one --landmark value: key=value pairs
// (feature/symbol/loc/what). A comma starts a NEW pair only when the text
// after it begins with a recognised key followed by '='; any other comma is
// part of the current value, preserved as typed (whitespace trimmed only at
// the value's ends). Each pair splits on its FIRST '=' only, so a value may
// itself contain '=' or '·'. A pair with an empty/unknown key, a first
// segment with no '=', or a duplicate key is an error — never a silent
// empty-key entry and never last-writer-wins.
func ParseLandmark(s string) (Landmark, error) {
	var lm Landmark
	seen := map[string]bool{}
	for _, pair := range splitLandmarkPairs(s) {
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
		if seen[key] {
			return Landmark{}, fmt.Errorf("duplicate landmark key %q in %q", key, s)
		}
		seen[key] = true
		set(&lm, val)
	}
	if len(seen) == 0 {
		return Landmark{}, fmt.Errorf("empty landmark %q", s)
	}
	return lm, nil
}

// splitLandmarkPairs cuts s into pair substrings at each comma whose following
// text (ignoring leading whitespace) starts a recognised landmark key=. Commas
// that don't open a new pair stay inside the current substring verbatim, so
// values round-trip as typed.
func splitLandmarkPairs(s string) []string {
	var pairs []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == ',' && startsLandmarkKey(s[i+1:]) {
			pairs = append(pairs, s[start:i])
			start = i + 1
		}
	}
	return append(pairs, s[start:])
}

// startsLandmarkKey reports whether s (after leading whitespace) begins with a
// recognised landmark key followed by '='.
func startsLandmarkKey(s string) bool {
	eq := strings.IndexByte(s, '=')
	if eq < 0 {
		return false
	}
	return landmarkKeys[strings.TrimSpace(s[:eq])] != nil
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
