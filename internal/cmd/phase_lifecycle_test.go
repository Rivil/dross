package cmd

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"testing"

	"github.com/Rivil/dross/internal/milestone"
	"github.com/Rivil/dross/internal/phase"
)

// snapshotPhases fingerprints every phase under a .dross root: for each slug it
// hashes the sorted (relpath, length, bytes) of every file in phases/<slug>/
// PLUS the slug's milestone-array slot (which milestone file lists it and at
// what index). The array slot is included so the byte-for-byte guarantee also
// catches a sibling's order changing in a milestone .toml — a change that lives
// outside the phase directory. Returns slug → hex sha256.
func snapshotPhases(t *testing.T, drossRoot string) map[string]string {
	t.Helper()
	slugs, err := phase.List(drossRoot)
	if err != nil {
		t.Fatalf("snapshotPhases: list phases: %v", err)
	}

	// slug → "<milestone-file>#<index>" for every slug named in any milestone array.
	arraySlot := map[string]string{}
	metas, _ := filepath.Glob(filepath.Join(drossRoot, "milestones", "*.toml"))
	sort.Strings(metas)
	for _, mp := range metas {
		m, err := milestone.Load(mp)
		if err != nil {
			continue
		}
		for i, s := range m.Phases {
			arraySlot[s] = fmt.Sprintf("%s#%d", filepath.Base(mp), i)
		}
	}

	out := make(map[string]string, len(slugs))
	for _, slug := range slugs {
		h := sha256.New()
		dir := filepath.Join(drossRoot, "phases", slug)
		var files []string
		_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !d.IsDir() {
				files = append(files, p)
			}
			return nil
		})
		sort.Strings(files)
		for _, f := range files {
			rel, _ := filepath.Rel(dir, f)
			b, _ := os.ReadFile(f)
			fmt.Fprintf(h, "%s\x00%d\x00", rel, len(b))
			h.Write(b)
		}
		fmt.Fprintf(h, "array\x00%s", arraySlot[slug])
		out[slug] = hex.EncodeToString(h.Sum(nil))
	}
	return out
}

// diffSnapshots returns the sorted slugs whose fingerprint changed, appeared, or
// disappeared between before and after, ignoring any slug in except. Pure, so it
// is directly testable without a mock *testing.T.
func diffSnapshots(before, after map[string]string, except ...string) []string {
	skip := make(map[string]bool, len(except))
	for _, e := range except {
		skip[e] = true
	}
	changed := map[string]bool{}
	for slug, h := range before {
		if !skip[slug] && after[slug] != h {
			changed[slug] = true
		}
	}
	for slug := range after {
		if skip[slug] {
			continue
		}
		if _, ok := before[slug]; !ok {
			changed[slug] = true
		}
	}
	out := make([]string, 0, len(changed))
	for s := range changed {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// assertUntouched fails the test if any phase outside except changed between two
// snapshots — the byte-for-byte guarantee recurring across c-1/c-2/c-3.
func assertUntouched(t *testing.T, before, after map[string]string, except ...string) {
	t.Helper()
	if changed := diffSnapshots(before, after, except...); len(changed) > 0 {
		t.Errorf("phases unexpectedly changed (byte-for-byte): %v (excepted: %v)", changed, except)
	}
}

// TestSnapshotHarnessSelfCheck proves the harness is not vacuous: a one-byte
// edit to a bystander spec must flip its fingerprint AND be reported by
// diffSnapshots, while excepting that slug suppresses the report.
func TestSnapshotHarnessSelfCheck(t *testing.T) {
	dir := setupDeferredFixture(t)
	root := filepath.Join(dir, ".dross")

	before := snapshotPhases(t, root)

	specPath := filepath.Join(root, "phases", "beta", "spec.toml")
	b, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(specPath, append(b, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}

	after := snapshotPhases(t, root)

	if before["beta"] == after["beta"] {
		t.Error("snapshotPhases is blind: a one-byte change to beta/spec.toml did not flip its fingerprint")
	}
	if changed := diffSnapshots(before, after); !slices.Contains(changed, "beta") {
		t.Errorf("diffSnapshots missed the bystander change: got %v, want it to include beta", changed)
	}
	if rest := diffSnapshots(before, after, "beta"); len(rest) != 0 {
		t.Errorf("only beta changed, but excepting beta still reports: %v", rest)
	}
}
