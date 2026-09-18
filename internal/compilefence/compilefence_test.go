package compilefence

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

// The fixtures below import a REAL internal package. That is the whole point:
// the failure mode this package was written against is a temp module that
// cannot import github.com/Rivil/dross/internal/... at all, which makes every
// fixture fail for the wrong reason.

const importsInternal = `package tmpfence

import "github.com/Rivil/dross/internal/pathfence"

var _ = pathfence.InTree
`

const setsUnexportedField = `package tmpfence

import "github.com/Rivil/dross/internal/pathfence"

var _ = pathfence.Contained{rel: "x"}
`

const setsUnknownField = `package tmpfence

import "github.com/Rivil/dross/internal/pathfence"

var _ = pathfence.Contained{nosuchfield: "x"}
`

const emptyLiteral = `package tmpfence

import "github.com/Rivil/dross/internal/pathfence"

var _ = pathfence.Contained{}
`

// TestPositiveControl is the load-bearing test in this file. If it fails, every
// AssertDoesNotCompile in the repo is passing because the module could not be
// built at all, not because the fixture was rejected.
func TestPositiveControl(t *testing.T) {
	if testing.Short() {
		t.Skip("runs go build")
	}
	out, err := build(t, importsInternal)
	if err != nil {
		t.Fatalf("a fixture importing internal/pathfence did not build — the fence is broken.\n%s", out)
	}
}

// TestModulePathMustBeUnderDrossPrefix pins the reason Module is what it is. A
// module outside the prefix cannot import an internal package, and the failure
// is indistinguishable from a fixture being correctly refused.
func TestModulePathMustBeUnderDrossPrefix(t *testing.T) {
	if !strings.HasPrefix(Module, "github.com/Rivil/dross/") {
		t.Fatalf("Module = %q, but it must sit under github.com/Rivil/dross/ or "+
			"no fixture can import an internal package", Module)
	}
}

func TestBuildRejectsUnexportedFieldWithItsOwnWording(t *testing.T) {
	if testing.Short() {
		t.Skip("runs go build")
	}
	out, err := build(t, setsUnexportedField)
	if err == nil {
		t.Fatal("setting an unexported field COMPILED")
	}
	if !strings.Contains(out, "cannot refer to unexported field") {
		t.Errorf("want the unexported-field wording, got:\n%s", out)
	}
}

// TestUnknownFieldIsADifferentMessage is why AssertDoesNotCompile takes a
// wantMsg. A fixture naming a field that does not exist is refused too — but
// with different wording, and it would be refused identically if the field were
// EXPORTED. Asserting on the wrong message proves nothing about unexported-ness.
func TestUnknownFieldIsADifferentMessage(t *testing.T) {
	if testing.Short() {
		t.Skip("runs go build")
	}
	out, err := build(t, setsUnknownField)
	if err == nil {
		t.Fatal("setting an unknown field COMPILED")
	}
	if !strings.Contains(out, "unknown field") {
		t.Errorf("want the unknown-field wording, got:\n%s", out)
	}
	if strings.Contains(out, "cannot refer to unexported field") {
		t.Error("unknown-field and unexported-field wording must stay distinguishable")
	}
}

// TestEmptyLiteralCompiles documents the hole the type system does NOT close:
// Go forbids SETTING an unexported field, not writing an empty composite
// literal. pathfence's I/O seam refuses the zero value for this reason.
func TestEmptyLiteralCompiles(t *testing.T) {
	if testing.Short() {
		t.Skip("runs go build")
	}
	if out, err := build(t, emptyLiteral); err != nil {
		t.Fatalf("pathfence.Contained{} must remain legal outside the package — "+
			"the seam's zero-value refusal is what covers it, not the compiler.\n%s", out)
	}
}

// TestNoProductionCodeImportsCompilefence keeps this package test-only. It is a
// normal (non-_test) package purely so several test packages can share it; if
// production code starts importing it, dross ships a go-build harness.
func TestNoProductionCodeImportsCompilefence(t *testing.T) {
	root := repoRoot(t)
	const self = "github.com/Rivil/dross/internal/compilefence"
	var offenders []string

	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if name := d.Name(); name == ".git" || name == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		f, perr := parser.ParseFile(token.NewFileSet(), p, nil, parser.ImportsOnly)
		if perr != nil {
			return nil // not our business to police unparseable files
		}
		for _, im := range f.Imports {
			if strings.Trim(im.Path.Value, `"`) == self {
				rel, _ := filepath.Rel(root, p)
				offenders = append(offenders, rel)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if len(offenders) > 0 {
		t.Errorf("non-test files import %s: %v", self, offenders)
	}

	// Vacuity guard: a walk that visited nothing would pass silently.
	var seen int
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(p, ".go") && !strings.HasSuffix(p, "_test.go") {
			seen++
		}
		return nil
	})
	if seen < 50 {
		t.Errorf("import walk visited only %d non-test .go files; it is not covering the repo", seen)
	}
	_ = ast.Print // keep go/ast referenced if the walk above is ever simplified
}

// recorder is a testing.TB whose Fatalf records instead of failing the test
// that owns it. It exists so AssertDoesNotCompile / AssertCompiles can be
// driven from INSIDE this package: every other test here calls build()
// directly, so the two assertion bodies never executed under this package's
// own test binary and read as uncovered to a per-package mutation run.
//
// Fatalf still ends the goroutine it is called on (runtime.Goexit), exactly as
// the real one does, so an assertion that fails records ONE message and never
// runs on to a second check over stale state — the recorder mirrors testing.T
// rather than inventing a laxer contract. run() supplies that goroutine.
type recorder struct {
	*testing.T
	fatals []string
}

func (r *recorder) Helper() {}

func (r *recorder) Fatalf(format string, args ...any) {
	r.fatals = append(r.fatals, fmt.Sprintf(format, args...))
	runtime.Goexit()
}

// run drives fn on its own goroutine so a recorded Fatalf can Goexit without
// taking the calling test down with it.
func (r *recorder) run(fn func(tb testing.TB)) {
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		fn(r)
	}()
	wg.Wait()
}

func newRecorder(t *testing.T) *recorder {
	if testing.Short() {
		t.Skip("runs go build")
	}
	return &recorder{T: t}
}

func TestAssertDoesNotCompileRefusesACompilingFixture(t *testing.T) {
	rec := newRecorder(t)
	rec.run(func(tb testing.TB) { AssertDoesNotCompile(tb, importsInternal, "x") })
	if len(rec.fatals) != 1 {
		t.Fatalf("want exactly one Fatalf, got %d: %q", len(rec.fatals), rec.fatals)
	}
	if !strings.Contains(rec.fatals[0], "COMPILED but should not have") {
		t.Errorf("want the compiled-but-should-not wording, got:\n%s", rec.fatals[0])
	}
}

func TestAssertDoesNotCompileAcceptsTheRightRefusal(t *testing.T) {
	rec := newRecorder(t)
	rec.run(func(tb testing.TB) {
		AssertDoesNotCompile(tb, setsUnexportedField, "cannot refer to unexported field")
	})
	if len(rec.fatals) != 0 {
		t.Fatalf("a correctly refused fixture must record no Fatalf, got %q", rec.fatals)
	}
}

func TestAssertDoesNotCompileRefusesTheWrongReason(t *testing.T) {
	rec := newRecorder(t)
	rec.run(func(tb testing.TB) {
		AssertDoesNotCompile(tb, setsUnknownField, "cannot refer to unexported field")
	})
	if len(rec.fatals) != 1 {
		t.Fatalf("want exactly one Fatalf, got %d: %q", len(rec.fatals), rec.fatals)
	}
	if !strings.Contains(rec.fatals[0], "WRONG reason") {
		t.Errorf("want the wrong-reason wording, got:\n%s", rec.fatals[0])
	}
}

func TestAssertCompilesRefusesABrokenFixture(t *testing.T) {
	rec := newRecorder(t)
	rec.run(func(tb testing.TB) { AssertCompiles(tb, setsUnexportedField) })
	if len(rec.fatals) != 1 {
		t.Fatalf("want exactly one Fatalf, got %d: %q", len(rec.fatals), rec.fatals)
	}
	// Both halves of the concatenated message: a mutant that breaks the `+`
	// between them cannot compile, and one that drops a half must read wrong.
	for _, want := range []string{"compile fence itself is broken", "so every AssertDoesNotCompile"} {
		if !strings.Contains(rec.fatals[0], want) {
			t.Errorf("Fatalf lacks %q:\n%s", want, rec.fatals[0])
		}
	}
}

func TestAssertCompilesAcceptsACleanFixture(t *testing.T) {
	rec := newRecorder(t)
	rec.run(func(tb testing.TB) { AssertCompiles(tb, importsInternal) })
	if len(rec.fatals) != 0 {
		t.Fatalf("a clean fixture must record no Fatalf, got %q", rec.fatals)
	}
}
