package testlane

import (
	"reflect"
	"strings"
	"testing"
)

// TestDeriveIsIndependentOfArgvOrder pins the locked selector_derivation
// decision. The derived line lands in the transcript beside the consented
// command, so it has to depend on the file set and nothing else — argv order
// leaking through would make the same task produce two different lines on two
// runs. The same call also asserts the collapse: three files, two directories.
func TestDeriveIsIndependentOfArgvOrder(t *testing.T) {
	forward, err := Derive("dir", []string{"b/x.go", "a/y.go", "a/z.go"})
	if err != nil {
		t.Fatal(err)
	}
	reversed, err := Derive("dir", []string{"a/z.go", "a/y.go", "b/x.go"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(forward, reversed) {
		t.Errorf("argv order changed the selector: %v vs %v", forward, reversed)
	}
	if !reflect.DeepEqual(forward, []string{"a", "b"}) {
		t.Errorf("Derive(dir) = %v, want [a b] — repeated directories collapse to one argument", forward)
	}
}

// TestGoPackageCollapsesAPackage: the shape the go lane exists for. Length is
// asserted, not just membership, because an implementation emitting one
// argument per file still contains the right pattern.
func TestGoPackageCollapsesAPackage(t *testing.T) {
	got, err := Derive("go-package", []string{"internal/cmd/test.go", "internal/cmd/validate.go", "internal/cmd/ship.go"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{"./internal/cmd/..."}) {
		t.Errorf("Derive(go-package) = %v, want exactly [./internal/cmd/...]", got)
	}
}

// TestGoPackageOnARootFileScopesToTheRootPackage: "." and never "./...".
// Deriving the whole module from one touched file is the unscoped run c-2
// forbids — and the worst kind, since it would report green under a scoped
// lane's name.
func TestGoPackageOnARootFileScopesToTheRootPackage(t *testing.T) {
	got, err := Derive("go-package", []string{"main.go"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{"."}) {
		t.Errorf("Derive(go-package, main.go) = %v, want [.] — [./...] is the whole module", got)
	}
}

// TestDirOnARootFileYieldsDot: path.Dir of a root-level path is ".", and
// dropping it as "not a real directory" would leave a root-only file set
// deriving no selector at all — which reads at the run site as a lane that
// collected nothing.
func TestDirOnARootFileYieldsDot(t *testing.T) {
	got, err := Derive("dir", []string{"main.go"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{"."}) {
		t.Errorf("Derive(dir, main.go) = %v, want [.]", got)
	}
}

// TestPathEmitsEachDistinctFile: the identity shape, and the dedupe underneath
// every other one.
func TestPathEmitsEachDistinctFile(t *testing.T) {
	two, err := Derive("path", []string{"b.go", "a.go"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(two, []string{"a.go", "b.go"}) {
		t.Errorf("Derive(path) = %v, want both files sorted", two)
	}
	one, err := Derive("path", []string{"a.go", "a.go"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(one, []string{"a.go"}) {
		t.Errorf("Derive(path) = %v, want the repeat collapsed", one)
	}
}

// TestADerivedArgumentNeverBeginsWithADash: a file named -x.go would otherwise
// reach the runner as an option dross never chose to pass. ./-x.go names the
// same file and cannot be read as a flag.
func TestADerivedArgumentNeverBeginsWithADash(t *testing.T) {
	got, err := Derive("path", []string{"-x.go"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{"./-x.go"}) {
		t.Errorf("Derive(path, -x.go) = %v, want [./-x.go]", got)
	}
}

// TestEmptyStyleDerivesNothing: the no-selector lane is a value in the same
// function, not a branch each caller has to remember. A caller that forgot it
// would be the one place c-3 breaks.
func TestEmptyStyleDerivesNothing(t *testing.T) {
	got, err := Derive("", []string{"internal/a.go", "main.go"})
	if err != nil {
		t.Fatalf("the no-selector lane must not be an error: %v", err)
	}
	if got != nil {
		t.Errorf("Derive(\"\") = %v, want a nil slice", got)
	}
}

// TestUnknownStyleErrorsAndDerivesNothing asserts BOTH halves. A caller that
// ignored the error would spawn the lane's whole suite — the unscoped run this
// feature exists to avoid, arriving silently under a scoped lane's name.
func TestUnknownStyleErrorsAndDerivesNothing(t *testing.T) {
	got, err := Derive("packages", []string{"internal/a.go"})
	if err == nil {
		t.Fatal("Derive accepted the style \"packages\"")
	}
	if got != nil {
		t.Errorf("a refused style still derived %v — a caller ignoring the error would spawn unscoped", got)
	}
	if !strings.Contains(err.Error(), "packages") {
		t.Errorf("the error must name the style the user wrote, got: %v", err)
	}
	for _, style := range []string{"path", "dir", "go-package"} {
		if !strings.Contains(err.Error(), style) {
			t.Errorf("the error must name the accepted set (missing %q), got: %v", style, err)
		}
	}
}

// TestDeriveNormalizesTheStyle: the same Normalize every other enumerated
// config value goes through, so a style validate blessed can never dispatch
// nowhere here.
func TestDeriveNormalizesTheStyle(t *testing.T) {
	got, err := Derive(" GO-PACKAGE ", []string{"internal/cmd/test.go"})
	if err != nil {
		t.Fatalf("Derive rejected a padded, upper-cased style validate accepts: %v", err)
	}
	if !reflect.DeepEqual(got, []string{"./internal/cmd/..."}) {
		t.Errorf("Derive(\" GO-PACKAGE \") = %v, want the go-package shape", got)
	}
}

// TestDeriveDropsBlankPaths: an empty argument reaching a runner is read as the
// current directory or ignored, and neither is what a matched path meant.
func TestDeriveDropsBlankPaths(t *testing.T) {
	got, err := Derive("path", []string{"", "  ", "a.go"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{"a.go"}) {
		t.Errorf("Derive(path) = %v, want the blanks dropped", got)
	}
}
