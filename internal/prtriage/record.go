package prtriage

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/Rivil/dross/internal/pathfence"
	"github.com/Rivil/dross/internal/phase"
	"github.com/Rivil/dross/internal/project"
	"github.com/Rivil/dross/internal/secretscan"
)

// File is the triage record of a phase's PR, tracked beside its plan.toml so
// it rides the phase PR like the plan does.
const File = "pr-triage.toml"

// ReplyMarker is the first line of every reply /dross-respond posts. A comment
// is its own reply only when it opens with this line AND the authenticated
// account wrote it; the marker alone can be copied by anyone.
const ReplyMarker = "## /dross-respond — rejected review comments"

// Verdicts.
const (
	VerdictAccept = "accept" // a new task in the plan of the PR's phase
	VerdictReject = "reject" // a stated reason, posted back on confirm
	VerdictRoute  = "route"  // a deferred item with a target
)

// Record is a phase's triage record: one resolution per comment or finding.
type Record struct {
	Resolution []Resolution `toml:"resolution"`
}

// Resolution is the verdict on one comment, or one finding of a split
// /dross-review comment. It keeps where the comment is and who wrote it,
// never what it says: a body is untrusted third-party text, and Digest is
// enough to notice it was edited.
//
// Evidence is embedded, so its keys (at, or cmd with output_sha256 and
// output_bytes) sit in the resolution's own table: the lossless patcher
// cannot patch a table nested inside an array-of-tables element.
type Resolution struct {
	ID      string `toml:"id"`
	Kind    string `toml:"kind"`
	PR      int    `toml:"pr"`
	URL     string `toml:"url"`
	Author  string `toml:"author"`
	Verdict string `toml:"verdict"`
	Reason  string `toml:"reason,omitempty"`
	Evidence
	Task     string `toml:"task,omitempty"`
	Deferred string `toml:"deferred,omitempty"`
	Target   string `toml:"target,omitempty"`
	Digest   string `toml:"digest"`
	Posted   bool   `toml:"posted,omitempty"`
}

// Refs is what a resolution may point at: the task ids of the phase's plan
// and the deferred ids of its spec.
type Refs struct {
	Tasks    []string
	Deferred []string
}

// itemID is a triage id: the kind's letter, the forge id, and for a finding
// of a split review its 1-based number.
var itemID = regexp.MustCompile(`^[cir][0-9]+(#[1-9][0-9]*)?$`)

// kindOf maps an id's letter to the kind it must carry.
var kindOf = map[byte]string{'c': "conversation", 'i': "inline", 'r': "review"}

var hexDigest = regexp.MustCompile(`^[0-9a-f]{64}$`)

// Load reads the record at path and the bytes it was read from, for Save's
// stale-read check. An absent file is an empty record; a key the schema does
// not know is an error naming it, so a hand-added `body = …` is never carried.
func Load(path string) (Record, []byte, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Record{}, nil, nil
	}
	if err != nil {
		return Record{}, nil, fmt.Errorf("read %s: %w", File, err)
	}
	var rec Record
	md, err := toml.Decode(string(b), &rec)
	if err != nil {
		return Record{}, nil, fmt.Errorf("decode %s: %w", File, err)
	}
	if extra := md.Undecoded(); len(extra) > 0 {
		keys := make([]string, len(extra))
		for i, k := range extra {
			keys[i] = k.String()
		}
		return Record{}, nil, fmt.Errorf("%s: unknown key %s — the record holds no such field", File, strings.Join(keys, ", "))
	}
	return rec, b, nil
}

// Upsert sets r as the resolution for its id: it replaces an existing entry
// in place, or appends.
func (rec *Record) Upsert(r Resolution) {
	for i := range rec.Resolution {
		if rec.Resolution[i].ID == r.ID {
			rec.Resolution[i] = r
			return
		}
	}
	rec.Resolution = append(rec.Resolution, r)
}

// Find returns the resolution for id.
func (rec Record) Find(id string) (Resolution, bool) {
	for _, r := range rec.Resolution {
		if r.ID == id {
			return r, true
		}
	}
	return Resolution{}, false
}

// Validate reports every problem in rec, each naming its entry and field.
func Validate(rec Record, refs Refs) []error {
	tasks := setOf(refs.Tasks)
	deferred := setOf(refs.Deferred)
	seen := map[string]bool{}
	var errs []error
	for i, r := range rec.Resolution {
		entry := fmt.Sprintf("resolution %d", i+1)
		if r.ID != "" {
			entry = fmt.Sprintf("resolution %s", r.ID)
		}
		bad := func(field, format string, args ...any) {
			errs = append(errs, fmt.Errorf("%s: %s %s: %s", File, entry, field, fmt.Sprintf(format, args...)))
		}
		switch {
		case !itemID.MatchString(r.ID):
			bad("id", "%q is not c|i|r<forge id>, with #<n> from 1 for a finding", r.ID)
		case seen[r.ID]:
			bad("id", "appears more than once")
		case kindOf[r.ID[0]] != r.Kind:
			bad("kind", "%q, but the id says %s", r.Kind, kindOf[r.ID[0]])
		}
		seen[r.ID] = true
		if r.PR <= 0 {
			bad("pr", "%d is not a PR number", r.PR)
		}
		if u, err := url.Parse(r.URL); r.URL == "" || err != nil || u.Scheme != "https" || u.Host == "" {
			bad("url", "%q is not an https:// link to the comment", r.URL)
		}
		if !hexDigest.MatchString(r.Digest) {
			bad("digest", "%q is not the comment's sha256", r.Digest)
		}
		if err := Require(r.Evidence); err != nil {
			bad("evidence", "%v", err)
		}
		switch r.Verdict {
		case VerdictAccept:
			if r.Task == "" {
				bad("task", "an accept names the task it added")
			} else if !tasks[r.Task] {
				bad("task", "%s is not a task in the phase's plan", r.Task)
			}
		case VerdictReject:
			if strings.TrimSpace(r.Reason) == "" {
				bad("reason", "a reject states why")
			}
		case VerdictRoute:
			if strings.TrimSpace(r.Target) == "" {
				bad("target", "a route names where it goes")
			}
			if r.Deferred == "" {
				bad("deferred", "a route names the deferred item it added")
			} else if !deferred[r.Deferred] {
				bad("deferred", "%s is not a deferred item in the phase's spec", r.Deferred)
			}
		default:
			bad("verdict", "%q is not accept, reject or route", r.Verdict)
		}
	}
	return errs
}

// ErrStaleRead is a Save whose record changed on disk after it was read.
var ErrStaleRead = errors.New(File + " changed since it was read — load it again and redo the change")

// Save writes rec to path. Every string is redacted first, and it is the
// redacted record that is validated against refs: what is checked is what is
// written. readBytes are what Load returned; if the file no longer holds them,
// another save got there first and this one is refused. On any refusal the
// file is left as it was.
func Save(path string, readBytes []byte, rec Record, refs Refs) error {
	clean := redactRecord(rec)
	if errs := Validate(clean, refs); len(errs) > 0 {
		return errors.Join(errs...)
	}
	current, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("read %s: %w", File, err)
	}
	if !bytes.Equal(current, readBytes) {
		return ErrStaleRead
	}
	return project.SaveTOML(path, &clean, project.ArrayKey{Path: "resolution", Field: "id"})
}

// redactRecord is rec with secretscan.Redact applied to every string field,
// found by reflection so a field added later cannot be missed.
func redactRecord(rec Record) Record {
	out := Record{Resolution: make([]Resolution, len(rec.Resolution))}
	for i, r := range rec.Resolution {
		redactStrings(reflect.ValueOf(&r).Elem())
		out.Resolution[i] = r
	}
	return out
}

func redactStrings(v reflect.Value) {
	switch v.Kind() {
	case reflect.String:
		s, _ := secretscan.Redact(v.String())
		v.SetString(s)
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			redactStrings(v.Field(i))
		}
	case reflect.Slice:
		for i := 0; i < v.Len(); i++ {
			redactStrings(v.Index(i))
		}
	}
}

func setOf(ids []string) map[string]bool {
	out := make(map[string]bool, len(ids))
	for _, id := range ids {
		out[id] = true
	}
	return out
}

// ParsePhaseHead is the one mapping from a PR head to the phase it ships: a
// same-repo `phase/<id>` branch whose id is a single path segment. It reads
// nothing.
func ParsePhaseHead(head string, crossRepo bool) (string, error) {
	if crossRepo {
		return "", fmt.Errorf("PR head %s is a branch in another repository; only a same-repo phase/<id> branch ships a phase", OneLine(head))
	}
	id, ok := strings.CutPrefix(head, "phase/")
	if !ok {
		return "", fmt.Errorf("PR head %s is not a phase/<id> branch", OneLine(head))
	}
	if err := pathfence.Segment("phase id", id); err != nil || id != strings.TrimSpace(id) {
		return "", fmt.Errorf("PR head %s does not name one phase/<id>", OneLine(head))
	}
	return id, nil
}

// PhaseFromHead is ParsePhaseHead for a verb that will write to the phase: the
// id must name a phase directory under root's .dross/phases/ that holds a
// plan.toml. What it returns is that directory's own name, read from the
// listing: a head ref is forge data, so no path is ever built from it.
func PhaseFromHead(root, head string, crossRepo bool) (string, error) {
	id, err := ParsePhaseHead(head, crossRepo)
	if err != nil {
		return "", err
	}
	local, ok := LocalPhase(root, id)
	if !ok {
		return "", fmt.Errorf("PR head phase/%s names no planned phase here: .dross/phases/%[1]s/plan.toml is missing", OneLine(id))
	}
	dir, err := phase.ContainID(filepath.Join(root, ".dross"), local)
	if err != nil {
		return "", fmt.Errorf("PR head %s does not name one phase/<id>", OneLine(head))
	}
	plan, err := pathfence.Contain(dir.String(), "phase plan", "plan.toml")
	if err != nil {
		return "", err
	}
	if info, err := pathfence.Stat(plan); err != nil || !info.Mode().IsRegular() {
		return "", fmt.Errorf("PR head phase/%s names no planned phase here: .dross/phases/%[1]s/plan.toml is missing", OneLine(id))
	}
	return local, nil
}

// LocalPhase is the phase directory under root's .dross/phases/ whose name is
// id, as the listing spells it — the name to build paths from when id came
// from somewhere else. It reads nothing but the listing.
func LocalPhase(root, id string) (string, bool) {
	entries, err := os.ReadDir(filepath.Join(root, ".dross", "phases"))
	if err != nil {
		return "", false
	}
	for _, e := range entries {
		if e.IsDir() && e.Name() == id {
			return e.Name(), true
		}
	}
	return "", false
}
