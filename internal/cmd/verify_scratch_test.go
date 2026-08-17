package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Rivil/dross/internal/mutation"
	"github.com/Rivil/dross/internal/project"
)

// adapterCacheVars pulls the resolved cache vars back off whichever adapter
// carries them, so the assertion is about what the RUN will use rather than
// about an intermediate value.
func adapterCacheVars(t *testing.T, adapters []mutation.Adapter) map[string][]string {
	t.Helper()
	out := map[string][]string{}
	for _, a := range adapters {
		switch v := a.(type) {
		case *mutation.Gremlins:
			out["gremlins"] = v.CacheVars
		case *mutation.Stryker:
			out["stryker"] = v.CacheVars
		case *mutation.StrykerNet:
			out["stryker-net"] = v.CacheVars
		}
	}
	return out
}

// TestAdaptersCarryProfileCacheVars: the vars reach every adapter, resolved
// from the stack profile rather than from a constant in this package. A
// hardcoded GOCACHE here would work today and make adding rust a code change,
// which is the thing the locked cache_var_source decision forbids.
func TestAdaptersCarryProfileCacheVars(t *testing.T) {
	root := wiringFixture(t, nil, false)
	p := loadWiringProject(t, root)
	p.Stack.Profile = "go"

	adapters, _, err := configuredAdapters(p, root, false)
	if err != nil {
		t.Fatal(err)
	}
	got := adapterCacheVars(t, adapters)
	if len(got) == 0 {
		t.Fatal("no adapters were built")
	}
	for name, vars := range got {
		found := false
		for _, v := range vars {
			if v == "GOCACHE" {
				found = true
			}
		}
		if !found {
			t.Errorf("adapter %s carries %v, want GOCACHE from the go profile", name, vars)
		}
	}
}

// TestNoProfileCacheVarsIsEmpty: a stack whose profile declares none must leave
// its runs exactly as they are. Injecting a var on its behalf would point a
// toolchain that never reads it at a directory nobody writes to.
func TestNoProfileCacheVarsIsEmpty(t *testing.T) {
	root := wiringFixture(t, nil, false)
	p := loadWiringProject(t, root)
	// javascript ships without a mutation_cache block today; if that changes,
	// the fixture below (an id nothing declares) still holds the property.
	p.Stack.Profile = "no-such-stack-profile"

	adapters, _, err := configuredAdapters(p, root, false)
	if err != nil {
		t.Fatal(err)
	}
	for name, vars := range adapterCacheVars(t, adapters) {
		if len(vars) != 0 {
			t.Errorf("adapter %s carries %v for a profile that declares none", name, vars)
		}
	}
}

// TestMissingProfileDoesNotBreakVerify: disk hygiene must never be able to stop
// a verify. A repo whose stack does not resolve still gets adapters — just no
// scratch — rather than an error.
func TestMissingProfileDoesNotBreakVerify(t *testing.T) {
	for _, tc := range []struct {
		name    string
		profile string
	}{
		{"unset", ""},
		{"whitespace", "   "},
		{"unknown id", "klingon"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := wiringFixture(t, nil, false)
			p := loadWiringProject(t, root)
			p.Stack.Profile = tc.profile

			adapters, _, err := configuredAdapters(p, root, false)
			if err != nil {
				t.Fatalf("an unresolvable stack profile failed the run: %v", err)
			}
			if len(adapters) == 0 {
				t.Fatal("no adapters were built — verify would measure nothing")
			}
			if got := profileCacheVars(p, t.TempDir()); len(got) != 0 {
				t.Errorf("profileCacheVars = %v, want none", got)
			}
		})
	}
	// A nil project must not panic: the resolver runs on paths that predate a
	// loaded config.
	if got := profileCacheVars(nil, t.TempDir()); got != nil {
		t.Errorf("profileCacheVars(nil) = %v, want nil", got)
	}
}

// TestProfileCacheVarsComesFromTheProfile: read straight from the embedded
// profile, so a shipped go.toml that lost its declaration fails here rather
// than silently reverting every Go repo to the shared cache.
func TestProfileCacheVarsComesFromTheProfile(t *testing.T) {
	p := &project.Project{}
	p.Stack.Profile = "go"

	got := profileCacheVars(p, "")
	if len(got) == 0 {
		t.Fatal("the go profile resolved no cache vars")
	}
	if got[0] != "GOCACHE" {
		t.Errorf("resolved %v, want GOCACHE first", got)
	}
}

// TestScratchLeavesNothingBesideTheProject: the end-state property, asserted on
// the filesystem. A run that finished must leave no cache directory behind —
// that accumulation is what reached 399 GB and filled the disk.
func TestScratchLeavesNothingBesideTheProject(t *testing.T) {
	root := wiringFixture(t, nil, false)
	repoDir := filepath.Dir(root)
	p := loadWiringProject(t, root)
	p.Stack.Profile = "go"

	if _, _, err := configuredAdapters(p, root, false); err != nil {
		t.Fatal(err)
	}
	// Building the adapters must not itself create anything — the scratch is
	// made on first use inside a run and wiped when that run ends.
	if _, err := os.Stat(filepath.Join(repoDir, ".dross-cache")); !os.IsNotExist(err) {
		t.Errorf("resolving adapters created a cache dir before any run: %v", err)
	}
}

// TestProfileCacheVarsFallsBackToDetection: stack.profile is newer than most
// project.toml files — dross's OWN does not carry it — so resolving only the
// recorded id would leave this feature silently doing nothing on every repo
// onboarded before the field existed. That is indistinguishable from never
// having wired it up, and it is exactly the dead-config failure this repo has a
// phase about.
func TestProfileCacheVarsFallsBackToDetection(t *testing.T) {
	repoDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(repoDir, "go.mod"), []byte("module x\n\ngo 1.25\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	p := &project.Project{} // no Stack.Profile recorded, as most repos have
	got := profileCacheVars(p, repoDir)
	if len(got) == 0 {
		t.Fatal("a Go repo with no recorded stack.profile resolved no cache vars — the feature is inert wherever the field was never written")
	}
	if got[0] != "GOCACHE" {
		t.Errorf("detected profile resolved %v, want GOCACHE", got)
	}

	// A recorded id still wins over detection.
	p.Stack.Profile = "klingon"
	if vars := profileCacheVars(p, repoDir); len(vars) != 0 {
		t.Errorf("detection overrode the recorded id: %v", vars)
	}

	// And a directory that detects as nothing stays empty rather than guessing.
	if vars := profileCacheVars(&project.Project{}, t.TempDir()); len(vars) != 0 {
		t.Errorf("an unrecognisable repo resolved %v", vars)
	}
}
