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
	Milestone Meta     `toml:"milestone"`
	Scope     Scope    `toml:"scope"`
	Phases    []string `toml:"phases"` // phase ids in delivery order
}

type Meta struct {
	Version     string `toml:"version"` // e.g. "v1.0"
	Title       string `toml:"title,omitempty"`
	Status      string `toml:"status,omitempty"` // planning | active | shipped | archived
	Started     string `toml:"started,omitempty"`
	Shipped     string `toml:"shipped,omitempty"`
}

type Scope struct {
	SuccessCriteria []string `toml:"success_criteria,omitempty"`
	NonGoals        []string `toml:"non_goals,omitempty"`
}

// FilePath returns the canonical milestone toml path.
// e.g. FilePath(".dross", "v1.0") -> ".dross/milestones/v1.0.toml"
func FilePath(root, version string) string {
	return filepath.Join(root, "milestones", version+".toml")
}

// SummaryDir returns the prose retrospective dir.
func SummaryDir(root, version string) string {
	return filepath.Join(root, "milestones", version)
}

func Load(path string) (*Milestone, error) {
	var m Milestone
	if _, err := toml.DecodeFile(path, &m); err != nil {
		return nil, fmt.Errorf("decode %s: %w", path, err)
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
