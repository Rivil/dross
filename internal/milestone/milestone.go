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
	"strings"

	"github.com/BurntSushi/toml"
)

type Milestone struct {
	Milestone Meta     `toml:"milestone" json:"milestone"`
	Scope     Scope    `toml:"scope" json:"scope"`
	Phases    []string `toml:"phases" json:"phases"` // phase ids in delivery order
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

// FilePath returns the canonical milestone toml path.
// e.g. FilePath(".dross", "v1.0") -> ".dross/milestones/v1.0.toml"
func FilePath(root, version string) string {
	return filepath.Join(root, "milestones", version+".toml")
}

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
