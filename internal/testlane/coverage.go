package testlane

import "path"

// buildConfigNames are the base names a scoped green can never vouch for, at any
// depth. A change to one of these can move every test in the repo while the
// tests related to the file itself stay green, so neither a covered entry nor an
// exempt glob reaches them: only a full run measures what they changed.
var buildConfigNames = []string{
	"package.json",
	"package-lock.json",
	"pnpm-lock.yaml",
	"yarn.lock",
	"go.mod",
	"go.sum",
	"Makefile",
	"tsconfig*.json",
	"*.config.js",
	"*.config.ts",
	"*.config.mjs",
	"*.config.cjs",
	"Dockerfile*",
	"docker-compose*.yml",
}

// NeedsFullGreen returns the changed paths a scoped green cannot admit: build
// config, out-of-tree paths, and anything neither covered nor matched by an
// exempt glob. An empty result means the scoped green is enough.
//
// Paths come back in the caller's spelling and input order, deduplicated by
// normalized form, because the refusal reads them to a human. Comparison is on
// the normalized form, so `./src/a.ts` meets `src/a.ts` in covered.
func NeedsFullGreen(changed, covered, exempt []string) []string {
	vouched := make(map[string]bool, len(covered))
	for _, c := range covered {
		if norm, inTree := normalize(c); inTree && norm != "" {
			vouched[norm] = true
		}
	}

	var out []string
	seen := map[string]bool{}
	for _, raw := range changed {
		norm, inTree := normalize(raw)
		if seen[norm] {
			continue
		}
		seen[norm] = true
		if !inTree || isBuildConfig(norm) || !(vouched[norm] || MatchAny(exempt, norm)) {
			out = append(out, raw)
		}
	}
	return out
}

// isBuildConfig reports whether a normalized path's base name is on the fixed
// list. The patterns are constants, so path.Match's only error cannot occur.
func isBuildConfig(norm string) bool {
	base := path.Base(norm)
	for _, pattern := range buildConfigNames {
		if ok, _ := path.Match(pattern, base); ok {
			return true
		}
	}
	return false
}
