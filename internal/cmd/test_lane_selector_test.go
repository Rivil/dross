package cmd

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// selectorLanes is goAndDocsLanes with selectors declared: the go lane scopes
// to Go packages, the docs lane to directories. Two DIFFERENT styles on
// purpose — a run site that derived one style for every lane would still pass
// a single-lane fixture.
const selectorLanes = `[[runtime.test_lane]]
name = "go"
match = ["internal/**", "main.go"]
command = "go test -count=1"
selector = "go-package"

[[runtime.test_lane]]
name = "docs"
match = ["docs/", "README.md"]
command = "markdownlint"
selector = "dir"`

// touchFile creates a file (and its parents) under the fixture repo, so the
// existence filter locked by missing_paths has something real to look at.
func touchFile(t *testing.T, rel string) {
	t.Helper()
	root, err := FindRoot()
	if err != nil {
		t.Fatal(err)
	}
	full := filepath.Join(filepath.Dir(root), rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestSelectorScopesTheSpawnedLine is c-2 at the run site: a task touching one
// file must cost that file's package, not the lane's whole suite. Asserted on
// the RECORDED LINE, so a lane that resolved a selector and then spawned its
// bare command fails here.
func TestSelectorScopesTheSpawnedLine(t *testing.T) {
	filesFixture(t, selectorLanes)
	grantAllLanes(t)
	touchFile(t, "internal/cmd/test.go")
	rec := installSpawnRecorder(t, nil)

	if err := runCmd(t, Test(), "--files", "internal/cmd/test.go"); err != nil {
		t.Fatalf("dross test --files: %v", err)
	}
	if rec.count() != 1 {
		t.Fatalf("want exactly 1 spawn, got %d: %v", rec.count(), rec.lines)
	}
	want := "go test -count=1 './internal/cmd/...'"
	if rec.lines[0] != want {
		t.Errorf("spawned %q, want %q — an unscoped line is the whole-suite run this replaces", rec.lines[0], want)
	}
}

// TestTheHeaderIsTheSpawnedLine compares the two strings against EACH OTHER
// rather than each against a literal. A header built from lane.Command while a
// derived line spawned would satisfy two separate literal assertions and still
// be a transcript that lies about what was measured.
func TestTheHeaderIsTheSpawnedLine(t *testing.T) {
	filesFixture(t, selectorLanes)
	grantAllLanes(t)
	touchFile(t, "internal/cmd/test.go")
	rec := installSpawnRecorder(t, nil)

	var out string
	if err := runCmdCapturing(t, &out, Test(), "--files", "internal/cmd/test.go"); err != nil {
		t.Fatal(err)
	}
	if rec.count() != 1 {
		t.Fatalf("want exactly 1 spawn, got %d: %v", rec.count(), rec.lines)
	}
	header := "lane go: " + rec.lines[0]
	if !strings.Contains(out, header) {
		t.Errorf("the header does not carry the line that ran (%q):\n%s", rec.lines[0], out)
	}
}

// TestSelectorHeaderStillPrecedesLaneOutput: the derived line must not cost the
// existing header-first property, which is what lets a transcript attribute a
// failure to a runner.
func TestSelectorHeaderStillPrecedesLaneOutput(t *testing.T) {
	filesFixture(t, selectorLanes)
	grantAllLanes(t)
	touchFile(t, "internal/cmd/test.go")

	orig := spawnLocal
	t.Cleanup(func() { spawnLocal = orig })
	var spawned string
	spawnLocal = func(_, line string, stdout, _ io.Writer) error {
		spawned = line
		_, _ = io.WriteString(stdout, "LANE-OUTPUT-SENTINEL\n")
		return nil
	}

	var out string
	if err := runCmdCapturing(t, &out, Test(), "--files", "internal/cmd/test.go"); err != nil {
		t.Fatal(err)
	}
	header := strings.Index(out, "lane go: "+spawned)
	sentinel := strings.Index(out, "LANE-OUTPUT-SENTINEL")
	if header < 0 {
		t.Fatalf("no derived-line header in the transcript:\n%s", out)
	}
	if header >= sentinel {
		t.Errorf("the header trails the lane's output:\n%s", out)
	}
}

// TestASelectorlessLaneIsStillByteIdentical is c-3 with a selector-declaring
// sibling present in the SAME run, which is the shape a per-run (rather than
// per-lane) implementation gets wrong: opting one lane in must not append
// anything to the lane that opted out.
func TestASelectorlessLaneIsStillByteIdentical(t *testing.T) {
	filesFixture(t, `[[runtime.test_lane]]
name = "go"
match = ["internal/**", "main.go"]
command = "go test -count=1"
selector = "go-package"

[[runtime.test_lane]]
name = "docs"
match = ["docs/", "README.md"]
command = "markdownlint docs"`)
	grantAllLanes(t)
	touchFile(t, "internal/cmd/test.go")
	touchFile(t, "docs/x.md")
	rec := installSpawnRecorder(t, nil)

	if err := runCmd(t, Test(), "--files", "internal/cmd/test.go", "--files", "docs/x.md"); err != nil {
		t.Fatal(err)
	}
	if rec.count() != 2 {
		t.Fatalf("want 2 spawns, got %d: %v", rec.count(), rec.lines)
	}
	var docs string
	for _, line := range rec.lines {
		if strings.HasPrefix(line, "markdownlint") {
			docs = line
		}
	}
	if docs != "markdownlint docs" {
		t.Errorf("the selectorless lane spawned %q, want its command byte-for-byte", docs)
	}
}

// TestASelectorCarriesOnlyItsOwnLanesPaths is c-4 asserted on ARGV, which is
// where it matters: an implementation that handed every in-tree path to every
// lane produces the right Lanes and the right spawn count, and only shows up as
// a go lane being told to test ./docs/... .
func TestASelectorCarriesOnlyItsOwnLanesPaths(t *testing.T) {
	filesFixture(t, selectorLanes)
	grantAllLanes(t)
	touchFile(t, "internal/cmd/test.go")
	touchFile(t, "docs/x.md")
	rec := installSpawnRecorder(t, nil)

	// README.md matches the docs lane's globs, so this file set is one path
	// per lane plus one shared between them; nothing here is unmatched.
	touchFile(t, "README.md")
	if err := runCmd(t, Test(), "--files", "internal/cmd/test.go", "--files", "docs/x.md", "--files", "README.md"); err != nil {
		t.Fatal(err)
	}
	var goLine, docsLine string
	for _, line := range rec.lines {
		switch {
		case strings.HasPrefix(line, "go test"):
			goLine = line
		case strings.HasPrefix(line, "markdownlint"):
			docsLine = line
		}
	}
	if !strings.Contains(goLine, "./internal/cmd/...") {
		t.Errorf("the go lane did not scope to its own package: %q", goLine)
	}
	if strings.Contains(goLine, "docs") || strings.Contains(goLine, "README") {
		t.Errorf("the go lane's selector carries another lane's paths: %q", goLine)
	}
	if strings.Contains(docsLine, "internal") {
		t.Errorf("the docs lane's selector carries the go lane's paths: %q", docsLine)
	}
}

// TestAnExistingGrantSurvivesTheAppendedSelector is c-6 and locked
// selector_consent in one run: the lane is granted for its DECLARED command,
// the spawned line differs from it, and the run is green. Re-fingerprinting the
// derived line fails on the refusal; dropping the selector to keep the grant
// valid fails on the difference.
func TestAnExistingGrantSurvivesTheAppendedSelector(t *testing.T) {
	filesFixture(t, selectorLanes)
	// Granted against the declared command only — which is also exactly the
	// grant a repo upgrading into this feature already has on disk.
	grantLane(t, "go")
	touchFile(t, "internal/cmd/test.go")
	rec := installSpawnRecorder(t, nil)

	if err := runCmd(t, Test(), "--files", "internal/cmd/test.go"); err != nil {
		t.Fatalf("a grant issued against the declared command went stale when a selector was appended: %v", err)
	}
	if rec.count() != 1 {
		t.Fatalf("want 1 spawn, got %d: %v", rec.count(), rec.lines)
	}
	if rec.lines[0] == "go test -count=1" {
		t.Error("the selector was dropped to keep the grant valid — the run was unscoped")
	}
	if !strings.HasPrefix(rec.lines[0], "go test -count=1 ") {
		t.Errorf("spawned %q, want the granted command with a selector appended", rec.lines[0])
	}
}

// TestAnUngrantedSelectorLaneStillRefuses: the selector changes what runs, not
// whether it may. The refusal names lane.Command — the line the user is being
// asked to trust — and not the derived one, which would send them to `dross
// trust` for a string they never wrote.
func TestAnUngrantedSelectorLaneStillRefuses(t *testing.T) {
	filesFixture(t, selectorLanes)
	touchFile(t, "internal/cmd/test.go")
	rec := installSpawnRecorder(t, nil)

	var out string
	err := runCmdCapturing(t, &out, Test(), "--files", "internal/cmd/test.go")
	if err == nil {
		t.Fatal("an ungranted lane ran")
	}
	if got := ExitCode(err); got != exitLaneRefused {
		t.Errorf("exit = %d, want %d (lane refused)", got, exitLaneRefused)
	}
	if rec.count() != 0 {
		t.Fatalf("an ungranted lane spawned %v", rec.lines)
	}
	if !strings.Contains(out, "go test -count=1") {
		t.Errorf("the refusal does not name the declared command:\n%s", out)
	}
	if strings.Contains(out, "./internal/cmd/...") {
		t.Errorf("the refusal names the derived line, which is not what consent binds to:\n%s", out)
	}
}

// TestASelectorPathWithASpaceIsQuoted: the line goes to `sh -c`, so a derived
// argument has to survive the shell. Unquoted, a file in a directory with a
// space would reach the runner as two arguments.
func TestASelectorPathWithASpaceIsQuoted(t *testing.T) {
	filesFixture(t, `[[runtime.test_lane]]
name = "docs"
match = ["docs/**"]
command = "markdownlint"
selector = "path"`)
	grantAllLanes(t)
	touchFile(t, "docs/my notes.md")
	rec := installSpawnRecorder(t, nil)

	if err := runCmd(t, Test(), "--files", "docs/my notes.md"); err != nil {
		t.Fatal(err)
	}
	if rec.count() != 1 {
		t.Fatalf("want 1 spawn, got %d: %v", rec.count(), rec.lines)
	}
	if !strings.Contains(rec.lines[0], "'docs/my notes.md'") {
		t.Errorf("spawned %q, want the path single-quoted for sh -c", rec.lines[0])
	}
}

// TestDeletedPathsAreDroppedFromTheSelector is locked missing_paths: `go test
// ./gone/...` is a hard runner error, so a task that deleted a file would read
// as a failing gate for work it did on purpose.
func TestDeletedPathsAreDroppedFromTheSelector(t *testing.T) {
	filesFixture(t, selectorLanes)
	grantAllLanes(t)
	touchFile(t, "internal/cmd/test.go")
	rec := installSpawnRecorder(t, nil)

	// internal/gone/x.go is never created: it is the path a task deleted.
	if err := runCmd(t, Test(), "--files", "internal/cmd/test.go", "--files", "internal/gone/x.go"); err != nil {
		t.Fatal(err)
	}
	if rec.count() != 1 {
		t.Fatalf("want 1 spawn, got %d: %v", rec.count(), rec.lines)
	}
	if strings.Contains(rec.lines[0], "gone") {
		t.Errorf("the spawned line carries a deleted path: %q", rec.lines[0])
	}
	if !strings.Contains(rec.lines[0], "./internal/cmd/...") {
		t.Errorf("the surviving path lost its package: %q", rec.lines[0])
	}
}

// TestALaneWhoseEveryPathIsGoneDoesNotSpawnAndIsNotGreen holds the interim this
// commit creates. The existence filter lands here and the miss verdict lands in
// t-6, and between the two a run that spawned nothing must NOT report success —
// that gap is exactly the false green the whole file set gate exists to prevent.
func TestALaneWhoseEveryPathIsGoneDoesNotSpawnAndIsNotGreen(t *testing.T) {
	filesFixture(t, selectorLanes)
	grantAllLanes(t)
	rec := installSpawnRecorder(t, nil)

	err := runCmd(t, Test(), "--files", "internal/gone/x.go")
	if err == nil {
		t.Fatal("a run that spawned nothing reported success")
	}
	if rec.count() != 0 {
		t.Fatalf("a lane with no surviving path spawned %v", rec.lines)
	}
}

// TestTheExistenceFilterOnlyTouchesSelectorLanes: every pre-selector fixture
// names paths that need not exist — the lane's globs are strings and nothing
// ever stat'd them. Filtering a selectorless lane would change the behaviour of
// every lane written before this phase.
func TestTheExistenceFilterOnlyTouchesSelectorLanes(t *testing.T) {
	filesFixture(t, goAndDocsLanes)
	grantAllLanes(t)
	rec := installSpawnRecorder(t, nil)

	if err := runCmd(t, Test(), "--files", "internal/gone/x.go"); err != nil {
		t.Fatalf("a selectorless lane refused a path that is not on disk: %v", err)
	}
	if rec.count() != 1 || rec.lines[0] != goCmd {
		t.Fatalf("spawned %v, want the selectorless lane's command", rec.lines)
	}
}

// TestTheRemotePathGetsTheDerivedLineToo: runRemoteLine is a second call site,
// and a local-only wiring passes every test above while running the whole suite
// on the granted host — the same code measuring two different things depending
// on where it ran.
func TestTheRemotePathGetsTheDerivedLineToo(t *testing.T) {
	root := grantedTestFixture(t, "go test ./...")
	appendLanes(t, filepath.Dir(root), selectorLanes)
	grantAllLanes(t)
	touchFile(t, "internal/cmd/test.go")
	rec := installRemoteRecorder(t, nil)

	if err := runCmd(t, Test(), "--files", "internal/cmd/test.go"); err != nil {
		t.Fatalf("remote lane run: %v", err)
	}
	found := false
	for _, script := range rec.scripts {
		if strings.Contains(script, "./internal/cmd/...") {
			found = true
		}
	}
	if !found {
		t.Errorf("no ssh script carries the derived selector: %v", rec.scripts)
	}
}
