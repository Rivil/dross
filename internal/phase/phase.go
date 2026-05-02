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

// Dir is the conventional path for a phase: phases/NN-slug
func Dir(root, id string) string {
	return filepath.Join(root, "phases", id)
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
	Phase     SpecPhase  `toml:"phase"`
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
	ID            string   `toml:"id"`
	Wave          int      `toml:"wave"`
	Title         string   `toml:"title"`
	Files         []string `toml:"files"`
	Description   string   `toml:"description,omitempty"`
	Covers        []string `toml:"covers,omitempty"`         // criterion ids
	DependsOn     []string `toml:"depends_on,omitempty"`     // task ids
	TestContract  []string `toml:"test_contract,omitempty"`
	Status        string   `toml:"status,omitempty"` // pending | in_progress | done
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
