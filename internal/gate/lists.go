package gate

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

// Lists are what the list-driven gates match against: built-in defaults
// (gate_list_source) unioned with the user-level gates.toml. The extension is
// additive only — an empty list there removes nothing — because the secret
// guards fire outside every dross repo (guard_scope), where no project.toml
// exists to hold a list, and a list a stray file could shrink protects nothing.
type Lists struct {
	// SecretTools are programs whose output is a secret (c-6).
	SecretTools []string
	// SecretPaths are file patterns whose contents are secret (c-7): a
	// pattern with a slash matches a path's trailing segments, one without
	// matches the base name.
	SecretPaths []string
	// CuratedFiles are .dross-relative patterns of hand-curated files a
	// whole-file rewrite must not shrink (c-8).
	CuratedFiles []string
	// Path is the gates.toml consulted.
	Path string
	// Err is set when gates.toml exists but cannot be read or parsed. The
	// defaults still apply; the gates whose lists it extends refuse the calls
	// they claim, naming the file, rather than judge on a partial list.
	Err error
}

// Defaults are the built-in lists.
func Defaults() Lists {
	return Lists{
		SecretTools: []string{"pass-cli"},
		SecretPaths: []string{
			"*.env", ".env*", "*.pem", "*.key", "*.agekey",
			"id_rsa", "id_dsa", "id_ecdsa", "id_ed25519",
			"id_rsa_sk", "id_dsa_sk", "id_ecdsa_sk", "id_ed25519_sk",
			"sops/age/keys.txt",
		},
		CuratedFiles: []string{
			"project.toml", "milestones/*.toml", "phases/*/spec.toml", "handoff.md", "rules.toml",
			// /dross-debug sessions: narrative evidence kept with Edit, which a
			// regenerated Write would silently lose.
			"debug/*.md",
		},
	}
}

// listFile is gates.toml.
type listFile struct {
	SecretTools  []string `toml:"secret_tools"`
	SecretPaths  []string `toml:"secret_paths"`
	CuratedFiles []string `toml:"curated_files"`
}

// ListsPath is the user-level extension file under home.
func ListsPath(home string) string {
	return filepath.Join(home, ".claude", "dross", "gates.toml")
}

// LoadLists returns the defaults unioned with home's gates.toml. A missing
// file is no extension; an unreadable or malformed one — an unknown key
// included, since a misspelt list would silently guard nothing — sets Err.
func LoadLists(home string) Lists {
	l := Defaults()
	if home == "" {
		return l
	}
	l.Path = ListsPath(home)
	b, err := os.ReadFile(l.Path)
	if errors.Is(err, fs.ErrNotExist) {
		return l
	}
	if err != nil {
		l.Err = fmt.Errorf("read %s: %w", l.Path, err)
		return l
	}
	var f listFile
	md, err := toml.Decode(string(b), &f)
	if err != nil {
		l.Err = fmt.Errorf("parse %s: %w", l.Path, err)
		return l
	}
	if extra := md.Undecoded(); len(extra) > 0 {
		keys := make([]string, len(extra))
		for i, k := range extra {
			keys[i] = k.String()
		}
		sort.Strings(keys)
		l.Err = fmt.Errorf("parse %s: unknown key(s) %s (want secret_tools, secret_paths, curated_files)", l.Path, strings.Join(keys, ", "))
		return l
	}
	l.SecretTools = union(l.SecretTools, f.SecretTools)
	l.SecretPaths = union(l.SecretPaths, f.SecretPaths)
	l.CuratedFiles = union(l.CuratedFiles, f.CuratedFiles)
	return l
}

func union(base, extra []string) []string {
	out := append([]string(nil), base...)
	seen := map[string]bool{}
	for _, s := range base {
		seen[s] = true
	}
	for _, s := range extra {
		if s = strings.TrimSpace(s); s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}
