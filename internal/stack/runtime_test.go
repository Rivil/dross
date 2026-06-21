package stack

import (
	"errors"
	"runtime"
	"testing"
)

// fakeLookPath returns a lookPath that reports only the named bins as present.
func fakeLookPath(present ...string) func(string) (string, error) {
	set := map[string]bool{}
	for _, p := range present {
		set[p] = true
	}
	return func(bin string) (string, error) {
		if set[bin] {
			return "/usr/bin/" + bin, nil
		}
		return "", errors.New("not found")
	}
}

func TestGoRuntimeMatchesLocked(t *testing.T) {
	emb, err := Embedded()
	if err != nil {
		t.Fatalf("Embedded: %v", err)
	}
	gp := ByID(emb, "go")
	if gp == nil {
		t.Fatal("no go profile")
	}
	rt := ResolveRuntime(gp, runtime.GOOS, fakeLookPath())
	want := ResolvedRuntime{
		Test:      "go test -count=1 ./...",
		Typecheck: "go vet ./...",
		Format:    "gofmt -l .",
		Build:     "make build",
	}
	if rt != want {
		t.Errorf("go runtime mismatch:\n got %+v\nwant %+v", rt, want)
	}

	// Editing a command in the profile changes the derived value — proving the
	// value flows from profile data, not a hardcoded default.
	edited, err := Decode([]byte("id = \"go\"\n[runtime.test]\n  run = \"go test -race ./...\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got := ResolveCommand(edited.Runtime.Test, runtime.GOOS, fakeLookPath()); got != "go test -race ./..." {
		t.Errorf("edited command not reflected: got %q", got)
	}
}

// TestRuntimeSeededForLanguageProfiles proves the stacks meant to be runnable
// declare a runtime command set: ResolveRuntime over each yields a non-empty Test
// and Build, so init/onboard/apply seed project.toml [runtime] from them.
func TestRuntimeSeededForLanguageProfiles(t *testing.T) {
	emb, err := Embedded()
	if err != nil {
		t.Fatalf("Embedded: %v", err)
	}
	for _, id := range []string{"kotlin", "dart", "svelte", "typescript"} {
		t.Run(id, func(t *testing.T) {
			p := ByID(emb, id)
			if p == nil {
				t.Fatalf("no %q profile", id)
			}
			rt := ResolveRuntime(p, runtime.GOOS, fakeLookPath())
			if rt.Test == "" {
				t.Errorf("%s profile declares no runtime.test", id)
			}
			if rt.Build == "" {
				t.Errorf("%s profile declares no runtime.build", id)
			}
		})
	}
}

// TestSQLProfileHasNoRuntime pins the seed_runtime decision: SQL has no
// meaningful test/build runtime, so it declares none. A sneaked-in [runtime]
// block in sql.toml makes Test/Build non-empty and fails this.
func TestSQLProfileHasNoRuntime(t *testing.T) {
	emb, err := Embedded()
	if err != nil {
		t.Fatalf("Embedded: %v", err)
	}
	p := ByID(emb, "sql")
	if p == nil {
		t.Fatal("no sql profile")
	}
	rt := ResolveRuntime(p, runtime.GOOS, fakeLookPath())
	if rt.Test != "" || rt.Build != "" {
		t.Errorf("sql profile must declare no runtime; got Test=%q Build=%q", rt.Test, rt.Build)
	}
}

func TestRuntimeSlotPicksAvailableVariant(t *testing.T) {
	slot := Command{Variants: []CommandVariant{
		{Run: "pnpm test", Bin: "pnpm"},
		{Run: "npm test", Bin: "npm"},
	}}

	// pnpm missing, npm present → must pick npm, exactly, not concatenated.
	got := ResolveCommand(slot, runtime.GOOS, fakeLookPath("npm"))
	if got != "npm test" {
		t.Fatalf("want the first available variant 'npm test', got %q", got)
	}

	// pnpm present → must prefer the first variant.
	if got := ResolveCommand(slot, runtime.GOOS, fakeLookPath("pnpm", "npm")); got != "pnpm test" {
		t.Errorf("want preferred 'pnpm test', got %q", got)
	}

	// None available → fall back to the first variant's command, never empty.
	if got := ResolveCommand(slot, runtime.GOOS, fakeLookPath()); got != "pnpm test" {
		t.Errorf("want fallback 'pnpm test', got %q", got)
	}
}
