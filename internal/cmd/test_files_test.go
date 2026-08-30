package cmd

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/project"
)

// filesFixture is a trusted repo with a go lane and a docs lane declared.
//
// Consent for the whole-suite command IS granted here, because the per-lane
// gate is not this task's subject: what is under test is resolution, and a
// fixture that refused at the whole-suite gate would prove nothing about it.
func filesFixture(t *testing.T, lanes string) {
	t.Helper()
	dir := laneFixture(t)
	mustRunSet(t, "runtime.test_command", "go test ./...")
	if lanes != "" {
		appendLanes(t, dir, lanes)
	}
	if err := GrantConsent(dir+"/"+RootDirName, "go test ./..."); err != nil {
		t.Fatal(err)
	}
	// This machine "has" every lane's toolchain. The fixture already stubs the
	// spawn seam, so resolving binaries for real would make it half a machine —
	// refusing a lane whose command it is simultaneously pretending to run.
	// Tests whose subject IS the local-absence rule install their own.
	installLaneLookPath(t)
}

// grantAllLanes consents to every declared lane's command on this machine.
//
// Called only by tests whose subject is resolution or transport rather than
// consent: the per-lane gate is real behaviour, so an ungranted fixture refuses
// exactly as a fresh clone would, and a test about something else must get past
// it deliberately rather than by accident.
func grantAllLanes(t *testing.T) {
	t.Helper()
	root, err := FindRoot()
	if err != nil {
		t.Fatal(err)
	}
	p, err := project.Load(filepath.Join(root, project.File))
	if err != nil {
		t.Fatal(err)
	}
	for _, lane := range p.Runtime.TestLane {
		if err := GrantLaneConsent(root, lane.Name, lane.Command); err != nil {
			t.Fatalf("grant lane %s: %v", lane.Name, err)
		}
	}
}

const goAndDocsLanes = `[[runtime.test_lane]]
name = "go"
match = ["internal/**", "main.go"]
command = "go test -count=1 ./..."

[[runtime.test_lane]]
name = "docs"
match = ["docs/", "README.md"]
command = "markdownlint docs"`

// TestNoLanesIsByteIdentical is c-5: a repo that never declared a lane behaves
// exactly as it did before lanes existed, with or without --files. Byte
// identity is the assertion because the consented fingerprint covers that exact
// string — a run that appended so much as a selector would be approving one
// command and running another.
func TestNoLanesIsByteIdentical(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
	}{
		{"bare", nil},
		{"with --files", []string{"--files", "internal/a.go"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			filesFixture(t, "")
			rec := installSpawnRecorder(t, nil)

			if err := runCmd(t, Test(), tc.args...); err != nil {
				t.Fatalf("a lane-less repo must run the suite: %v", err)
			}
			if rec.count() != 1 {
				t.Fatalf("want exactly 1 spawn, got %d: %v", rec.count(), rec.lines)
			}
			if rec.lines[0] != "go test ./..." {
				t.Errorf("spawned %q, want the consented line byte-for-byte", rec.lines[0])
			}
		})
	}
}

// TestUnmatchedFileSetRefusesWithoutRunning is c-8. A file set that matches
// nothing is not a small run — it is no run, and reporting it green to a caller
// deciding whether to commit is worse than any red result.
func TestUnmatchedFileSetRefusesWithoutRunning(t *testing.T) {
	filesFixture(t, `[[runtime.test_lane]]
name = "go"
match = ["internal/**"]
command = "go test -count=1 ./..."`)
	rec := installSpawnRecorder(t, nil)

	err := runCmd(t, Test(), "--files", "docs/x.md")
	if err == nil {
		t.Fatal("a file set matching no lane reported success")
	}
	if !strings.Contains(err.Error(), "docs/x.md") {
		t.Errorf("refusal does not name the unmatched path: %v", err)
	}
	if got := ExitCode(err); got != exitNothingMeasured {
		t.Errorf("exit = %d, want %d (nothing measured)", got, exitNothingMeasured)
	}
	if n := rec.count(); n != 0 {
		t.Errorf("the refusal spawned %d command(s)", n)
	}
}

// TestUnmatchedRefusalNamesTheDeclaredLanes: "nothing matched" without saying
// what could have matched sends the user to open project.toml. The lanes they
// do have are the answer to the question the refusal raises.
func TestUnmatchedRefusalNamesTheDeclaredLanes(t *testing.T) {
	filesFixture(t, goAndDocsLanes)

	err := runCmd(t, Test(), "--files", "scripts/x.sh")
	if err == nil {
		t.Fatal("an unmatched file set reported success")
	}
	for _, want := range []string{"go", "docs"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal does not list lane %q: %v", want, err)
		}
	}
}

// TestExitCodesArePairwiseDistinct: the codes are a contract read by a caller
// deciding whether to commit. Two of them colliding is how "nothing ran" gets
// read as "your code is broken" — or worse, the other way round.
func TestExitCodesArePairwiseDistinct(t *testing.T) {
	codes := map[string]int{
		"suite failed":      exitSuiteFailed,
		"bad file set":      exitBadFileSet,
		"transport":         exitTransport,
		"partial":           exitPartial,
		"nothing measured":  exitNothingMeasured,
		"lane refused":      exitLaneRefused,
		"prepare failed":    exitPrepareFailed,
		"toolchain missing": exitToolchainMissing,
	}
	seen := map[int]string{}
	for name, code := range codes {
		if code == 0 {
			t.Errorf("%s = 0, which means success", name)
		}
		if other, dup := seen[code]; dup {
			t.Errorf("%s and %s share exit code %d", name, other, code)
		}
		seen[code] = name
	}
	// The map above is hand-maintained and the compiler has no opinion about
	// what is missing from it, so a code left out collides in silence — which
	// is exactly what this test exists to catch. Counting the const block
	// closes that: the next exit code added to test.go fails HERE, at the
	// moment it is declared, rather than rotting until two codes overlap.
	if declared := declaredExitCodes(t); len(codes) != declared {
		t.Errorf("the distinctness map holds %d codes and test.go declares %d — add the missing one to the map above", len(codes), declared)
	}
}

// declaredExitCodes counts the exit* constants declared in test.go by reading
// the source, not by asking the package: untyped int constants leave nothing
// behind at run time to enumerate.
func declaredExitCodes(t *testing.T) int {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), "test.go", nil, 0)
	if err != nil {
		t.Fatalf("parse test.go: %v", err)
	}
	n := 0
	for _, decl := range f.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			for _, name := range spec.(*ast.ValueSpec).Names {
				if strings.HasPrefix(name.Name, "exit") {
					n++
				}
			}
		}
	}
	if n == 0 {
		t.Fatal("found no exit* constants in test.go — the parse walked the wrong file")
	}
	return n
}

// TestAbsolutePathRefusalSaysOutOfTree: the two refusals must not collapse. An
// absolute path reported as "matched no lane" sends the user to widen a glob
// over a mistake in their own command line — and widening a lane's globs to
// admit /etc is not a fix, it is damage.
func TestAbsolutePathRefusalSaysOutOfTree(t *testing.T) {
	filesFixture(t, goAndDocsLanes)
	rec := installSpawnRecorder(t, nil)

	err := runCmd(t, Test(), "--files", "/abs/path/internal/a.go")
	if err == nil {
		t.Fatal("an absolute path was accepted")
	}
	if !strings.Contains(err.Error(), "OUTSIDE THIS REPOSITORY") {
		t.Errorf("refusal does not say the path is out of tree: %v", err)
	}
	if !strings.Contains(err.Error(), "/abs/path/internal/a.go") {
		t.Errorf("refusal does not name the path: %v", err)
	}
	if got := ExitCode(err); got != exitBadFileSet {
		t.Errorf("exit = %d, want %d (bad file set), not %d (nothing measured)", got, exitBadFileSet, exitNothingMeasured)
	}
	if n := rec.count(); n != 0 {
		t.Errorf("the refusal spawned %d command(s)", n)
	}
}

// TestOutOfTreePoisonsTheSet: one bad path refuses the whole run. Resolving the
// in-tree half would report on a subset the caller never asked for, and they
// would read the green as covering everything they listed.
func TestOutOfTreePoisonsTheSet(t *testing.T) {
	filesFixture(t, goAndDocsLanes)
	var out string

	err := runCmdCapturing(t, &out, Test(), "--files", "internal/a.go", "--files", "/abs/x.go")
	if err == nil {
		t.Fatal("a half-broken file set was accepted")
	}
	if got := ExitCode(err); got != exitBadFileSet {
		t.Errorf("exit = %d, want %d", got, exitBadFileSet)
	}
	if strings.Contains(out, "lane ") {
		t.Errorf("the in-tree half was resolved and run anyway:\n%s", out)
	}
}

// TestFilesAndSelectorIsAnError: a selector is appended to the command line,
// and a lane's command has to run byte-identically to the fingerprinted line.
// Silently dropping the positionals would run something narrower than asked
// while reporting success.
func TestFilesAndSelectorIsAnError(t *testing.T) {
	filesFixture(t, goAndDocsLanes)
	rec := installSpawnRecorder(t, nil)

	err := runCmd(t, Test(), "--files", "internal/a.go", "./internal/...")
	if err == nil {
		t.Fatal("--files combined with a selector was accepted")
	}
	for _, want := range []string{"--files", "selector", "./internal/..."} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal does not mention %q: %v", want, err)
		}
	}
	if n := rec.count(); n != 0 {
		t.Errorf("the refusal spawned %d command(s)", n)
	}
}

// TestFilesAndSelectorRefusesBeforeAnyIO: the combination is rejected before
// the consent gate and before the project is loaded, so the message is about
// the argv rather than about whatever the repo's state happens to be.
func TestFilesAndSelectorRefusesBeforeAnyIO(t *testing.T) {
	// No consent granted: an untrusted repo would normally refuse first.
	dir := laneFixture(t)
	mustRunSet(t, "runtime.test_command", "go test ./...")
	appendLanes(t, dir, goAndDocsLanes)

	err := runCmd(t, Test(), "--files", "internal/a.go", "./internal/...")
	if err == nil {
		t.Fatal("the combination was accepted")
	}
	if !strings.Contains(err.Error(), "--files") {
		t.Errorf("the argv refusal was preempted by another gate: %v", err)
	}
}

// TestPartialMissNamesTheRest is the locked unmatched_files decision: a task
// touching Go code and a README runs the Go lane and says the README matched
// nothing. Dragging in the full suite would defeat the point; dropping the
// README silently would cover less than the caller listed without saying so.
func TestPartialMissNamesTheRest(t *testing.T) {
	filesFixture(t, `[[runtime.test_lane]]
name = "go"
match = ["internal/**"]
command = "go test -count=1 ./..."`)
	grantAllLanes(t)
	installSpawnRecorder(t, nil)

	var out string
	err := runCmdCapturing(t, &out, Test(), "--files", "internal/a.go", "--files", "docs/x.md")
	if err != nil {
		t.Fatalf("a partial miss must not refuse the run: %v", err)
	}
	if !strings.Contains(out, "docs/x.md") {
		t.Errorf("the unmatched path was dropped silently:\n%s", out)
	}
	if !strings.Contains(out, "go") {
		t.Errorf("the matched lane was not resolved:\n%s", out)
	}
}

// TestFilesIsRepeatableNotCommaSplit: a comma is legal in a path, so splitting
// on one would turn a single real file into two paths that exist nowhere — and
// the failure would surface as an unmatched-file refusal pointing at names the
// user never typed.
func TestFilesIsRepeatableNotCommaSplit(t *testing.T) {
	filesFixture(t, `[[runtime.test_lane]]
name = "odd"
match = ["a,b.go"]
command = "true"`)
	grantAllLanes(t)
	installSpawnRecorder(t, nil)

	var out string
	if err := runCmdCapturing(t, &out, Test(), "--files", "a,b.go"); err != nil {
		t.Fatalf("a path containing a comma was split: %v", err)
	}
	if !strings.Contains(out, "odd") {
		t.Errorf("the lane was not resolved:\n%s", out)
	}
}

// TestNothingMeasuredIsTaggedForTheExitMapper: the code only reaches the
// process through ExitCodeError, so a refusal returned as a plain error would
// exit 1 and read to a caller as a red suite.
func TestNothingMeasuredIsTaggedForTheExitMapper(t *testing.T) {
	filesFixture(t, goAndDocsLanes)

	err := runCmd(t, Test(), "--files", "scripts/x.sh")
	var ec *ExitCodeError
	if !errors.As(err, &ec) {
		t.Fatalf("the refusal is not tagged with an exit code: %v", err)
	}
	if ec.Code != exitNothingMeasured {
		t.Errorf("Code = %d, want %d", ec.Code, exitNothingMeasured)
	}
}

// --- selector templates at the run site (c-1, c-2) ---

// templateLanes declares both placeholder shapes at once. One lane repeats its
// template per path, the other joins them into a single argument — a run site
// that expanded one shape for every lane would still pass a single-lane
// fixture.
const templateLanes = `[[runtime.test_lane]]
name = "rust"
match = ["crates/**"]
command = "cargo test"
selector = "dir"
selector_template = "--package {path}"

[[runtime.test_lane]]
name = "ctest"
match = ["tests/**"]
command = "ctest"
selector = "path"
selector_template = "-R {paths}"
selector_join = "|"`

// TestTemplatedLaneSpawnsTheExpandedLine is c-1 at the run site: the whole
// point of a template is a scoped line for a runner the closed selector enum
// cannot shape. Asserted on the RECORDED line, so a lane that expanded a
// template and then spawned its bare command fails here.
func TestTemplatedLaneSpawnsTheExpandedLine(t *testing.T) {
	filesFixture(t, templateLanes)
	grantLane(t, "rust")
	grantLane(t, "ctest")
	touchFile(t, "crates/a/src/lib.rs")
	touchFile(t, "crates/b/src/lib.rs")
	rec := installSpawnRecorder(t, nil)

	if err := runCmd(t, Test(), "--files", "crates/a/src/lib.rs", "--files", "crates/b/src/lib.rs"); err != nil {
		t.Fatalf("dross test --files: %v", err)
	}
	if rec.count() != 1 {
		t.Fatalf("want exactly 1 spawn, got %d: %v", rec.count(), rec.lines)
	}
	want := "cargo test --package 'crates/a/src' --package 'crates/b/src'"
	if rec.lines[0] != want {
		t.Errorf("spawned %q, want %q", rec.lines[0], want)
	}
}

// TestTemplatedLaneJoinsPathsIntoOneArgument is the other placeholder shape,
// and the one that cannot be reached by appending: `-R` takes a single regex,
// so a joined argument is the only line ctest can be scoped with.
func TestTemplatedLaneJoinsPathsIntoOneArgument(t *testing.T) {
	filesFixture(t, templateLanes)
	grantLane(t, "rust")
	grantLane(t, "ctest")
	touchFile(t, "tests/a.cc")
	touchFile(t, "tests/b.cc")
	rec := installSpawnRecorder(t, nil)

	if err := runCmd(t, Test(), "--files", "tests/a.cc", "--files", "tests/b.cc"); err != nil {
		t.Fatalf("dross test --files: %v", err)
	}
	if rec.count() != 1 {
		t.Fatalf("want exactly 1 spawn, got %d: %v", rec.count(), rec.lines)
	}
	want := "ctest -R 'tests/a.cc|tests/b.cc'"
	if rec.lines[0] != want {
		t.Errorf("spawned %q, want %q", rec.lines[0], want)
	}
}

// TestTemplatedLaneHeaderIsTheSpawnedLine compares the two strings against EACH
// OTHER rather than each against a literal. A header built from lane.Command
// while an expanded line spawned would satisfy two separate literal assertions
// and still be a transcript that lies about what was measured.
func TestTemplatedLaneHeaderIsTheSpawnedLine(t *testing.T) {
	filesFixture(t, templateLanes)
	grantLane(t, "rust")
	grantLane(t, "ctest")
	touchFile(t, "crates/a/src/lib.rs")
	rec := installSpawnRecorder(t, nil)

	var out string
	if err := runCmdCapturing(t, &out, Test(), "--files", "crates/a/src/lib.rs"); err != nil {
		t.Fatal(err)
	}
	if rec.count() != 1 {
		t.Fatalf("want exactly 1 spawn, got %d: %v", rec.count(), rec.lines)
	}
	if !strings.Contains(out, "lane rust: "+rec.lines[0]+"\n") {
		t.Errorf("the header does not carry the spawned line %q:\n%s", rec.lines[0], out)
	}
}

// TestMalformedTemplateRefusesBeforeAnyLaneSpawns: a placeholder-less template
// is discovered by the up-front fence, with NOTHING spawned. Found inside the
// run loop instead, the go lane ahead of it would already have run — and worse,
// honouring it there would spawn the lane's whole command under a scoped lane's
// name, which is the silent whole-suite run the feature exists to replace.
//
// The malformed lane is deliberately SECOND: a fence checked in order would
// pass a fixture where the bad lane came first regardless of where it ran.
func TestMalformedTemplateRefusesBeforeAnyLaneSpawns(t *testing.T) {
	filesFixture(t, `[[runtime.test_lane]]
name = "go"
match = ["internal/**"]
command = "go test -count=1"
selector = "go-package"

[[runtime.test_lane]]
name = "rust"
match = ["crates/**"]
command = "cargo test"
selector = "dir"
selector_template = "--package"`)
	grantLane(t, "go")
	grantLane(t, "rust")
	touchFile(t, "internal/cmd/test.go")
	touchFile(t, "crates/a/src/lib.rs")
	rec := installSpawnRecorder(t, nil)

	err := runCmd(t, Test(), "--files", "internal/cmd/test.go", "--files", "crates/a/src/lib.rs")
	if err == nil {
		t.Fatal("a placeholder-less template was accepted")
	}
	if rec.count() != 0 {
		t.Errorf("the fence let %d lane(s) spawn first: %v", rec.count(), rec.lines)
	}
	if !strings.Contains(err.Error(), "rust") {
		t.Errorf("the refusal must name the offending lane, got: %v", err)
	}
}

// TestTemplatedPathCarryingAMetacharacterIsQuoted is c-2's injection half at
// the run site rather than in the expander: the line reaches `sh -c`, so a
// matched path spelled with a semicolon must arrive as one argument and can
// never become a second command.
func TestTemplatedPathCarryingAMetacharacterIsQuoted(t *testing.T) {
	filesFixture(t, `[[runtime.test_lane]]
name = "rust"
match = ["crates/**"]
command = "cargo test"
selector = "path"
selector_template = "--only {path}"`)
	grantLane(t, "rust")
	touchFile(t, "crates/a; touch pwned")
	rec := installSpawnRecorder(t, nil)

	if err := runCmd(t, Test(), "--files", "crates/a; touch pwned"); err != nil {
		t.Fatalf("dross test --files: %v", err)
	}
	if rec.count() != 1 {
		t.Fatalf("want exactly 1 spawn, got %d: %v", rec.count(), rec.lines)
	}
	want := "cargo test --only 'crates/a; touch pwned'"
	if rec.lines[0] != want {
		t.Errorf("spawned %q, want %q — an unquoted path is a second command", rec.lines[0], want)
	}
}

// TestLaneWithNoTemplateSpawnsItsCommandByteForByte is the opt-in half at the
// run site. The templated branch must be unreachable for a lane that declares
// nothing, or every lane written before this phase would have its consented
// line rewritten under it.
func TestLaneWithNoTemplateSpawnsItsCommandByteForByte(t *testing.T) {
	filesFixture(t, goAndDocsLanes)
	grantAllLanes(t)
	rec := installSpawnRecorder(t, nil)

	if err := runCmd(t, Test(), "--files", "internal/cmd/test.go"); err != nil {
		t.Fatalf("dross test --files: %v", err)
	}
	if rec.count() != 1 {
		t.Fatalf("want exactly 1 spawn, got %d: %v", rec.count(), rec.lines)
	}
	if rec.lines[0] != "go test -count=1 ./..." {
		t.Errorf("spawned %q, want the lane's command byte-for-byte", rec.lines[0])
	}
}
