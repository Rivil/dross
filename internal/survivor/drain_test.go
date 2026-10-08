package survivor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNotCoveredPositionsRefusesMalformedStatuses: a payload whose statuses
// cannot be read is an error naming that step — never an empty set, which
// would silently strip every mutant of its NOT COVERED status and with it the
// attribution ceiling.
func TestNotCoveredPositionsRefusesMalformedStatuses(t *testing.T) {
	for _, payload := range []string{`{not json`, `{"files":[{"file_name":"y.go","mutations":[{"line":"four"}]}]}`} {
		got, err := notCoveredPositions([]byte(payload), "./internal")
		if err == nil || !strings.Contains(err.Error(), "read mutant statuses") {
			t.Errorf("payload %q: got %v, %v; want an error naming \"read mutant statuses\"", payload, got, err)
		}
	}
}

// TestNotCoveredPositionsKeysPerMutantUnderThePackage: only NOT COVERED
// mutants are in the set, keyed by file, line and operator with the package
// prefix applied exactly once.
func TestNotCoveredPositionsKeysPerMutantUnderThePackage(t *testing.T) {
	payload := `{"files":[` +
		`{"file_name":"y.go","mutations":[{"type":"OP","status":"NOT COVERED","line":4},{"type":"OP","status":"LIVED","line":6}]},` +
		`{"file_name":"internal/z.go","mutations":[{"type":"OP2","status":"NOT COVERED","line":9}]}]}`
	got, err := notCoveredPositions([]byte(payload), "./internal")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"internal/y.go:4:OP": true, "internal/z.go:9:OP2": true}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for k := range want {
		if !got[k] {
			t.Errorf("missing %s in %v", k, got)
		}
	}
}

// TestReadRawReportCarriesEachMutantsOwnStatus: two survivors in one file, one
// NOT COVERED and one LIVED, come back with their own statuses — and a
// testdata survivor is returned too, since the scope rule is the caller's.
func TestReadRawReportCarriesEachMutantsOwnStatus(t *testing.T) {
	path := filepath.Join(t.TempDir(), "report.json")
	body := `{"go_module":"example.com/m","files":[` +
		`{"file_name":"y.go","mutations":[` +
		`{"type":"CONDITIONALS_NEGATION","status":"NOT COVERED","line":4},` +
		`{"type":"CONDITIONALS_NEGATION","status":"LIVED","line":6}]},` +
		`{"file_name":"testdata/f.go","mutations":[{"type":"OP","status":"LIVED","line":2}]}]}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := ReadRawReport(path, "./internal")
	if err != nil {
		t.Fatal(err)
	}
	byLine := map[string]RawMutant{}
	for _, m := range got {
		byLine[fmt.Sprintf("%s:%d", m.File, m.Line)] = m
	}
	if m, ok := byLine["internal/y.go:4"]; !ok || !m.NotCovered {
		t.Errorf("the NOT COVERED mutant = %+v, %v; want it present and NotCovered", m, ok)
	}
	if m, ok := byLine["internal/y.go:6"]; !ok || m.NotCovered {
		t.Errorf("the LIVED mutant = %+v, %v; want it present and not NotCovered", m, ok)
	}
	if _, ok := byLine["internal/testdata/f.go:2"]; !ok {
		t.Errorf("the testdata survivor was dropped here; the scope rule is the caller's: %+v", got)
	}
	if _, err := ReadRawReport(filepath.Join(t.TempDir(), "absent.json"), ""); err == nil {
		t.Error("a missing report read as no survivors")
	}
}

// TestCoverageWithoutGoIsUnknown: with no `go` on PATH the coverage pass
// produces no profile, and no profile is unknown — never "not covered". A
// profile a previous run left at the old fixed temp path must not be read as
// this run's.
func TestCoverageWithoutGoIsUnknown(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	stale := "mode: set\nexample.com/m/y.go:4.1,5.1 1 0\n"
	if err := os.WriteFile(filepath.Join(tmp, "dross-drain-cover.out"), []byte(stale), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", "")
	prof := RunCoverageProfile(t.TempDir(), []string{"./..."})
	if prof != nil {
		t.Fatalf("a coverage run with no toolchain produced a profile: %+v", prof)
	}
	if got, _ := prof.CoverageAt("y.go", 4); got != CoverageUnknown {
		t.Errorf("coverage with no profile = %q, want %q", got, CoverageUnknown)
	}
}

// TestRealGoListDirsSurfacesFailures covers the REAL package-discovery closure.
// Every cmd drain test substitutes goListDirs wholesale, so the function that
// actually shells out to the toolchain had no coverage: a broken `go list` would
// have surfaced as an empty package set — a drain that reports nothing
// outstanding because it looked at nothing. Moved here from internal/cmd with
// GoListDirs itself.
func TestRealGoListDirsSurfacesFailures(t *testing.T) {
	t.Run("a directory with no Go module is an error", func(t *testing.T) {
		// Not a module: `go list ./...` exits non-zero.
		_, err := GoListDirs(t.TempDir())
		if err == nil {
			t.Fatal("GoListDirs over a non-module directory returned no error")
		}
		if !strings.Contains(err.Error(), "go list") {
			t.Errorf("err = %q, want the go list context", err)
		}
	})

	t.Run("this repo lists its packages", func(t *testing.T) {
		dirs, err := GoListDirs(repoRootFromHere(t))
		if err != nil {
			t.Fatalf("GoListDirs over the real repo: %v", err)
		}
		if len(dirs) < 20 {
			t.Fatalf("listed %d package dirs, want the whole repo — the blank-line filter or the split is wrong", len(dirs))
		}
		for _, d := range dirs {
			if strings.TrimSpace(d) == "" {
				t.Error("a blank line survived into the package list")
			}
		}
	})
}
