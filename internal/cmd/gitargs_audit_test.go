package cmd

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A repo-wide gate on the property this phase bought: no git invocation may
// carry a caller-derived positional that git could read as an option.
//
// It exists because the per-site tests cannot see a site nobody wrote a test
// for, and both plan reviews of this phase found call sites the inventory had
// missed. A new `gitCombined(repoDir, "branch", "-D", someVar)` added a year
// from now fails here, by file and line, without anyone having remembered this
// phase happened.
//
// THE RULE: flag a git positional that is neither a string literal nor provably
// prefix-constant.
//
// The prefix-constant carve-out is deliberate and load-bearing.
// `"refs/heads/"+branch` cannot begin with a dash whatever branch holds, so it
// is not an injection vector — and rewriting such sites purely to satisfy a
// stricter rule would be churn that teaches nothing. What the rule does NOT
// permit is a bare variable: `branch` alone is flagged even where the author is
// sure of its provenance, because that certainty is what erodes.
//
// ACCEPTED, WITH REASON (per rule r-02, recorded here rather than in a
// file:line exception list that would rot):
//
//   - statusline.go's `exec.CommandContext(ctx, "git", full...)` passes a spread
//     slice the AST cannot resolve to individual arguments. Its argv is
//     constructed entirely from internal literals a few lines above, with no
//     caller-derived value anywhere in it, so there is nothing for a separator
//     to protect. A spread is invisible to this audit by construction; naming
//     the one occurrence here is more durable than a line number that moves.
//
// The audit scans internal/ and cmd/ — internal/codex/git.go is in scope, which
// TestAuditScansCodexPackage pins, because the first sweep of this phase nearly
// stopped at internal/cmd.

// valueTakingFlags are the git flags in this codebase that consume the NEXT
// argument as their value. Everything else is treated as boolean, so what
// follows it is a positional and must be fenced. Erring toward "boolean" is the
// safe direction: it can only produce a false positive someone has to look at,
// never a missed vector.
var valueTakingFlags = map[string]bool{
	"-m": true, // commit -m <msg>
	"-b": true, // checkout -b <branch>
	"-C": true, // git -C <dir>
	"-F": true, // commit -F <file>
}

// gitCallFuncs are the helpers whose variadic tail IS a git argv.
var gitCallFuncs = map[string]bool{
	"gitCombined": true,
	"gitNoOut":    true,
	"gitTrim":     true,
}

// auditFinding is one flagged positional.
type auditFinding struct {
	Pos  string
	Arg  string
	Call string
}

// auditFile walks one parsed file and returns the positionals it flags.
func auditFile(fset *token.FileSet, f *ast.File) []auditFinding {
	var out []auditFinding

	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		name, args, isGit := gitArgvOf(call)
		if !isGit {
			return true
		}
		// A spread (f(x...)) hides its elements from the AST. Skipped rather
		// than flagged — see the accepted-with-reason note above.
		if call.Ellipsis.IsValid() {
			return true
		}
		sawSeparator := false
		for i, a := range args {
			if lit, ok := stringLit(a); ok {
				if lit == "--" || lit == "--end-of-options" {
					sawSeparator = true
				}
				continue
			}
			if sawSeparator {
				continue // fenced: git cannot read it as an option
			}
			if hasConstPrefix(a) {
				continue // a constant prefix makes a leading dash unreachable
			}
			// The value of a preceding VALUE-TAKING flag (`-m msg`, `-b branch`)
			// is an option ARGUMENT, not a positional: git reads it literally,
			// and a separator in front of it would become the value.
			//
			// An explicit set, not "any preceding dash": `git branch -D <name>`
			// takes a boolean -D followed by a real positional, so treating
			// every flag as value-taking would wave through exactly the shape
			// this audit exists to catch.
			if i > 0 {
				if prev, ok := stringLit(args[i-1]); ok && valueTakingFlags[prev] {
					continue
				}
			}
			out = append(out, auditFinding{
				Pos:  fset.Position(a.Pos()).String(),
				Arg:  exprText(a),
				Call: name,
			})
		}
		return true
	})
	return out
}

// gitArgvOf recognises the two shapes a git argv takes in this codebase and
// returns the argument slice that is the argv.
func gitArgvOf(call *ast.CallExpr) (name string, args []ast.Expr, ok bool) {
	switch fn := call.Fun.(type) {
	case *ast.Ident:
		// gitCombined(repoDir, args...) — first arg is the repo dir.
		if gitCallFuncs[fn.Name] && len(call.Args) > 1 {
			return fn.Name, call.Args[1:], true
		}
	case *ast.SelectorExpr:
		// exec.Command("git", …) / exec.CommandContext(ctx, "git", …)
		pkg, isIdent := fn.X.(*ast.Ident)
		if !isIdent || pkg.Name != "exec" {
			return "", nil, false
		}
		if fn.Sel.Name != "Command" && fn.Sel.Name != "CommandContext" {
			return "", nil, false
		}
		rest := call.Args
		if fn.Sel.Name == "CommandContext" && len(rest) > 0 {
			rest = rest[1:]
		}
		if len(rest) == 0 {
			return "", nil, false
		}
		if lit, isLit := stringLit(rest[0]); !isLit || lit != "git" {
			return "", nil, false
		}
		return "exec." + fn.Sel.Name, rest[1:], true
	}
	return "", nil, false
}

// stringLit unwraps a plain string literal.
func stringLit(e ast.Expr) (string, bool) {
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	s := lit.Value
	if len(s) >= 2 && (s[0] == '"' || s[0] == '`') {
		s = s[1 : len(s)-1]
	}
	return s, true
}

// hasConstPrefix reports whether e is a concatenation whose LEFTMOST operand is
// a non-empty string literal — the shape that makes a leading dash unreachable.
//
// Leftmost specifically: `x + ":path"` has a constant SUFFIX and is still an
// injection vector, so only the head of the concatenation counts.
func hasConstPrefix(e ast.Expr) bool {
	bin, ok := e.(*ast.BinaryExpr)
	if !ok || bin.Op != token.ADD {
		return false
	}
	if lit, ok := stringLit(bin.X); ok {
		return lit != ""
	}
	return hasConstPrefix(bin.X)
}

func exprText(e ast.Expr) string {
	switch v := e.(type) {
	case *ast.Ident:
		return v.Name
	case *ast.SelectorExpr:
		return exprText(v.X) + "." + v.Sel.Name
	case *ast.BinaryExpr:
		return exprText(v.X) + " + " + exprText(v.Y)
	case *ast.BasicLit:
		return v.Value
	case *ast.CallExpr:
		return exprText(v.Fun) + "(…)"
	}
	return "<expr>"
}

// auditRoots are the trees scanned. Both, not just internal/cmd — see
// TestAuditScansCodexPackage.
var auditRoots = []string{"internal", "cmd"}

// TestNoUnseparatedGitPositional is the gate.
func TestNoUnseparatedGitPositional(t *testing.T) {
	root := repoRootForDocs(t)
	fset := token.NewFileSet()
	var findings []auditFinding
	scanned := 0

	for _, r := range auditRoots {
		err := filepath.WalkDir(filepath.Join(root, r), func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			f, perr := parser.ParseFile(fset, path, nil, 0)
			if perr != nil {
				return perr
			}
			scanned++
			findings = append(findings, auditFile(fset, f)...)
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", r, err)
		}
	}

	if scanned == 0 {
		t.Fatal("scanned no files — the audit would pass vacuously")
	}
	for _, f := range findings {
		t.Errorf("%s: %s(…) passes %q as a positional with no separator before it — build the argv with gitRefArgs/gitPathArgs",
			f.Pos, f.Call, f.Arg)
	}
}

// TestAuditFlagsItsOwnSnippets checks the checker. An audit that quietly
// degraded into "return no findings" would pass TestNoUnseparatedGitPositional
// forever and read as coverage. Half of this table must be flagged and half
// must pass.
func TestAuditFlagsItsOwnSnippets(t *testing.T) {
	root := repoRootForDocs(t)
	body, err := os.ReadFile(filepath.Join(root, "internal", "cmd", "testdata", "gitargs_audit", "snippets.txt"))
	if err != nil {
		t.Fatal(err)
	}

	var (
		name    string
		want    bool
		src     []string
		checked int
	)
	flush := func() {
		if name == "" {
			return
		}
		t.Run(name, func(t *testing.T) {
			fset := token.NewFileSet()
			f, perr := parser.ParseFile(fset, name+".go",
				"package cmd\nimport \"os/exec\"\nvar _ = exec.Command\nfunc snippet(repoDir, branch, base, ref, path, msg string) {\n"+
					strings.Join(src, "\n")+"\n}\n", 0)
			if perr != nil {
				t.Fatalf("snippet does not parse: %v\n%s", perr, strings.Join(src, "\n"))
			}
			got := auditFile(fset, f)
			if want && len(got) == 0 {
				t.Errorf("expected a finding, got none:\n%s", strings.Join(src, "\n"))
			}
			if !want && len(got) > 0 {
				t.Errorf("false positive %+v on:\n%s", got, strings.Join(src, "\n"))
			}
		})
		checked++
	}

	for _, line := range strings.Split(string(body), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.HasPrefix(trimmed, "FLAG ") || strings.HasPrefix(trimmed, "PASS ") {
			flush()
			want = strings.HasPrefix(trimmed, "FLAG ")
			name = strings.TrimSpace(trimmed[5:])
			src = nil
			continue
		}
		src = append(src, line)
	}
	flush()

	if checked < 6 {
		t.Fatalf("only %d snippets exercised — the table is too thin to detect a degraded audit", checked)
	}
}

// TestAuditFlagsBarePrefixlessVar pins the carve-out's exact boundary: a
// constant PREFIX passes, a bare variable does not, and a constant SUFFIX does
// not either — `ref + ":path"` still begins with whatever ref holds.
func TestAuditFlagsBarePrefixlessVar(t *testing.T) {
	for _, tc := range []struct {
		name string
		line string
		want bool
	}{
		{"const prefix", `gitNoOut(repoDir, "rev-parse", "refs/heads/"+branch)`, false},
		{"origin prefix", `gitNoOut(repoDir, "rev-parse", "origin/"+base)`, false},
		{"bare var", `gitNoOut(repoDir, "rev-parse", branch)`, true},
		{"const suffix only", `gitNoOut(repoDir, "ls-tree", ref+":.dross")`, true},
		{"empty prefix", `gitNoOut(repoDir, "rev-parse", ""+branch)`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, "x.go",
				"package cmd\nfunc snippet(repoDir, branch, base, ref string) {\n\t"+tc.line+"\n}\n", 0)
			if err != nil {
				t.Fatal(err)
			}
			got := len(auditFile(fset, f)) > 0
			if got != tc.want {
				t.Errorf("flagged = %v, want %v, for: %s", got, tc.want, tc.line)
			}
		})
	}
}

// TestAuditScansCodexPackage: the first sweep of this phase nearly stopped at
// internal/cmd, and internal/codex/git.go shells git too. A narrowed scan root
// is a silent loss of coverage, so it is asserted rather than assumed.
func TestAuditScansCodexPackage(t *testing.T) {
	root := repoRootForDocs(t)
	target := filepath.Join(root, "internal", "codex", "git.go")
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("expected %s to exist: %v", target, err)
	}
	covered := false
	for _, r := range auditRoots {
		if strings.HasPrefix(target, filepath.Join(root, r)+string(filepath.Separator)) {
			covered = true
		}
	}
	if !covered {
		t.Errorf("internal/codex/git.go is outside the audit roots %v", auditRoots)
	}
}
