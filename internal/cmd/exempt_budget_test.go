package cmd

import (
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// exempt_budget_test.go caps the //dross:exec-exempt directives in the tree
// (cmd-exec-baseline-drain c-4). The phase moves spawn sites between packages;
// a move that "fixed" an audit by adding a marker at the new home would pass
// every per-site test while quietly widening the exemption surface. The phase
// held the ceiling at its starting count (26) while moves spent markers down,
// and locked it at the count the phase ended on.
//
// Counting goes through directiveMarkers — the parser every audit binds
// markers with — so what is counted is exactly what clears a site: a prose
// comment quoting the directive or a string literal mentioning it is not one.

// execExemptCeiling is the most //dross:exec-exempt directives the non-test
// tree may carry. cmd-exec-baseline-drain began at 26 and ended at 9 — gitrun's
// four verbs, codex's ast-grep, ship's gh client, remote's transport seam,
// update's self-exec and compilefence's test-only compile fence — and the ceiling is
// locked there: a new exemption is a deliberate edit to this number, never a
// quiet side effect of a move.
const execExemptCeiling = 9

// countExecExemptDirectives parses every non-test .go file under the roots
// (testdata skipped) and counts the exec-exempt directives that bind a line.
func countExecExemptDirectives(roots ...string) (int, error) {
	fset := token.NewFileSet()
	n := 0
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if d.Name() == "testdata" {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			f, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
			if err != nil {
				return fmt.Errorf("parse %s: %w", path, err)
			}
			n += len(directiveMarkers(fset, f, execExemptMarker))
			return nil
		})
		if err != nil {
			return 0, err
		}
	}
	return n, nil
}

// execExemptBudgetErr is the verdict: over the ceiling is an error naming both
// numbers.
func execExemptBudgetErr(count, ceiling int) error {
	if count > ceiling {
		return fmt.Errorf("the tree carries %d %s directives, over its ceiling: %d > %d — a moved spawn must keep or lose its marker, never gain one",
			count, execExemptMarker, count, ceiling)
	}
	return nil
}

// TestExecExemptBudget is the live gate.
func TestExecExemptBudget(t *testing.T) {
	root := repoRootFromTest(t)
	var roots []string
	for _, r := range auditRoots {
		roots = append(roots, filepath.Join(root, r))
	}
	n, err := countExecExemptDirectives(roots...)
	if err != nil {
		t.Fatal(err)
	}
	if n == 0 {
		t.Fatal("counted no exec-exempt directives — the walk is not pointed at the tree")
	}
	if err := execExemptBudgetErr(n, execExemptCeiling); err != nil {
		t.Error(err)
	}
	t.Logf("%d exec-exempt directives, ceiling %d", n, execExemptCeiling)
}

// TestExecExemptBudgetCountsDirectivesOnly: over a synthetic tree holding two
// real directives, a prose comment quoting the directive, a string literal
// mentioning it, a test file and a testdata file each carrying a real one, the
// count is exactly 2 — and a ceiling of 1 refuses it naming "2 > 1".
func TestExecExemptBudgetCountsDirectivesOnly(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "a", "a.go"), `package a

import "os/exec"

func a() {
	//dross:exec-exempt the first real directive, bound to the line below
	_ = exec.Command("true")
	// //dross:exec-exempt a prose comment quoting the directive is not one
	_ = exec.Command("true")
	_ = "//dross:exec-exempt inside a string literal is not one either"
}
`)
	mustWrite(t, filepath.Join(dir, "b", "b.go"), `package b

import "os/exec"

func b() {
	//dross:exec-exempt the second real directive, bound to the line below
	_ = exec.Command("true")
}
`)
	mustWrite(t, filepath.Join(dir, "b", "b_test.go"), `package b

import "os/exec"

func c() {
	//dross:exec-exempt in a test file, which the budget does not scan
	_ = exec.Command("true")
}
`)
	mustWrite(t, filepath.Join(dir, "b", "testdata", "fixture.go"), `package fixture

import "os/exec"

func d() {
	//dross:exec-exempt in a testdata fixture, which the budget does not scan
	_ = exec.Command("true")
}
`)
	n, err := countExecExemptDirectives(dir)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("counted %d directives, want exactly 2", n)
	}
	err = execExemptBudgetErr(n, 1)
	if err == nil || !strings.Contains(err.Error(), "2 > 1") {
		t.Errorf("a ceiling of 1 over 2 directives gave %v, want an error naming \"2 > 1\"", err)
	}
	if err := execExemptBudgetErr(n, 2); err != nil {
		t.Errorf("a ceiling equal to the count refused it: %v", err)
	}
}
