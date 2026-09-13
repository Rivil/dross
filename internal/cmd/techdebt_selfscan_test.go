package cmd

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/project"
	"github.com/Rivil/dross/internal/techdebt"
)

// underDir reports whether abs lies beneath the repo-relative directory rel.
func underDir(root, abs, rel string) bool {
	r, err := filepath.Rel(root, abs)
	if err != nil {
		return false
	}
	return strings.HasPrefix(filepath.ToSlash(r), rel)
}

// TestTechdebtSelfScanExcludesOwnPackage is c-2 on the real repo: with the
// tracked .dross/project.toml exclude list applied, the tech-debt scan reports
// nothing from internal/techdebt/ — whose marker regex, package doc and test
// fixtures spell every marker word — while the unfiltered set does (the
// negative control that fails by name if the project.toml line or the Filter
// call goes missing). Bounds on the drop guard against a match-all exclude
// passing vacuously.
func TestTechdebtSelfScanExcludesOwnPackage(t *testing.T) {
	root := repoRootFromTest(t)
	raw, err := trackedFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	proj, err := project.Load(filepath.Join(root, ".dross", "project.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(proj.Techdebt.Exclude) == 0 {
		t.Fatal(".dross/project.toml carries no [techdebt] exclude — dross's own package must be listed")
	}
	filtered, err := techdebt.Filter(root, raw, proj.Techdebt.Exclude)
	if err != nil {
		t.Fatal(err)
	}

	const pkg = "internal/techdebt/"
	for _, f := range techdebt.Scan(filtered, techdebt.DefaultThresholds) {
		if underDir(root, f.File, pkg) {
			t.Errorf("finding re-admitted from the scanner's own package: [%s] %s:%d — %s", f.Class, f.File, f.Line, f.Detail)
		}
	}
	markers := 0
	for _, f := range techdebt.Scan(raw, techdebt.DefaultThresholds) {
		if f.Class == techdebt.ClassMarker && underDir(root, f.File, pkg) {
			markers++
		}
	}
	if markers == 0 {
		t.Fatal("negative control: the unfiltered scan found no marker under internal/techdebt/, so the exclusion proves nothing")
	}

	dropped := len(raw) - len(filtered)
	switch {
	case len(filtered) == 0:
		t.Fatal("the exclude list emptied the scan")
	case dropped < 6:
		t.Fatalf("only %d files dropped; internal/techdebt/ alone tracks more than that", dropped)
	case dropped >= len(raw)/2:
		t.Fatalf("%d of %d files dropped — the exclude list is over-broad", dropped, len(raw))
	}
}

// TestTechdebtDrossTreeNoFixtureFindings is c-3 on the real repo: no finding
// names a path under a testdata/ or fixtures/ directory.
func TestTechdebtDrossTreeNoFixtureFindings(t *testing.T) {
	root := repoRootFromTest(t)
	paths, err := trackedFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range techdebt.Scan(paths, techdebt.DefaultThresholds) {
		for _, seg := range strings.Split(filepath.ToSlash(f.File), "/") {
			if seg == "testdata" || seg == "fixtures" {
				t.Errorf("fixture path reached the scan: [%s] %s", f.Class, f.File)
				break
			}
		}
	}
}
