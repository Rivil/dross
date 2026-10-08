// Package secretpath decides whether a file path names a secret, against the
// secret-path patterns gates.toml extends. It is a leaf: the secret-read gate
// and the solo review context share this one implementation.
package secretpath

import (
	"path"
	"path/filepath"
	"strings"
)

// envTemplateSuffixes mark an env file that holds placeholders, not values.
var envTemplateSuffixes = []string{".example", ".sample", ".template", ".dist"}

// Pattern returns the first pattern p matches, or "" — an env template
// (.env.example and kin) never matches. A pattern with a slash matches p's
// trailing path segments; one without matches its base name.
func Pattern(p string, patterns []string) string {
	clean := path.Clean(filepath.ToSlash(p))
	base := path.Base(clean)
	if strings.Contains(base, ".env") {
		for _, sfx := range envTemplateSuffixes {
			if strings.HasSuffix(base, sfx) {
				return ""
			}
		}
	}
	for _, pat := range patterns {
		if strings.Contains(pat, "/") {
			if clean == pat || strings.HasSuffix(clean, "/"+pat) {
				return pat
			}
			continue
		}
		if ok, _ := path.Match(pat, base); ok {
			return pat
		}
	}
	return ""
}
