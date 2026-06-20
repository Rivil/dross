package stack

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// embeddedProfiles holds the built-in stack profiles compiled into the binary, so
// a fresh install ships a working Go profile with no external files (honoring the
// locked single-static-binary stack decision). User profiles in ~/.claude/dross/
// profiles/ extend or override these (profile_home decision).
//
//go:embed profiles/*.toml
var embeddedProfiles embed.FS

// UserProfileDir is the directory user-supplied profiles are read from. Dropping a
// <stack>.toml here is the entire mechanism for adding or overriding a stack — no
// recompile, zero code change (the c-5 keystone).
func UserProfileDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude", "dross", "profiles"), nil
}

// Embedded parses and validates the built-in profiles.
func Embedded() ([]*Profile, error) {
	entries, err := embeddedProfiles.ReadDir("profiles")
	if err != nil {
		return nil, fmt.Errorf("read embedded profiles: %w", err)
	}
	var out []*Profile
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".toml") {
			continue
		}
		data, err := embeddedProfiles.ReadFile("profiles/" + e.Name())
		if err != nil {
			return nil, fmt.Errorf("read embedded %s: %w", e.Name(), err)
		}
		p, err := Decode(data)
		if err != nil {
			return nil, fmt.Errorf("embedded %s: %w", e.Name(), err)
		}
		out = append(out, p)
	}
	return out, nil
}

// LoadFromDir reads and validates every *.toml profile in dir. A missing dir is
// not an error (returns nil, nil) — most repos have no user profiles. A malformed
// file returns the profiles parsed so far plus an error naming the offending file,
// so the caller can keep going with the valid set rather than losing everything.
func LoadFromDir(dir string) ([]*Profile, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read profile dir %s: %w", dir, err)
	}
	var out []*Profile
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".toml") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		p, err := Load(path)
		if err != nil {
			return out, err // Load already wraps with the file path
		}
		out = append(out, p)
	}
	return out, nil
}

// Merge overlays user profiles on embedded ones by id — the user profile wins on a
// collision (profile_home decision). The result is sorted by id for stable output.
func Merge(embedded, user []*Profile) []*Profile {
	byID := map[string]*Profile{}
	for _, p := range embedded {
		byID[p.ID] = p
	}
	for _, p := range user {
		byID[p.ID] = p
	}
	out := make([]*Profile, 0, len(byID))
	for _, p := range byID {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// LoadAll returns the merged profile set: embedded built-ins overlaid by user-dir
// profiles. A malformed user profile is surfaced as an error but never silently
// drops the embedded set — the returned slice still contains the built-ins, so a
// caller can warn and proceed.
func LoadAll() ([]*Profile, error) {
	dir, err := UserProfileDir()
	if err != nil {
		return Embedded()
	}
	return loadAllFrom(dir)
}

func loadAllFrom(userDir string) ([]*Profile, error) {
	emb, err := Embedded()
	if err != nil {
		return nil, err
	}
	user, uerr := LoadFromDir(userDir)
	merged := Merge(emb, user)
	if uerr != nil {
		return merged, fmt.Errorf("user profile: %w", uerr)
	}
	return merged, nil
}

// ByID returns the profile with the given id, or nil if absent.
func ByID(profiles []*Profile, id string) *Profile {
	for _, p := range profiles {
		if p.ID == id {
			return p
		}
	}
	return nil
}
