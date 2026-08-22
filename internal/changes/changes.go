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
	Phase string `json:"phase"`
	PR    int    `json:"pr,omitempty"`
	// Base is the branch this phase was forked from — the truth `dross phase
	// complete` reconciles against, instead of re-deriving a base from
	// current_milestone (which goes wrong the moment a stale milestone branch
	// is sitting in the local repo). Phase-scoped for the same reason PR is.
	Base string `json:"base,omitempty"`
	// BaseCommit is the SHA Base pointed at when the phase branch was forked —
	// the phase's fork point. Base alone names a moving target: by the time
	// anything needs to know where the phase started, main has moved on. A red
	// proof pinned to a commit needs a durable fork point to repoint to, and
	// that is this field.
	//
	// Empty on every record written before the field existed; the fork point is
	// then resolved on demand from Base plus the phase's own commits and cached
	// back here (the locked fork_point_backfill decision — no migration
	// command).
	BaseCommit string `json:"base_commit,omitempty"`
	// Status is how far this phase got: StatusShipped once its PR is open,
	// StatusComplete once `dross phase complete` reconciled it. Empty on every
	// record written before the field existed, which reads as "unknown", not
	// "not done".
	//
	// It exists because the only durable evidence of a finished phase used to
	// be the "completed <id>" breadcrumb in state.json's history — and that
	// history is capped at 50 entries. mutation-diff-scope's breadcrumb had
	// already been evicted while the phase was plainly done (verdict pass, PR
	// 79 merged), so anything counting doneness from history alone was reading
	// a window, not a record. This field is per-phase and never scrolls.
	Status string `json:"status,omitempty"`
	// BackfillEvidence names the commit a backfilled status was inferred from.
	// It is provenance, never a third status value: a record carrying it is
	// StatusComplete like any other, so the doneness switch stays two-valued
	// (phasedone.go). What it adds is auditability — a marker `dross phase
	// backfill` inferred from a commit subject is distinguishable from one
	// `dross phase complete` observed, so the sweep can be re-run against
	// better evidence rather than trusted blind.
	BackfillEvidence *BackfillEvidence `json:"backfill_evidence,omitempty"`
	// RedProof pins the commit a phase's red proof was recorded at, plus the
	// prose doc that replays it. Keyed to the phase (rather than living in the
	// doc alone) so a checker can name the fork point to repoint a rotted pin
	// to — the doc names no phase, and a SHA with no owner is a dead end.
	RedProof *RedProof             `json:"red_proof,omitempty"`
	Tasks    map[string]TaskRecord `json:"tasks"`
}

// RedProof is the machine half of a red proof: the pinned commit and the path
// (repo-relative) of the doc whose `base commit:` line must agree with it. The
// doc stays the human artefact; this is what a check reads.
type RedProof struct {
	SHA string `json:"sha"`
	Doc string `json:"doc"`
	// Replay is the command that replays the proof at the pinned commit, if
	// one was recorded. It is what turns a repoint from a hopeful rewrite into
	// a checked one: the repair can re-run this at the proposed commit and
	// refuse unless the proof still goes red there. Optional — omitted, a
	// repoint reports itself unverified rather than implying it was checked.
	Replay string `json:"replay,omitempty"`
}

// BackfillEvidence is the commit a backfilled status was inferred from.
//
// The SHA deliberately does NOT serialize under a "commit" key, at any nesting
// depth. doctor's extractCommitSHAs (doctor.go) is a literal `"commit":` text
// scan over the raw changes.json body rather than a JSON parse, and every
// backfilled ship SHA entering that map would have phaseCommitsOnMain report it
// as a leaked phase commit whenever local main is ahead of origin. The wrapper
// object plus an inner "sha" keeps 67 backfilled records out of that scan.
type BackfillEvidence struct {
	SHA string `json:"sha"`
}

// The two values Status takes. Ordered: a phase reaches shipped first and
// complete after, and SetStatus refuses to walk that backwards.
const (
	StatusShipped  = "shipped"
	StatusComplete = "complete"
)

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

// SetBase records the branch a phase was forked from, mirroring SetPR's
// load-set-save so the two coexist in one phase-scoped record. Written at fork
// time by `dross phase create` and overwritten at ship time with the base the
// PR was actually opened against; read back by `dross phase complete` so it
// reconciles against the real base rather than an inferred one.
func SetBase(root, phaseID, branch string) error {
	path := FilePath(root, phaseID)
	c, err := Load(path, phaseID)
	if err != nil {
		return err
	}
	c.Base = branch
	return c.Save(path)
}

// SetFork records the branch a phase was forked from AND the commit that
// branch pointed at, in one load-set-save. Written at fork time by `dross phase
// create`/`insert`, which is the only moment both facts are known without
// re-deriving either.
//
// Separate from SetBase because ship rewrites the base alone (the branch the PR
// was actually opened against) long after the fork — a ship-time SHA would be
// main's tip today, not the phase's fork point, and silently overwriting the
// fork point with it is the exact rot this field exists to survive.
func SetFork(root, phaseID, branch, commit string) error {
	path := FilePath(root, phaseID)
	c, err := Load(path, phaseID)
	if err != nil {
		return err
	}
	c.Base = branch
	c.BaseCommit = commit
	return c.Save(path)
}

// SetStatus records how far the phase got, mirroring SetPR's load-set-save.
//
// It is monotonic: once a phase reads StatusComplete, a later StatusShipped
// write is dropped rather than applied. Re-shipping a completed phase (a
// follow-up PR against the same phase dir) is a real thing to do, and it must
// not make a finished phase look unfinished to everything that reads this
// marker.
func SetStatus(root, phaseID, status string) error {
	path := FilePath(root, phaseID)
	c, err := Load(path, phaseID)
	if err != nil {
		return err
	}
	if c.Status == StatusComplete && status == StatusShipped {
		return nil
	}
	c.Status = status
	return c.Save(path)
}

// SetBackfilled records an inferred completion: StatusComplete plus the commit
// the inference was drawn from, written together in one load-set-save so a
// marker can never land without its provenance.
//
// It refuses a record that already carries a status. Backfill is a sweep over
// status-less records; reaching one that is already marked means the candidate
// set and the writer disagree, and overwriting quietly would destroy an
// observed marker in favour of an inferred one.
func SetBackfilled(root, phaseID, sha string) error {
	sha = strings.TrimSpace(sha)
	if sha == "" {
		return fmt.Errorf("backfill %s: no evidence commit", phaseID)
	}
	path := FilePath(root, phaseID)
	c, err := Load(path, phaseID)
	if err != nil {
		return err
	}
	if c.Status != "" {
		return fmt.Errorf("backfill %s: record already has status %q — refusing to overwrite an observed marker", phaseID, c.Status)
	}
	c.Status = StatusComplete
	c.BackfillEvidence = &BackfillEvidence{SHA: sha}
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

// Complete reports whether a phase's record says `dross phase complete`
// reconciled it — status is exactly StatusComplete.
//
// It is the narrower sibling of cmd.phaseDone, and deliberately NOT the same
// question. phaseDone asks "did this phase finish its run?", which StatusShipped
// also answers; that is right for milestone counting. The re-entry surfaces
// (watch's drift digest, the reconcile count, status's waiting-on-merge line,
// the SessionStart line) ask "is this phase closed out?" — and a shipped record
// is a phase mid-flight between the push and the merge, which is exactly the
// state those surfaces exist to announce. Reading phaseDone there would silence
// the merge gate, so the narrowing stays.
//
// A missing or unreadable record is not complete: absence of evidence is
// "unknown", never "done" — the same reasoning phaseIsDone carries.
func Complete(root, phaseID string) bool {
	c, err := Load(FilePath(root, phaseID), phaseID)
	if err != nil {
		return false
	}
	return c.Complete()
}

// Complete is the predicate over an already-loaded record. Callers that need to
// distinguish "unreadable" from "not complete" (the reap sweep does) load the
// record themselves and ask it here, so there is one definition of complete
// rather than one per reader.
func (c *Changes) Complete() bool {
	return c != nil && c.Status == StatusComplete
}
