// Package milestone handles .dross/milestones/<version>.toml.
//
// A milestone is a versioned bundle of phases — typically a release scope.
// SUMMARY.md (retrospective prose) lives at .dross/milestones/<version>/SUMMARY.md
// and is written manually at milestone close.
package milestone

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/Rivil/dross/internal/pathfence"
)

type Milestone struct {
	Milestone Meta     `toml:"milestone" json:"milestone"`
	Scope     Scope    `toml:"scope" json:"scope"`
	Phases    []string `toml:"phases" json:"phases"` // phase ids in delivery order

	// Closeout holds one acknowledgement per close-out item the user chose to
	// carry past `milestone complete` with a reason (locked ack_storage). Nil
	// on every milestone without acks, so an untouched toml re-saves unchanged.
	Closeout []CloseoutAck `toml:"closeout_ack,omitempty" json:"closeout_ack,omitempty"`
}

// CloseoutAck acknowledges one unresolved close-out item. Item is an id in the
// ParseAckItem grammar; Reason is the non-empty prose carried into the
// integration PR body.
type CloseoutAck struct {
	Item   string    `toml:"item" json:"item"`
	Reason string    `toml:"reason" json:"reason"`
	At     time.Time `toml:"at" json:"at"`
}

// Ack records an acknowledgement for item, replacing any earlier one for the
// same item so a re-ack leaves exactly one record carrying the latest reason.
// A blank or whitespace-only reason is refused and leaves m unchanged.
func (m *Milestone) Ack(item, reason string, at time.Time) error {
	if strings.TrimSpace(reason) == "" {
		return fmt.Errorf("acknowledge %s: a non-empty --reason is required", item)
	}
	for i := range m.Closeout {
		if m.Closeout[i].Item == item {
			m.Closeout[i].Reason = reason
			m.Closeout[i].At = at
			return nil
		}
	}
	m.Closeout = append(m.Closeout, CloseoutAck{Item: item, Reason: reason, At: at})
	return nil
}

// Ack item kinds — the prefix before the first colon of an ack item id.
const (
	AckPhase    = "phase"
	AckDeferred = "deferred"
	AckSurvivor = "survivor"
)

// AckItem is a parsed close-out item id. Source and Index are set only for
// the deferred kind; Payload holds the phase slug or the survivor key.
type AckItem struct {
	Kind    string
	Payload string
	Source  string
	Index   int
}

// ackForms names the accepted item shapes in every parse refusal.
const ackForms = "phase:<slug>, deferred:<source>#<index>, survivor:<key>"

// ParseAckItem parses a close-out item id. Only three shapes are accepted:
// phase:<slug>, deferred:<source>#<non-negative int> and survivor:<key>. The
// kind splits on the first colon only, so a keyless survivor key such as
// f.go:12:op keeps its inner colons.
func ParseAckItem(id string) (AckItem, error) {
	kind, payload, ok := strings.Cut(id, ":")
	if !ok || payload == "" {
		return AckItem{}, fmt.Errorf("unknown close-out item %q: want one of %s", id, ackForms)
	}
	switch kind {
	case AckPhase, AckSurvivor:
		return AckItem{Kind: kind, Payload: payload}, nil
	case AckDeferred:
		hash := strings.LastIndex(payload, "#")
		if hash <= 0 {
			return AckItem{}, fmt.Errorf("unknown close-out item %q: want one of %s", id, ackForms)
		}
		source, digits := payload[:hash], payload[hash+1:]
		idx, err := parseIndex(digits)
		if err != nil {
			return AckItem{}, fmt.Errorf("unknown close-out item %q: index %w; want one of %s", id, err, ackForms)
		}
		return AckItem{Kind: kind, Payload: payload, Source: source, Index: idx}, nil
	}
	return AckItem{}, fmt.Errorf("unknown close-out item %q: want one of %s", id, ackForms)
}

// parseIndex accepts only plain decimal digits, so "+1" or "01"-style
// spellings cannot alias one item under two ids.
func parseIndex(s string) (int, error) {
	if s == "" {
		return 0, errors.New("is empty")
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, fmt.Errorf("%q is not a non-negative integer", s)
		}
	}
	if len(s) > 1 && s[0] == '0' {
		return 0, fmt.Errorf("%q has a leading zero", s)
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("%q: %w", s, err)
	}
	return n, nil
}

// FormatAckItem renders a parsed item back to its id; ParseAckItem(FormatAckItem(x))
// returns x for every item ParseAckItem accepts.
func FormatAckItem(it AckItem) string {
	if it.Kind == AckDeferred {
		return AckDeferred + ":" + it.Source + "#" + strconv.Itoa(it.Index)
	}
	return it.Kind + ":" + it.Payload
}

type Meta struct {
	Version string `toml:"version" json:"version"` // e.g. "v1.0"
	Title   string `toml:"title,omitempty" json:"title,omitempty"`
	Status  string `toml:"status,omitempty" json:"status,omitempty"` // configenum.MilestoneStatuses: planning | active | complete
	Started string `toml:"started,omitempty" json:"started,omitempty"`
	Shipped string `toml:"shipped,omitempty" json:"shipped,omitempty"`
	// Base is the branch milestone/<version> was cut from, recorded at create
	// time and read back verbatim — never re-inferred from git topology or
	// state.current_milestone. Empty on every milestone created before v1.2,
	// which reads as the repo's main branch (see BaseOr).
	Base string `toml:"base,omitempty" json:"base,omitempty"`
}

type Scope struct {
	SuccessCriteria []string `toml:"success_criteria,omitempty" json:"success_criteria,omitempty"`
	NonGoals        []string `toml:"non_goals,omitempty" json:"non_goals,omitempty"`
}

// ContainVersion contains a milestone version's toml under root/milestones.
// A version is read back from hand-editable state.json and spec.toml, so it is
// untrusted: one that escapes milestones/ is refused with pathfence.ErrEscapes.
func ContainVersion(root, version string) (pathfence.Contained, error) {
	return pathfence.Contain(filepath.Join(root, "milestones"), "milestone version", version+".toml")
}

// FilePath returns the canonical milestone toml path.
// e.g. FilePath(".dross", "v1.0") -> ".dross/milestones/v1.0.toml". A version
// that would escape milestones/ resolves to the refused segment
// (phase.RefusedSegment), where no milestone exists.
func FilePath(root, version string) string {
	c, err := ContainVersion(root, version)
	if err != nil {
		return filepath.Join(root, "milestones", refusedSegment+".toml")
	}
	p := c.String()
	return p
}

// refusedSegment mirrors phase.RefusedSegment.
const refusedSegment = "_refused"

func Load(path string) (*Milestone, error) {
	var m Milestone
	if _, err := toml.DecodeFile(path, &m); err != nil {
		return nil, fmt.Errorf("load milestone %s: %w", path, err)
	}
	return &m, nil
}

func (m *Milestone) Save(path string) error {
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
	return enc.Encode(m)
}

// BaseOr returns the recorded cut branch, falling back to mainBranch when the
// milestone records none. mainBranch is passed in rather than hardcoded so a
// repo on `master` reads correctly without this package importing project
// config.
func (m *Milestone) BaseOr(mainBranch string) string {
	if m.Milestone.Base == "" {
		return mainBranch
	}
	return m.Milestone.Base
}

// LoadAll loads every milestone under .dross/milestones/, keyed by version.
//
// It fails closed: a malformed or unreadable toml returns an error naming that
// file rather than a short map. Callers use the result to decide whether a
// branch delete is safe, and a silently-skipped file is indistinguishable from
// "no dependents" — with an irreversible remote delete as the consequence.
func LoadAll(root string) (map[string]*Milestone, error) {
	versions, err := List(root)
	if err != nil {
		return nil, err
	}
	out := make(map[string]*Milestone, len(versions))
	for _, v := range versions {
		m, err := Load(FilePath(root, v))
		if err != nil {
			return nil, err
		}
		out[v] = m
	}
	return out, nil
}

// List returns milestone versions discovered under .dross/milestones/, sorted.
func List(root string) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(root, "milestones"))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var versions []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".toml") {
			continue
		}
		versions = append(versions, strings.TrimSuffix(name, ".toml"))
	}
	sort.Strings(versions)
	return versions, nil
}
