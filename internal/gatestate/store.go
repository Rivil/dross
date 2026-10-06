// Package gatestate is the machine-local record the tool gates judge against:
// the tree the last full green `dross test` ran on, the mode /dross-execute
// is running in, the approval a human gave the current task, the solo review
// ledger, the mode a /dross-quick run is in, the review context file, and the
// approval a human gave to post one drafted PR reply.
//
// Records live in .dross/gate/, which carries its own `*` .gitignore: a green
// or an approval is a fact about THIS machine, and a copy committed to the repo
// would let a clone arrive pre-approved. For the same reason a record that git
// reports tracked is refused unread (mirroring consent.RefuseTrackedLocal).
//
// Each record has one writer, and every write is a unique temp file renamed
// over the record, so a reader — a hook running concurrently with
// `dross test` — sees the old record, the new one, or none, never half of one.
// A missing record is no record; a record that does not decode is an error
// naming its path, never a zero value a gate could mistake for a real one.
package gatestate

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Rivil/dross/internal/gitrun"
	"github.com/Rivil/dross/internal/review"
)

// Dir is the record directory, relative to a repo root.
const Dir = ".dross/gate"

// Record file names.
const (
	GreenFile    = "green.json"
	ExecuteFile  = "execute.json"
	ApprovalFile = "approval.json"
	ReviewFile   = "review.json"
	QuickFile    = "quick.json"
	ReplyFile    = "reply.json"
	// ContextFile is the rendered review context the reviewer reads. It is
	// written by `dross review context` and never read back by a gate: the
	// recorder regenerates the context and compares digests.
	ContextFile = "review-context.md"
)

// Green is the tree fingerprint (internal/treefp) of the last full green
// `dross test`, when it ran and where.
type Green struct {
	Tree   string    `json:"tree"`
	At     time.Time `json:"at"`
	Runner string    `json:"runner"`
}

// Execute is the mode /dross-execute recorded for a phase: "pair" or "solo".
type Execute struct {
	Phase string    `json:"phase"`
	Mode  string    `json:"mode"`
	At    time.Time `json:"at"`
}

// Approval is a human's approval of one task, valid at one HEAD.
type Approval struct {
	Phase string    `json:"phase"`
	Task  string    `json:"task"`
	Head  string    `json:"head"`
	At    time.Time `json:"at"`
}

// ReplyApproval is a human's approval to post one drafted PR reply: the PR,
// and the digest of the exact body they were shown.
type ReplyApproval struct {
	PR     int       `json:"pr"`
	Digest string    `json:"digest"`
	At     time.Time `json:"at"`
}

// Review is the solo review ledger for one armed scope: a plan task (Phase,
// Task) or a solo quick, at one Attempt — the key a fresh attempt changes.
// Rounds keep spec and quality findings apart on disk too.
type Review struct {
	Kind    review.Kind    `json:"kind"`
	Phase   string         `json:"phase,omitempty"`
	Task    string         `json:"task,omitempty"`
	Attempt string         `json:"attempt"`
	Rounds  []review.Round `json:"rounds"`
	// Pending are background reviewer spawns whose verdict has not arrived:
	// the PostToolUse launch records one, the same agent's SubagentStop turns
	// it into a round.
	Pending []Launch `json:"pending,omitempty"`
}

// Launch is one background reviewer spawn awaiting its SubagentStop: the
// agent's id, the digest of the context its prompt named, and the tree that
// context was built from — the tree its verdict is bound to.
type Launch struct {
	AgentID string `json:"agent_id"`
	Digest  string `json:"digest"`
	Tree    string `json:"tree"`
}

// Quick is the mode a /dross-quick run recorded: "pair" or "solo", the
// quick's stated description (its only spec source), the HEAD it began at,
// and when — a fresh `dross quick begin` is a fresh attempt.
type Quick struct {
	Mode        string    `json:"mode"`
	Description string    `json:"description"`
	Head        string    `json:"head"`
	At          time.Time `json:"at"`
}

// Path is the absolute path of a record under root.
func Path(root, name string) string {
	return filepath.Join(root, filepath.FromSlash(Dir), name)
}

func rel(name string) string { return Dir + "/" + name }

// LoadGreen returns the green record, or nil when there is none.
func LoadGreen(root string) (*Green, error) {
	var g Green
	ok, err := load(root, GreenFile, &g)
	if err != nil || !ok {
		return nil, err
	}
	if strings.TrimSpace(g.Tree) == "" {
		return nil, fmt.Errorf("%s: green record has no tree", rel(GreenFile))
	}
	return &g, nil
}

// SaveGreen records a green run. A green with no tree is refused: an empty
// fingerprint could only ever match another empty one.
func SaveGreen(root string, g Green) error {
	if strings.TrimSpace(g.Tree) == "" {
		return fmt.Errorf("%s: refusing to record a green with no tree", rel(GreenFile))
	}
	return save(root, GreenFile, g)
}

// ClearGreen removes the green record; none is not an error.
func ClearGreen(root string) error { return remove(root, GreenFile) }

// LoadExecute returns the execute-mode record, or nil when there is none.
func LoadExecute(root string) (*Execute, error) {
	var e Execute
	ok, err := load(root, ExecuteFile, &e)
	if err != nil || !ok {
		return nil, err
	}
	return &e, nil
}

// SaveExecute records the execute mode.
func SaveExecute(root string, e Execute) error { return save(root, ExecuteFile, e) }

// LoadApproval returns the approval record, or nil when there is none.
func LoadApproval(root string) (*Approval, error) {
	var a Approval
	ok, err := load(root, ApprovalFile, &a)
	if err != nil || !ok {
		return nil, err
	}
	return &a, nil
}

// SaveApproval records an approval.
func SaveApproval(root string, a Approval) error { return save(root, ApprovalFile, a) }

// LoadReplyApproval returns the reply approval, or nil when there is none.
func LoadReplyApproval(root string) (*ReplyApproval, error) {
	var a ReplyApproval
	ok, err := load(root, ReplyFile, &a)
	if err != nil || !ok {
		return nil, err
	}
	if err := a.check(); err != nil {
		return nil, err
	}
	return &a, nil
}

// SaveReplyApproval records a reply approval, replacing any earlier one.
func SaveReplyApproval(root string, a ReplyApproval) error {
	if err := a.check(); err != nil {
		return fmt.Errorf("refusing to record it: %w", err)
	}
	return save(root, ReplyFile, a)
}

// ClearReplyApproval removes the reply approval once it is spent; none is not
// an error.
func ClearReplyApproval(root string) error { return remove(root, ReplyFile) }

// check refuses an approval that names no PR or no digest: an empty digest
// could only ever match another empty one.
func (a ReplyApproval) check() error {
	if a.PR <= 0 {
		return fmt.Errorf("%s: reply approval names no PR", rel(ReplyFile))
	}
	if strings.TrimSpace(a.Digest) == "" {
		return fmt.Errorf("%s: reply approval has no digest", rel(ReplyFile))
	}
	return nil
}

// LoadReview returns the review ledger, or nil when there is none.
func LoadReview(root string) (*Review, error) {
	var r Review
	ok, err := load(root, ReviewFile, &r)
	if err != nil || !ok {
		return nil, err
	}
	if err := r.check(); err != nil {
		return nil, err
	}
	return &r, nil
}

// SaveReview records the review ledger. Its one writer is the PostToolUse
// recorder in internal/gate — no CLI verb lets the executing agent record a
// pass (review_pass_signal).
func SaveReview(root string, r Review) error {
	if err := r.check(); err != nil {
		return fmt.Errorf("refusing to record it: %w", err)
	}
	return save(root, ReviewFile, r)
}

// check refuses a ledger a gate could misread: an unknown kind, no attempt
// key, or a pass bound to no tree — an empty fingerprint could only ever
// match another empty one.
func (r Review) check() error {
	if r.Kind != review.KindTask && r.Kind != review.KindQuick {
		return fmt.Errorf("%s: unknown review kind %q", rel(ReviewFile), r.Kind)
	}
	if strings.TrimSpace(r.Attempt) == "" {
		return fmt.Errorf("%s: review record has no attempt key", rel(ReviewFile))
	}
	for i, rd := range r.Rounds {
		if rd.Outcome == review.OutcomePass && strings.TrimSpace(rd.Tree) == "" {
			return fmt.Errorf("%s: round %d is a pass with no tree", rel(ReviewFile), i+1)
		}
	}
	for i, l := range r.Pending {
		if strings.TrimSpace(l.AgentID) == "" || strings.TrimSpace(l.Digest) == "" || strings.TrimSpace(l.Tree) == "" {
			return fmt.Errorf("%s: pending launch %d lacks its agent id, digest or tree", rel(ReviewFile), i+1)
		}
	}
	return nil
}

// LoadQuick returns the quick-mode record, or nil when there is none.
func LoadQuick(root string) (*Quick, error) {
	var q Quick
	ok, err := load(root, QuickFile, &q)
	if err != nil || !ok {
		return nil, err
	}
	if q.Mode != "pair" && q.Mode != "solo" {
		return nil, fmt.Errorf("%s: unknown quick mode %q", rel(QuickFile), q.Mode)
	}
	return &q, nil
}

// SaveQuick records the quick mode.
func SaveQuick(root string, q Quick) error { return save(root, QuickFile, q) }

// RemoveQuick removes the quick-mode record; none is not an error.
func RemoveQuick(root string) error { return remove(root, QuickFile) }

// SaveContext writes the review context file the reviewer reads.
func SaveContext(root string, b []byte) error { return saveBytes(root, ContextFile, b) }

// RefuseTracked errors when git reports the record tracked: a cloned repo must
// not ship its own green or approval.
func RefuseTracked(root, name string) error {
	if gitrun.Quiet(root, "ls-files", "--error-unmatch", "--", rel(name)) != nil {
		return nil
	}
	return fmt.Errorf("refusing to read %s: git reports it tracked.\n\n"+
		"The gate records are machine-local by design — a committed green or approval\n"+
		"would let a repo vouch for itself. Untrack it and keep the local copy:\n\n"+
		"    git rm --cached %s", rel(name), rel(name))
}

// load decodes a record. The tracked check runs before the read, so a
// committed record is refused without its contents ever being looked at.
func load(root, name string, into any) (bool, error) {
	_, err := os.Stat(Path(root, name))
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read %s: %w", rel(name), err)
	}
	if err := RefuseTracked(root, name); err != nil {
		return false, err
	}
	b, err := os.ReadFile(Path(root, name))
	if err != nil {
		return false, fmt.Errorf("read %s: %w", rel(name), err)
	}
	if err := json.Unmarshal(b, into); err != nil {
		return false, fmt.Errorf("%s: %w", rel(name), err)
	}
	return true, nil
}

// ensureDir creates .dross/gate/ with its self-ignoring .gitignore, so the
// first record never shows in `git status` and never rides `git add .dross/`.
func ensureDir(root string) error {
	dir := filepath.Join(root, filepath.FromSlash(Dir))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", Dir, err)
	}
	ignore := filepath.Join(dir, ".gitignore")
	if _, err := os.Stat(ignore); err == nil {
		return nil
	}
	if err := os.WriteFile(ignore, []byte("# machine-local tool-gate records — never committed\n*\n"), 0o644); err != nil {
		return fmt.Errorf("write %s/.gitignore: %w", Dir, err)
	}
	return nil
}

// save writes the record to a unique temp file and renames it into place.
func save(root, name string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return saveBytes(root, name, append(b, '\n'))
}

// saveBytes writes b to a unique temp file and renames it over name.
func saveBytes(root, name string, b []byte) error {
	if err := ensureDir(root); err != nil {
		return err
	}
	dst := Path(root, name)
	tmp, err := os.CreateTemp(filepath.Dir(dst), "."+name+".*.tmp")
	if err != nil {
		return fmt.Errorf("write %s: %w", rel(name), err)
	}
	defer os.Remove(tmp.Name()) // a no-op once the rename has moved it
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return fmt.Errorf("write %s: %w", rel(name), err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("write %s: %w", rel(name), err)
	}
	if err := os.Rename(tmp.Name(), dst); err != nil {
		return fmt.Errorf("write %s: %w", rel(name), err)
	}
	return nil
}

func remove(root, name string) error {
	err := os.Remove(Path(root, name))
	if err == nil || errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return fmt.Errorf("remove %s: %w", rel(name), err)
}
