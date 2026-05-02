// Package profile handles the user behavioural profile.
//
// Global lives at ~/.claude/dross/profile.toml.
// Project overrides live at <repo>/.dross/profile.toml.
//
// Schema mirrors GSD's 8-dimension model so SeedFromGSD can import
// an existing USER-PROFILE.md without losing calibration.
package profile

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

const File = "profile.toml"

type Confidence string

const (
	High   Confidence = "high"
	Medium Confidence = "medium"
	Low    Confidence = "low"
)

type Dimension struct {
	Rating     string     `toml:"rating"`
	Confidence Confidence `toml:"confidence"`
	Directive  string     `toml:"directive"`
}

type Profile struct {
	Generated     string               `toml:"generated,omitempty"`
	Source        string               `toml:"source,omitempty"`
	Dimensions    map[string]Dimension `toml:"dimensions"`
	UserOverrides map[string]string    `toml:"user_overrides,omitempty"` // arbitrary k/v
}

func LoadFile(path string) (*Profile, error) {
	var p Profile
	_, err := toml.DecodeFile(path, &p)
	if errors.Is(err, fs.ErrNotExist) {
		return &Profile{Dimensions: map[string]Dimension{}}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("decode %s: %w", path, err)
	}
	if p.Dimensions == nil {
		p.Dimensions = map[string]Dimension{}
	}
	return &p, nil
}

func (p *Profile) SaveFile(path string) error {
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
	return enc.Encode(p)
}

// Merge overlays project on global per dimension.
func Merge(global, project *Profile) *Profile {
	out := &Profile{
		Generated:  global.Generated,
		Source:     global.Source,
		Dimensions: map[string]Dimension{},
	}
	for k, v := range global.Dimensions {
		out.Dimensions[k] = v
	}
	for k, v := range project.Dimensions {
		out.Dimensions[k] = v
	}
	return out
}

// SeedFromGSD looks for ~/.claude/get-shit-done/USER-PROFILE.md and
// writes a parsed profile.toml to dest if found. Returns nil if no
// GSD profile exists, an error only on parse failure or write failure.
func SeedFromGSD(dest string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	src := filepath.Join(home, ".claude", "get-shit-done", "USER-PROFILE.md")
	b, err := os.ReadFile(src)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	p := parseGSDProfile(string(b))
	p.Source = "seeded_from_gsd"
	return p.SaveFile(dest)
}

// parseGSDProfile is a minimal regex-light parser for GSD's
// USER-PROFILE.md format. It looks for "## <Dimension>" sections
// followed by **Rating:** ... | **Confidence:** ... and **Directive:** ...
//
// Best-effort. Anything it doesn't recognise is ignored — the profile
// can be rebuilt later via /dross-profile refresh.
func parseGSDProfile(s string) *Profile {
	p := &Profile{Source: "seeded_from_gsd", Dimensions: map[string]Dimension{}}
	lines := strings.Split(s, "\n")
	var current string
	var dim Dimension
	flush := func() {
		if current != "" && (dim.Rating != "" || dim.Directive != "") {
			p.Dimensions[normaliseDim(current)] = dim
		}
	}
	for _, line := range lines {
		t := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(t, "## "):
			flush()
			current = strings.TrimSpace(strings.TrimPrefix(t, "## "))
			dim = Dimension{}
		case strings.HasPrefix(t, "**Rating:**"):
			rest := strings.TrimSpace(strings.TrimPrefix(t, "**Rating:**"))
			parts := strings.Split(rest, "|")
			dim.Rating = strings.TrimSpace(parts[0])
			if len(parts) > 1 {
				conf := strings.TrimSpace(parts[1])
				conf = strings.TrimPrefix(conf, "**Confidence:**")
				conf = strings.TrimSpace(conf)
				dim.Confidence = Confidence(strings.ToLower(conf))
			}
		case strings.HasPrefix(t, "**Directive:**"):
			dim.Directive = strings.TrimSpace(strings.TrimPrefix(t, "**Directive:**"))
		}
	}
	flush()
	return p
}

func normaliseDim(s string) string {
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, " style", "")
	s = strings.ReplaceAll(s, " philosophy", "")
	s = strings.ReplaceAll(s, " triggers", "")
	s = strings.ReplaceAll(s, " ", "_")
	return s
}
