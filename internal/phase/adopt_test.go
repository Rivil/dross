package phase

import (
	"os"
	"path/filepath"
	"testing"
)

// placeholderRoot is the shape `deferred route --target` and `milestone add
// phases` leave behind: a phase directory with nothing in it yet.
func placeholderRoot(t *testing.T, slug string, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "phases", slug)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// TestResolveAdoptsAnUnstartedPhase is c-1 at the resolver.
//
// The placeholder is exactly what produced `<slug>-2`: a directory that exists
// because someone routed a deferred item at it, holding none of the files the
// loop writes as a phase begins.
func TestResolveAdoptsAnUnstartedPhase(t *testing.T) {
	root := placeholderRoot(t, "survivor-drain", nil)

	slug, d := CreateSlug(root, "Survivor drain")
	if slug != "survivor-drain" {
		t.Errorf("slug = %q, want survivor-drain — a coined suffix is the bug this replaced", slug)
	}
	if d != SlugAdopt {
		t.Errorf("disposition = %v, want SlugAdopt", d)
	}
}

// TestResolveRefusesAStartedPhase, per marker file. Asserting one marker would
// let the other two silently stop counting, and each of the three is enough on
// its own to mean someone started the phase.
func TestResolveRefusesAStartedPhase(t *testing.T) {
	for _, marker := range []string{"spec.toml", "plan.toml", "changes.json"} {
		t.Run(marker, func(t *testing.T) {
			root := placeholderRoot(t, "survivor-drain", map[string]string{marker: "{}\n"})

			slug, d := CreateSlug(root, "Survivor drain")
			if d != SlugOccupied {
				t.Errorf("a phase holding %s resolved as %v, want SlugOccupied — adopting would retitle someone's in-flight phase", marker, d)
			}
			if slug != "survivor-drain" {
				t.Errorf("slug = %q — the refusal must name the phase that is actually there", slug)
			}
		})
	}
}

// TestStrayFileDoesNotBlockAdoption is the other direction, and it is why the
// rule keys on the loop's own files rather than on the directory being empty.
//
// An editor swap file or a stray README in a placeholder must not make it look
// occupied — that would re-open the duplicate-coining path this phase closes,
// through a file nobody meant as a signal.
func TestStrayFileDoesNotBlockAdoption(t *testing.T) {
	for _, stray := range []string{"README.md", ".spec.toml.swp", "notes.txt", "panel"} {
		t.Run(stray, func(t *testing.T) {
			root := placeholderRoot(t, "survivor-drain", map[string]string{stray: "x"})

			if _, d := CreateSlug(root, "Survivor drain"); d != SlugAdopt {
				t.Errorf("a placeholder holding %q resolved as %v, want SlugAdopt", stray, d)
			}
		})
	}
}

// TestResolveNeverCoinsASuffix: the auto-coined slug is the bug, not a fallback
// for it. No input to CreateSlug may produce one.
func TestResolveNeverCoinsASuffix(t *testing.T) {
	for _, files := range []map[string]string{
		nil,
		{"spec.toml": ""},
		{"README.md": "x"},
	} {
		root := placeholderRoot(t, "survivor-drain", files)
		slug, _ := CreateSlug(root, "Survivor drain")
		if slug == "survivor-drain-2" {
			t.Errorf("CreateSlug coined %q for files %v", slug, files)
		}
	}
}

// TestFreeSlugIsUnchanged: adoption is additive. A title whose slug is free
// must resolve exactly as it always did.
func TestFreeSlugIsUnchanged(t *testing.T) {
	root := t.TempDir()
	slug, d := CreateSlug(root, "Brand New Thing")
	if slug != "brand-new-thing" {
		t.Errorf("slug = %q, want brand-new-thing", slug)
	}
	if d != SlugFree {
		t.Errorf("disposition = %v, want SlugFree", d)
	}
	if slug != Slugify("Brand New Thing") {
		t.Error("CreateSlug and Slugify disagree on a free title — every phase id in the repo comes from one of them")
	}
}

// TestUniqueSlugStillCoins: the old helper is retained for the lifecycle verbs
// that genuinely need a free slug, and it must keep doing what they rely on.
func TestUniqueSlugStillCoins(t *testing.T) {
	root := placeholderRoot(t, "taken", nil)
	if got := UniqueSlug(root, "Taken"); got != "taken-2" {
		t.Errorf("UniqueSlug = %q, want taken-2 — its callers depend on a free slug", got)
	}
}
