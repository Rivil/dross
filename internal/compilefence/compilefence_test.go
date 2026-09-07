package compilefence

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
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
