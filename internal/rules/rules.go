// Package rules implements the two-tier rules system.
//
// Global rules live at ~/.claude/dross/rules.toml.
// Project rules live at <repo>/.dross/rules.toml.
//
// At command boot, both files are loaded and merged. Project rules
// win on id collision. The merged set is rendered into a <rules>
// block injected into every dross command's prompt context.
package rules

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

const File = "rules.toml"

type Severity string

const (
	Hard Severity = "hard" // blocking — must never violate
	Soft Severity = "soft" // warning — flag, allow override
)

type Rule struct {
	ID       string   `toml:"id"`
	Text     string   `toml:"text"`
	Severity Severity `toml:"severity,omitempty"`
	Created  string   `toml:"created,omitempty"`
	Disabled bool     `toml:"disabled,omitempty"`
}

type Set struct {
	Rules []Rule `toml:"rule"`
}

type Scope string

const (
	Global  Scope = "global"
	Project Scope = "project"
)

// LoadFile reads a single rules.toml. Missing file = empty set, no error.
func LoadFile(path string) (*Set, error) {
	var s Set
	_, err := toml.DecodeFile(path, &s)
	if errors.Is(err, fs.ErrNotExist) {
		return &Set{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("decode %s: %w", path, err)
	}
	return &s, nil
}

// SaveFile writes a rules.toml (creating parent dir if needed).
func (s *Set) SaveFile(path string) error {
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
	return enc.Encode(s)
}

// Merge combines global + project sets. Project wins on id collision.
// Result is sorted by scope then id for stable rendering.
func Merge(global, project *Set) []Resolved {
	out := map[string]Resolved{}
	for _, r := range global.Rules {
		if r.Disabled {
			continue
		}
		out[r.ID] = Resolved{Rule: r, Scope: Global}
	}
	for _, r := range project.Rules {
		if r.Disabled {
			continue
		}
		out[r.ID] = Resolved{Rule: r, Scope: Project}
	}
	merged := make([]Resolved, 0, len(out))
	for _, v := range out {
		merged = append(merged, v)
	}
	sort.Slice(merged, func(i, j int) bool {
		if merged[i].Scope != merged[j].Scope {
			return merged[i].Scope == Global // global first
		}
		return merged[i].ID < merged[j].ID
	})
	return merged
}

type Resolved struct {
	Rule
	Scope Scope
}

// Builtins are rules baked into every Render output. They are not
// editable via `dross rule add/remove` — they encode invariants the
// tool itself relies on (e.g. commit hygiene for .dross/ writes).
var Builtins = []Resolved{
	{
		Scope: "builtin",
		Rule: Rule{
			ID:       "dross-commit-hygiene",
			Severity: Hard,
			Text:     "If your slash command (or a `dross` CLI call inside it) writes to any file under `.dross/`, commit those writes with `git add <files> && git commit` before wrapping. Use `repo.commit_convention` from project.toml; if none, prefix with `chore(dross): `. Never leave `.dross/` dirty across slash-command boundaries.",
		},
	},
}

// Render produces the <rules> block injected into prompt context.
// Builtins are always emitted first, followed by user-configured rules.
func Render(merged []Resolved) string {
	var b strings.Builder
	b.WriteString("<rules>\n")
	b.WriteString("These rules are MUST-FOLLOW. Hard rules are blocking; soft rules are advisories.\n\n")
	for _, r := range Builtins {
		sev := r.Severity
		if sev == "" {
			sev = Hard
		}
		fmt.Fprintf(&b, "[%s/%s/%s] %s\n", r.Scope, sev, r.ID, r.Text)
	}
	if len(merged) == 0 {
		b.WriteString("\n(no user rules configured)\n")
	} else {
		for _, r := range merged {
			sev := r.Severity
			if sev == "" {
				sev = Hard
			}
			fmt.Fprintf(&b, "[%s/%s/%s] %s\n", r.Scope, sev, r.ID, r.Text)
		}
	}
	b.WriteString("</rules>")
	return b.String()
}

// Add appends a new rule to a Set. Returns an error if id already exists.
func (s *Set) Add(r Rule) error {
	for _, existing := range s.Rules {
		if existing.ID == r.ID {
			return fmt.Errorf("rule %q already exists", r.ID)
		}
	}
	if r.Severity == "" {
		r.Severity = Hard
	}
	if r.Created == "" {
		r.Created = time.Now().UTC().Format("2006-01-02")
	}
	s.Rules = append(s.Rules, r)
	return nil
}

// Remove deletes a rule by id. Returns false if not found.
func (s *Set) Remove(id string) bool {
	for i, r := range s.Rules {
		if r.ID == id {
			s.Rules = append(s.Rules[:i], s.Rules[i+1:]...)
			return true
		}
	}
	return false
}

// Find returns a rule by id and a flag whether it was found.
func (s *Set) Find(id string) (Rule, bool) {
	for _, r := range s.Rules {
		if r.ID == id {
			return r, true
		}
	}
	return Rule{}, false
}

// SetDisabled toggles a rule's disabled flag. Returns false if not found.
func (s *Set) SetDisabled(id string, disabled bool) bool {
	for i := range s.Rules {
		if s.Rules[i].ID == id {
			s.Rules[i].Disabled = disabled
			return true
		}
	}
	return false
}
