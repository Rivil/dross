package techdebt

import (
	"fmt"
	"path"
	"path/filepath"
	"strings"
)

// Filter drops from paths every entry matched by one of excludes — the
// [techdebt] exclude list from project.toml — and returns the survivors in
// their original order. Each path is compared in repoDir-relative slash form,
// so a pattern is written the way it appears in the repo, never against the
// absolute prefix the enumerator attached. Entry semantics:
//
//   - an entry ending in "/" is a directory prefix: "internal/techdebt/" drops
//     everything beneath that directory and nothing beside it, so
//     internal/techdebtx/a.go and internal/techdebt.go both survive;
//   - any other entry is a path.Match glob against the relative path and, when
//     the pattern carries no "/", also against the base name, so "*.golden"
//     reaches docs/a.golden as well as a.golden while "docs/*.md" stays
//     anchored (docs/sub/a.md survives it).
//
// A pattern that does not compile is returned as `techdebt.exclude %q: <err>`
// with a nil slice — never swallowed and never the unfiltered input, so a
// typo cannot silently widen the scan. nil or empty excludes return the input
// unchanged. Pure: no disk access and no stack import — the shared skip set is
// the enumerator's concern, applied before this runs.
func Filter(repoDir string, paths, excludes []string) ([]string, error) {
	if len(excludes) == 0 {
		return paths, nil
	}
	// Surface a bad pattern before touching any path, so an empty path set
	// still reports it.
	for _, ex := range excludes {
		if strings.HasSuffix(ex, "/") {
			continue
		}
		if _, err := path.Match(ex, ""); err != nil {
			return nil, fmt.Errorf("techdebt.exclude %q: %w", ex, err)
		}
	}
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		rel := relSlash(repoDir, p)
		if excluded(rel, excludes) {
			continue
		}
		out = append(out, p)
	}
	return out, nil
}

// relSlash returns p relative to repoDir in slash form. A path that cannot be
// made relative is compared as given (slash form), so it can only match a
// pattern that spells it that way.
func relSlash(repoDir, p string) string {
	if rel, err := filepath.Rel(repoDir, p); err == nil {
		return filepath.ToSlash(rel)
	}
	return filepath.ToSlash(p)
}

// excluded reports whether rel is matched by any entry. Patterns were
// validated by Filter, so path.Match cannot error here.
func excluded(rel string, excludes []string) bool {
	base := path.Base(rel)
	for _, ex := range excludes {
		if strings.HasSuffix(ex, "/") {
			if strings.HasPrefix(rel, ex) {
				return true
			}
			continue
		}
		if ok, _ := path.Match(ex, rel); ok {
			return true
		}
		if !strings.Contains(ex, "/") {
			if ok, _ := path.Match(ex, base); ok {
				return true
			}
		}
	}
	return false
}
