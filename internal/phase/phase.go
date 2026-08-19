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
	"slices"
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
//
// A slug appearing more than once in order is emitted once, at its FIRST
// position. Callers concatenate every milestone's phases array in version
// order, so a phase carried forward onto a later milestone's roadmap — a
// legitimate re-scope — appears twice in the input; it is one directory and
// must list as one line. The earlier position wins because that is where the
// phase actually sits in the project's sequence. `dross doctor` names any such
// slug, so the ambiguity stays findable rather than being silently smoothed.
func Ordered(order, dirs []string) []string {
	onDisk := make(map[string]bool, len(dirs))
	for _, d := range dirs {
		onDisk[d] = true
	}
	out := make([]string, 0, len(dirs))
	placed := make(map[string]bool, len(dirs))
	for _, o := range order {
		if onDisk[o] && !placed[o] {
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

// ErrAnchorNotFound is returned by the relative array-order helpers when the
// anchor slug they were asked to position against is absent from the array.
var ErrAnchorNotFound = errors.New("anchor slug not found in milestone phases array")

// InsertRelative returns a copy of arr with slug placed immediately before or
// after anchor (before=true → before it). When anchor is absent it returns
// ErrAnchorNotFound rather than appending at the tail; arr is never mutated.
func InsertRelative(arr []string, slug, anchor string, before bool) ([]string, error) {
	i := slices.Index(arr, anchor)
	if i < 0 {
		return nil, ErrAnchorNotFound
	}
	pos := i + 1
	if before {
		pos = i
	}
	return slices.Insert(slices.Clone(arr), pos, slug), nil
}

// MoveRelative returns a copy of arr with slug repositioned immediately before
// or after anchor, preserving the relative order of every other element. Moving
// slug to the position it already holds returns arr unchanged — the no-op the
// caller reports as "already there". Returns ErrAnchorNotFound if anchor is
// absent, or an error if slug itself is not in arr.
func MoveRelative(arr []string, slug, anchor string, before bool) ([]string, error) {
	from := slices.Index(arr, slug)
	if from < 0 {
		return nil, fmt.Errorf("phase %q is not in the milestone phases array", slug)
	}
	if !slices.Contains(arr, anchor) {
		return nil, ErrAnchorNotFound
	}
	reduced := slices.Delete(slices.Clone(arr), from, from+1)
	out, err := InsertRelative(reduced, slug, anchor, before)
	if err != nil {
		return nil, err
	}
	if slices.Equal(out, arr) {
		return arr, nil // already in place
	}
	return out, nil
}

// RenameInArray returns a copy of arr with oldSlug replaced by newSlug in place,
// preserving its index and the slice length. When oldSlug is absent the copy is
// returned unchanged (callers validate existence separately).
func RenameInArray(arr []string, oldSlug, newSlug string) []string {
	out := slices.Clone(arr)
	for i, s := range out {
		if s == oldSlug {
			out[i] = newSlug
		}
	}
	return out
}

// UniqueSlug slugifies title and, if a phase directory by that slug already
// exists under root, appends "-2", "-3", … until it finds a free name.
// SlugDisposition is what CreateSlug decided about a title.
type SlugDisposition int

const (
	// SlugFree — no phase directory of this slug exists. Create it.
	SlugFree SlugDisposition = iota
	// SlugAdopt — a directory exists but holds none of the files the loop
	// writes as a phase begins. It is a placeholder — the shape
	// `deferred route --target` and `milestone add phases` leave behind — and
	// the caller should adopt it rather than coin a near-identical slug.
	SlugAdopt
	// SlugOccupied — a directory exists and holds real work. The caller must
	// refuse: adopting would retitle someone's in-flight phase, and coining
	// `<slug>-2` is the behaviour this replaced.
	SlugOccupied
)

// startedMarkers are the files the loop writes as a phase begins. A directory
// holding none of them is a placeholder.
//
// Keyed on THESE files rather than on the directory being empty, and the
// difference matters in both directions: emptiness would let a stray file (an
// editor swap file, a README someone dropped in) make a placeholder look
// occupied, which re-opens the duplicate-coining path — while a marker file
// present is unambiguous evidence someone started the phase.
var startedMarkers = []string{"spec.toml", "plan.toml", "changes.json"}

// CreateSlug resolves a title to the slug `dross phase create` should use, and
// what to do about it.
//
// It replaces UniqueSlug's unconditional coining. That coining is the bug this
// exists to remove: `phase create "Survivor drain"` over an existing
// survivor-drain/ produced survivor-drain-2, appended THAT to the milestone's
// phases array and cut a branch for it — so the roadmap entry someone
// scaffolded and the phase they then started were two different phases
// separated by a trailing digit nobody chose.
func CreateSlug(root, title string) (slug string, d SlugDisposition) {
	slug = Slugify(title)
	dir := filepath.Join(root, "phases", slug)
	if !statDir(dir) {
		return slug, SlugFree
	}
	for _, marker := range startedMarkers {
		if _, err := os.Stat(filepath.Join(dir, marker)); err == nil {
			return slug, SlugOccupied
		}
	}
	return slug, SlugAdopt
}

// UniqueSlug slugifies title and coins a numbered suffix when the slug is
// taken.
//
// Retained for the lifecycle verbs that genuinely need a free slug. `dross
// phase create` does NOT use it any more — see CreateSlug for why.
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
	Phase     SpecPhase   `toml:"phase" json:"phase"`
	Criteria  []Criterion `toml:"criteria" json:"criteria"`
	Decisions []Decision  `toml:"decisions,omitempty" json:"decisions,omitempty"`
	Deferred  []Deferred  `toml:"deferred,omitempty" json:"deferred,omitempty"`
}

type SpecPhase struct {
	ID        string `toml:"id" json:"id"`
	Title     string `toml:"title" json:"title"`
	Milestone string `toml:"milestone,omitempty" json:"milestone,omitempty"`
}

type Criterion struct {
	ID   string `toml:"id" json:"id"`
	Text string `toml:"text" json:"text"`
}

type Decision struct {
	Key    string `toml:"key" json:"key"`
	Choice string `toml:"choice" json:"choice"`
	Why    string `toml:"why" json:"why"`
	Locked bool   `toml:"locked,omitempty" json:"locked,omitempty"`
}

type Deferred struct {
	// ID is a stable internal identity for the item, assigned when it is filed
	// (or backfilled on first board sync). It keys the item's issue-board
	// backlog entry so a board link survives its neighbours being removed —
	// positional keys re-point at whatever slid into the index. It is
	// deliberately not an addressing handle: every CLI verb still addresses an
	// item by `<source> <idx>` (the locked deferred_identity decision).
	// omitempty so specs written before ids existed stay byte-clean.
	ID   string `toml:"id,omitempty" json:"id,omitempty"`
	Text string `toml:"text" json:"text"`
	Why  string `toml:"why,omitempty" json:"why,omitempty"`
	// Target routes the deferred item to a destination: a phase slug it should
	// re-surface in. Empty means "someday" — unrouted, awaiting triage.
	Target string `toml:"target,omitempty" json:"target,omitempty"`
	// Dismissed retires the item to a "dismissed" state — triaged as
	// wontfix/done without a target. It is a third state distinct from
	// "someday" (no target, not dismissed) and "routed" (target set); dismiss
	// is someday-only, so a dismissed entry never carries a target.
	Dismissed bool `toml:"dismissed,omitempty" json:"dismissed,omitempty"`
	// Survivor carries the identity key of a routed surviving mutant
	// (internal/survivor). The locked routed_state_source decision routes a
	// survivor through this machinery rather than a parallel one, so a routed
	// survivor re-surfaces on the destination phase's slate like any other
	// parked item — and stays queryable as a survivor rather than dissolving
	// into the prose of Text. Empty on ordinary deferred items.
	Survivor string `toml:"survivor,omitempty" json:"survivor,omitempty"`
}

// Plan is the task graph for a phase.
//
// TaskSeq is a per-plan high-water mark: the highest task-id ordinal ever
// assigned in this plan. It is written first so it serialises as a top-level
// key ahead of the [phase] / [[task]] tables. A zero value means "unset" — for
// pre-existing plans NextTaskID backfills it from the current maximum id. An id
// freed by a remove is never handed out again because the counter only ever
// advances (see NextTaskID / AddTask).
type Plan struct {
	TaskSeq int       `toml:"task_seq,omitempty" json:"task_seq,omitempty"`
	Phase   PlanPhase `toml:"phase" json:"phase"`
	Task    []Task    `toml:"task" json:"task"` // ordered
}

type PlanPhase struct {
	ID string `toml:"id" json:"id"`
}

type Task struct {
	ID           string   `toml:"id" json:"id"`
	Wave         int      `toml:"wave" json:"wave"`
	Title        string   `toml:"title" json:"title"`
	Files        []string `toml:"files" json:"files"`
	Description  string   `toml:"description,omitempty" json:"description,omitempty"`
	Covers       []string `toml:"covers,omitempty" json:"covers,omitempty"`         // criterion ids
	DependsOn    []string `toml:"depends_on,omitempty" json:"depends_on,omitempty"` // task ids
	TestContract []string `toml:"test_contract,omitempty" json:"test_contract,omitempty"`
	Status       string   `toml:"status,omitempty" json:"status,omitempty"` // pending | in_progress | done | failed
}

// Task statuses.
const (
	StatusPending    = "pending"
	StatusInProgress = "in_progress"
	StatusDone       = "done"
	StatusFailed     = "failed"
)

// NextRunnable returns the next task with status==pending whose
// dependencies are all done, picked by lowest wave then by array position in
// the plan — so `dross task move` reorders what runs next within a wave.
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
		// Wave dominates; within a wave the first in document order wins.
		if best == nil || t.Wave < best.Wave {
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

// saveTOML writes v as TOML to path atomically: it encodes into a temp sibling
// (<path>.tmp) and os.Rename's it over the target only after a fully successful
// write. A mid-write failure (encode error, or a temp path that can't be
// created) therefore leaves any existing file byte-identical rather than
// truncated — the crash-safety guarantee the truncate-in-place os.Create lacked.
func saveTOML(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	enc := toml.NewEncoder(f)
	enc.Indent = "  "
	if err := enc.Encode(v); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}
