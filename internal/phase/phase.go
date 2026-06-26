// Package phase handles per-phase artefacts inside .dross/phases/NN-slug/.
//
// A phase has up to 5 files:
//   - spec.toml     — acceptance criteria and locked decisions (input)
//   - plan.toml     — tasks, waves, dependencies, test contracts (input)
//   - changes.json  — files+symbols touched per task (auto, written by execute)
//   - tests.json    — criterion→test map + mutation results (auto, written by verify)
//   - verify.toml   — goal-backward verdict (auto, written by verify)
//
// changes.json and tests.json are JSON because they're machine-written
// during execute/verify. Specs and plans are TOML so they're human-editable.
package phase

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

// Dir resolves a phase id to its on-disk directory under phases/.
//
// Identity is the bare slug (e.g. phases/auth). For back-compat, a legacy
// NN-slug id (e.g. "01-auth") still resolves: if the literal directory is
// absent, Dir falls back to the prefix-stripped slug when that directory
// exists. When neither exists it returns the literal phases/<id> unchanged,
// so callers that build a path for a not-yet-created phase are unaffected.
func Dir(root, id string) string {
	literal := filepath.Join(root, "phases", id)
	if _, err := os.Stat(literal); err == nil {
		return literal
	}
	if stripped := StripLegacyPrefix(id); stripped != id {
		if alt := filepath.Join(root, "phases", stripped); statDir(alt) {
			return alt
		}
	}
	return literal
}

func statDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// StripLegacyPrefix removes a leading ordinal prefix ("NN-") from a phase id,
// leaving the slug. A non-numeric leading segment is left untouched, so it is
// a no-op on ids that are already bare slugs.
//
//	StripLegacyPrefix("03-fix-foo") == "fix-foo"
//	StripLegacyPrefix("fix-foo")    == "fix-foo"
func StripLegacyPrefix(id string) string {
	i := strings.IndexByte(id, '-')
	if i <= 0 {
		return id
	}
	for _, r := range id[:i] {
		if r < '0' || r > '9' {
			return id
		}
	}
	return id[i+1:]
}

// Ordered returns the phase dirs ordered by their position in a milestone's
// phases array. Entries present in order and on disk come first, in array
// order; orphan dirs (on disk but in no array) are appended, sorted, never
// dropped. A stale array entry (in the array but with no dir) is skipped —
// there is nothing on disk to list for it.
func Ordered(order, dirs []string) []string {
	onDisk := make(map[string]bool, len(dirs))
	for _, d := range dirs {
		onDisk[d] = true
	}
	out := make([]string, 0, len(dirs))
	placed := make(map[string]bool, len(dirs))
	for _, o := range order {
		if onDisk[o] {
			out = append(out, o)
			placed[o] = true
		}
	}
	var orphans []string
	for _, d := range dirs {
		if !placed[d] {
			orphans = append(orphans, d)
		}
	}
	sort.Strings(orphans)
	return append(out, orphans...)
}

// DisplayNumber is the 1-based position of slug within a milestone's phases
// array, or 0 if it is not in the array. The number is derived from array
// position, so reordering the array changes it.
func DisplayNumber(order []string, slug string) int {
	for i, o := range order {
		if o == slug {
			return i + 1
		}
	}
	return 0
}

// UniqueSlug slugifies title and, if a phase directory by that slug already
// exists under root, appends "-2", "-3", … until it finds a free name.
func UniqueSlug(root, title string) string {
	base := Slugify(title)
	if !statDir(filepath.Join(root, "phases", base)) {
		return base
	}
	for i := 2; ; i++ {
		cand := fmt.Sprintf("%s-%d", base, i)
		if !statDir(filepath.Join(root, "phases", cand)) {
			return cand
		}
	}
}

// Slugify converts a free-form title into a directory-safe slug.
// e.g. "Meal Tagging System" → "meal-tagging-system".
func Slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	prevDash := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevDash = false
		default:
			if !prevDash && b.Len() > 0 {
				b.WriteRune('-')
				prevDash = true
			}
		}
	}
	return strings.TrimRight(b.String(), "-")
}

// List returns phase directory names (e.g. "01-auth") sorted.
func List(root string) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(root, "phases"))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}

// Spec is the acceptance contract for a phase.
type Spec struct {
	Phase     SpecPhase   `toml:"phase"`
	Criteria  []Criterion `toml:"criteria"`
	Decisions []Decision  `toml:"decisions,omitempty"`
	Deferred  []Deferred  `toml:"deferred,omitempty"`
}

type SpecPhase struct {
	ID        string `toml:"id"`
	Title     string `toml:"title"`
	Milestone string `toml:"milestone,omitempty"`
}

type Criterion struct {
	ID   string `toml:"id"`
	Text string `toml:"text"`
}

type Decision struct {
	Key    string `toml:"key"`
	Choice string `toml:"choice"`
	Why    string `toml:"why"`
	Locked bool   `toml:"locked,omitempty"`
}

type Deferred struct {
	Text string `toml:"text"`
	Why  string `toml:"why,omitempty"`
	// Target routes the deferred item to a destination: a phase slug it should
	// re-surface in. Empty means "someday" — unrouted, awaiting triage.
	Target string `toml:"target,omitempty"`
}

// Plan is the task graph for a phase.
type Plan struct {
	Phase PlanPhase `toml:"phase"`
	Task  []Task    `toml:"task"` // ordered
}

type PlanPhase struct {
	ID string `toml:"id"`
}

type Task struct {
	ID           string   `toml:"id"`
	Wave         int      `toml:"wave"`
	Title        string   `toml:"title"`
	Files        []string `toml:"files"`
	Description  string   `toml:"description,omitempty"`
	Covers       []string `toml:"covers,omitempty"`     // criterion ids
	DependsOn    []string `toml:"depends_on,omitempty"` // task ids
	TestContract []string `toml:"test_contract,omitempty"`
	Status       string   `toml:"status,omitempty"` // pending | in_progress | done | failed
}

// Task statuses.
const (
	StatusPending    = "pending"
	StatusInProgress = "in_progress"
	StatusDone       = "done"
	StatusFailed     = "failed"
)

// NextRunnable returns the next task with status==pending whose
// dependencies are all done, picked by lowest wave then by id.
// Returns nil if nothing is runnable (all done, all blocked, or empty plan).
func (p *Plan) NextRunnable() *Task {
	doneSet := map[string]bool{}
	for _, t := range p.Task {
		if t.Status == StatusDone {
			doneSet[t.ID] = true
		}
	}
	var best *Task
	for i := range p.Task {
		t := &p.Task[i]
		if t.Status != "" && t.Status != StatusPending {
			continue
		}
		blocked := false
		for _, dep := range t.DependsOn {
			if !doneSet[dep] {
				blocked = true
				break
			}
		}
		if blocked {
			continue
		}
		if best == nil ||
			t.Wave < best.Wave ||
			(t.Wave == best.Wave && t.ID < best.ID) {
			best = t
		}
	}
	return best
}

// SetTaskStatus mutates the status of a task by id.
// Returns false if the task is not found.
func (p *Plan) SetTaskStatus(id, status string) bool {
	for i := range p.Task {
		if p.Task[i].ID == id {
			p.Task[i].Status = status
			return true
		}
	}
	return false
}

// FindTask returns a pointer to a task in-place, or nil.
func (p *Plan) FindTask(id string) *Task {
	for i := range p.Task {
		if p.Task[i].ID == id {
			return &p.Task[i]
		}
	}
	return nil
}

// Summary counts tasks by status. Useful for /dross-execute wrap-up.
func (p *Plan) Summary() (pending, inProgress, done, failed int) {
	for _, t := range p.Task {
		switch t.Status {
		case StatusInProgress:
			inProgress++
		case StatusDone:
			done++
		case StatusFailed:
			failed++
		default:
			pending++
		}
	}
	return
}

func LoadSpec(path string) (*Spec, error) {
	var s Spec
	if _, err := toml.DecodeFile(path, &s); err != nil {
		return nil, fmt.Errorf("decode spec %s: %w", path, err)
	}
	return &s, nil
}

func (s *Spec) Save(path string) error { return saveTOML(path, s) }

func LoadPlan(path string) (*Plan, error) {
	var p Plan
	if _, err := toml.DecodeFile(path, &p); err != nil {
		return nil, fmt.Errorf("decode plan %s: %w", path, err)
	}
	return &p, nil
}

func (p *Plan) Save(path string) error { return saveTOML(path, p) }

func saveTOML(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := toml.NewEncoder(f)
	enc.Indent = "  "
	return enc.Encode(v)
}
