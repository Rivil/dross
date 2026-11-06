package testlane

import (
	"reflect"
	"testing"
)

// TestNeedsFullGreen holds the classifier's precedence: build config beats
// coverage and exempt, and everything else needs one of the two.
func TestNeedsFullGreen(t *testing.T) {
	cases := []struct {
		name    string
		changed []string
		covered []string
		exempt  []string
		want    []string
	}{
		{
			name:    "exempt does not beat the config list",
			changed: []string{"package.json"},
			covered: []string{"package.json"},
			exempt:  []string{"*.json"},
			want:    []string{"package.json"},
		},
		{
			name:    "coverage does not beat the config list at depth",
			changed: []string{"src/vite.config.ts"},
			covered: []string{"src/vite.config.ts"},
			want:    []string{"src/vite.config.ts"},
		},
		{
			name:    "lockfile under an exempt glob",
			changed: []string{"package-lock.json"},
			exempt:  []string{"*.json"},
			want:    []string{"package-lock.json"},
		},
		{
			name:    "spelling does not split a covered path",
			changed: []string{"./src/a.ts"},
			covered: []string{"src/a.ts"},
		},
		{
			name:    "covered spelling is normalized too",
			changed: []string{"src/a.ts"},
			covered: []string{"./src/a.ts"},
		},
		{
			name:    "exempt glob admits under its directory",
			changed: []string{"docs/x.md"},
			exempt:  []string{"docs/**"},
		},
		{
			name:    "exempt glob stops at its directory",
			changed: []string{"src/docs.md"},
			exempt:  []string{"docs/**"},
			want:    []string{"src/docs.md"},
		},
		{
			name:    "empty cover returns every non-exempt path in input order",
			changed: []string{"z.ts", "docs/x.md", "a.ts"},
			exempt:  []string{"docs/**"},
			want:    []string{"z.ts", "a.ts"},
		},
		{
			name:    "only the uncovered path is named",
			changed: []string{"a.ts", "b.ts"},
			covered: []string{"a.ts"},
			want:    []string{"b.ts"},
		},
		{
			name:    "caller's spelling comes back, once",
			changed: []string{"./b.ts", "b.ts"},
			want:    []string{"./b.ts"},
		},
		{
			name:    "out-of-tree is never covered",
			changed: []string{"../x.ts"},
			covered: []string{"../x.ts"},
			exempt:  []string{"**/*.ts"},
			want:    []string{"../x.ts"},
		},
		{
			name:    "a blank covered entry vouches for nothing",
			changed: []string{""},
			covered: []string{""},
			want:    []string{""},
		},
		{
			name: "nothing changed needs nothing",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := NeedsFullGreen(c.changed, c.covered, c.exempt)
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("NeedsFullGreen(%q, %q, %q) = %q, want %q", c.changed, c.covered, c.exempt, got, c.want)
			}
		})
	}
}

// TestBuildConfigListHasNoGap walks every entry on the list, each covered, at
// the root and at depth, so a dropped or misspelled pattern fails by name.
func TestBuildConfigListHasNoGap(t *testing.T) {
	for _, base := range []string{
		"package.json",
		"package-lock.json",
		"pnpm-lock.yaml",
		"yarn.lock",
		"go.mod",
		"go.sum",
		"Makefile",
		"tsconfig.json",
		"tsconfig.build.json",
		"svelte.config.js",
		"vite.config.ts",
		"vitest.mutation.config.ts",
		"postcss.config.mjs",
		"eslint.config.cjs",
		"vitest.config.mts",
		"vite.config.cts",
		"Dockerfile",
		"Dockerfile.test",
		"docker-compose.yml",
		"docker-compose.test.yml",
		"docker-compose.yaml",
		"docker-compose.test.yaml",
		"compose.yml",
		"compose.yaml",
		"compose.override.yml",
		"compose.override.yaml",
	} {
		for _, p := range []string{base, "apps/web/" + base} {
			got := NeedsFullGreen([]string{p}, []string{p}, []string{"**/" + base})
			if !reflect.DeepEqual(got, []string{p}) {
				t.Errorf("%s in covered and exempt: got %q, want it to need a full green", p, got)
			}
		}
	}
}

// TestBuildConfigListStaysNarrow is the other direction: a list that matched
// ordinary source would refuse every commit a scoped green should admit.
func TestBuildConfigListStaysNarrow(t *testing.T) {
	for _, p := range []string{
		"src/config.ts",
		"src/app.config.json",
		"src/package.ts",
		"go.mod.md",
		"src/Makefile.md",
		"docs/tsconfig.md",
		"src/docker-compose.md",
		"src/compose.ts",
		"src/compose.md",
		"config/composer.yml",
		"config/composer.yaml",
	} {
		if got := NeedsFullGreen([]string{p}, []string{p}, nil); got != nil {
			t.Errorf("%s in covered: got %q, want it admitted", p, got)
		}
	}
}
