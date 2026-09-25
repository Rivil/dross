package cmd

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/argfence"
)

// A repo-wide gate on the property this milestone bought: no subprocess dross
// spawns may carry a caller-derived positional that the tool could read as an
// option.
//
// It began as a git-only audit. Generalising it to every binary is the locked
// `audit_gate_breadth` decision, and it is fail-closed on purpose: a subprocess
// for a binary nobody anticipated is in scope the day it is added. That is the
// exact failure mode the git-only version was written to prevent and which this
// phase proved real — `gh` was spawning config-derived values the whole time the
// git gate was green.
//
// THE RULE, per binary, read from argfence's policy table rather than restated
// here so the runtime call sites and this gate cannot drift:
//
//   - Separator tools (git, gh, ast-grep, semgrep) may pass a derived positional
//     only BEHIND their end-of-options token. A flag appearing AFTER that token
//     is itself a finding: in cobra `--` ends flag parsing entirely rather than
//     fencing one argument, so a demoted flag becomes a positional — a crash for
//     `gh pr view` (max 1 arg) and silent wrong content for `gh pr comment`.
//   - Reject tools (gremlins, npx, dotnet) have no such token, so ANY
//     non-literal, non-prefix-constant positional is a finding. No separator can
//     rescue them.
//   - A binary with no table entry is a finding. The default is flag, not pass.
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
//   - A SPREAD (`f(x...)`) hides its elements from the AST and is skipped. The
//     occurrences are: statusline.go's `exec.CommandContext(ctx, "git", full...)`,
//     ship_recover.go's and phase.go's `exec.Command("git", full...)`,
//     ship/open.go's `ghCommand(args...)`, codex/ast_grep.go's
//     `exec.Command("ast-grep", argv[1:]...)`, and the three mutation runners'
//     `exec.Command(args[0], args[1:]...)` / `(full[0], full[1:]...)` in
//     gremlins.go, stryker.go and stryker_net.go, plus test.go's
//     `exec.Command("sh", argv...)`. Every one of them builds its argv through a
//     fenced builder a few lines above — gitRefArgs/gitPathArgs, astGrepArgv,
//     shArgv, or an argfence.Fence call — which carries the guarantee this walk
//     would otherwise check. A spread is invisible to an AST audit by
//     construction; naming the shape here is more durable than line numbers that
//     move.
//
//   - A NON-LITERAL BINARY cannot be resolved to a policy and is a finding by
//     default. acceptedNonLiteralBinaries below is the one exception list, keyed
//     by file and expression text rather than by line.
//
// The audit scans internal/ and cmd/. internal/codex/git.go is in scope, which
// TestAuditScansCodexPackage pins, because the first sweep of the git-only
// version nearly stopped at internal/cmd; internal/mutation and internal/ship
// are pinned the same way by TestAuditScansMutationAndShip.

// valueTakingFlags maps a binary to the flags that consume the NEXT argument as
// their value. Everything else is treated as boolean, so what follows it is a
// positional and must be fenced. Erring toward "boolean" is the safe direction:
// it can only produce a false positive someone has to look at, never a missed
// vector.
var valueTakingFlags = map[string]map[string]bool{
	"git": {
		"-m": true, // commit -m <msg>
		"-b": true, // checkout -b <branch>
		"-C": true, // git -C <dir>
		"-F": true, // commit -F <file>
	},
	"gh": {
		"--title": true, "--body": true, "--head": true, "--base": true,
		"--reviewer": true, "--json": true, "--state": true, "--repo": true,
		"--label": true, "--limit": true,
	},
	"ast-grep": {"--lang": true, "--pattern": true},
	"semgrep":  {"--config": true, "--output": true},
	"gremlins": {
		"--output": true, "--timeout-coefficient": true,
		"--workers": true, "--test-cpu": true,
	},
	"npx": {"--mutate": true, "--reporters": true},
	// node's only dross invocation is `node -` (script on stdin); no option
	// dross emits takes a value.
	"node":   {},
	"dotnet": {"--output": true, "--reporter": true},
	// `go list -f <template>`: the template is a constant format string at
	// every call site, but the carve-out has to exist or the value reads as an
	// unfenced positional.
	"go": {"-f": true},
	// ssh and rsync carry the remote mutation run. Only the flags dross itself
	// would ever emit are listed — an over-broad set here silently waves
	// through the operand that follows, which for ssh is the destination host.
	"ssh":   {"-o": true, "-i": true, "-p": true, "-l": true, "-F": true},
	"rsync": {"-e": true, "--filter": true, "--exclude": true, "--rsh": true},
	// sh's -c takes the script as its value. The rest of sh's options are
	// boolean, which is the safe direction to err in: a false positive is
	// something someone looks at, a missed one is a vector.
	"sh": {"-c": true, "-o": true},
}

// separatorTokens are the end-of-options tokens any tool in the table uses.
// They are recognised generically because a mixed git argv legitimately carries
// both (`--end-of-options <ref> -- <path>`), so the "no flag after the
// separator" rule must not mistake the second one for a demoted flag.
var separatorTokens = map[string]bool{"--": true, "--end-of-options": true}

// acceptedNonLiteralBinaries names the spawn sites whose binary is a variable,
// with the reason each is safe. Keyed by "<repo-relative file>:<expr>" so it
// survives the line moving — and never by base name: remote.go, run.go and
// test.go each exist more than once in this tree, and a base-name key would
// accept the same expression in a file the reason was never written about.
// Anything not listed is a finding under the fail-closed rule; a key whose site
// is gone is a finding too (TestAcceptedBinariesAreLive).
var acceptedNonLiteralBinaries = map[string]string{
	"internal/update/apply.go:newBinary": "self-exec of the just-downloaded, signature-verified binary; argv is the single literal \"install\"",
	"internal/testlane/spawn.go:argv[…]": "dross test's remote spawn seam. argv is built by internal/remote's SSHArgs or SyncArgs, which return an error INSTEAD of an argv unless the target passes remote's host/workdir allowlist, so argv[0] is always the literal \"ssh\" or \"rsync\" and every operand is validated before the argv exists. " +
		"Written as Command(argv[0]) + an Args assignment rather than the spread form for the same reason remote.go is: a spread is skipped by this walk, so the spread form would be accommodated by accident.",
	"internal/remote/remote.go:argv[…]": "internal/remote's single exec seam. argv[0] is always the literal \"ssh\" or \"rsync\" chosen by SSHArgs/SyncArgs/FetchArgs, and every operand is validated against remote's host/workdir allowlist before the argv exists. " +
		"It is written as Command(argv[0]) + an Args assignment rather than the usual Command(argv[0], argv[1:]...) spread ON PURPOSE: a spread is skipped by this walk, so the spread form would be accommodated by accident. This form is accommodated on the record.",
}

// acceptedBinaryKey returns the acceptedNonLiteralBinaries key a spawn site in
// filename with binary expression expr matches, or "" when none does. The
// key's file must match filename as a whole repo-relative path suffix, so
// internal/remote/remote.go does not match internal/testlane/remote.go.
func acceptedBinaryKey(filename, expr string) string {
	path := "/" + filepath.ToSlash(filename)
	for key := range acceptedNonLiteralBinaries {
		i := strings.LastIndex(key, ".go:")
		if i < 0 {
			continue
		}
		rel, keyExpr := key[:i+len(".go")], key[i+len(".go:"):]
		if keyExpr == expr && strings.HasSuffix(path, "/"+rel) {
			return key
		}
	}
	return ""
}

// staleAcceptedBinaries returns, sorted, every key of table that no scanned
// site matched.
func staleAcceptedBinaries(table map[string]string, hits map[string]bool) []string {
	var stale []string
	for key := range table {
		if !hits[key] {
			stale = append(stale, key)
		}
	}
	sort.Strings(stale)
	return stale
}

// gitrunVerbs are internal/gitrun's verbs, whose variadic tail IS a git argv.
// Recognised by the gitrun selector anywhere in the tree, which is why an
// aliased import of the package is itself a finding (auditFileHits).
var gitrunVerbs = map[string]bool{"Trim": true, "Raw": true, "Read": true, "Run": true, "Quiet": true}

// gitrunImportPath is the runner's import path.
const gitrunImportPath = "github.com/Rivil/dross/internal/gitrun"

// gitCallFuncs are the helpers whose variadic tail IS a git argv.
var gitCallFuncs = map[string]bool{
	"gitRun":   true,
	"gitNoOut": true,
	"gitTrim":  true,
	"gitRead":  true,
}

// gitHelperSiteFloor is ~25% under each git helper's live call-site count
// (gitRun 39, gitTrim 40, gitRead 11, gitNoOut 41). A helper that fell out of
// gitCallFuncs would have its every argv skipped by the audit; its count going
// to zero is how that shows.
var gitHelperSiteFloor = map[string]int{
	"gitRun":   29,
	"gitTrim":  30,
	"gitRead":  8,
	"gitNoOut": 30,
}

// gitHelperSiteFloorErr checks per-helper call-site counts against the floor.
func gitHelperSiteFloorErr(counts map[string]int) error {
	for name, floor := range gitHelperSiteFloor {
		if counts[name] < floor {
			return fmt.Errorf("the audit examined %d %s call sites, under its floor of %d — %s has left gitCallFuncs or the walk narrowed",
				counts[name], name, floor, name)
		}
	}
	return nil
}

// gitHelperSites counts, per git helper, the call sites the audit examines.
func gitHelperSites(t *testing.T) map[string]int {
	t.Helper()
	root := repoRootForDocs(t)
	fset := token.NewFileSet()
	counts := map[string]int{}
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
			ast.Inspect(f, func(n ast.Node) bool {
				if call, ok := n.(*ast.CallExpr); ok {
					if name, bin, _, _, ok := spawnArgvOf(call); ok && bin == "git" && gitCallFuncs[name] {
						counts[name]++
					}
				}
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", r, err)
		}
	}
	return counts
}

// TestGitHelperCallSiteFloor: every git helper's argv is audited at every one
// of its call sites. Dropping a helper from gitCallFuncs — gitRun above all,
// the helper every effect-only git call goes through — takes its count to zero.
func TestGitHelperCallSiteFloor(t *testing.T) {
	counts := gitHelperSites(t)
	if err := gitHelperSiteFloorErr(counts); err != nil {
		t.Error(err)
	}
	without := map[string]int{}
	for k, v := range counts {
		without[k] = v
	}
	delete(without, "gitRun")
	if gitHelperSiteFloorErr(without) == nil {
		t.Error("the floor passes with no gitRun call site audited")
	}
	atFloor := map[string]int{}
	for k, v := range gitHelperSiteFloor {
		atFloor[k] = v
	}
	if err := gitHelperSiteFloorErr(atFloor); err != nil {
		t.Errorf("the floor fails at its own minimum: %v", err)
	}
	atFloor["gitRun"]--
	if gitHelperSiteFloorErr(atFloor) == nil {
		t.Error("the floor passes one gitRun site under its minimum")
	}
}

// auditFinding is one flagged positional.
type auditFinding struct {
	Pos  string
	Arg  string
	Call string
	Bin  string
	Why  string
}

// auditFile walks one parsed file and returns the positionals it flags.
func auditFile(fset *token.FileSet, f *ast.File) []auditFinding {
	return auditFileHits(fset, f, nil)
}

// auditFileHits is auditFile recording, into hits when non-nil, every
// acceptedNonLiteralBinaries key a site in the file consumed.
func auditFileHits(fset *token.FileSet, f *ast.File, hits map[string]bool) []auditFinding {
	var out []auditFinding

	for _, imp := range f.Imports {
		if path, err := strconv.Unquote(imp.Path.Value); err == nil && path == gitrunImportPath && imp.Name != nil && imp.Name.Name != "gitrun" {
			out = append(out, auditFinding{
				Pos: fset.Position(imp.Pos()).String(), Arg: imp.Name.Name, Call: "import", Bin: "git",
				Why: "internal/gitrun imported as " + quote(imp.Name.Name) + " — every argv audit recognises the runner's calls by the gitrun selector, so an alias hides them all",
			})
		}
	}

	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		name, bin, binExpr, args, isSpawn := spawnArgvOf(call)
		if !isSpawn {
			return true
		}
		// A spread (f(x...)) hides its elements from the AST. Skipped rather
		// than flagged — see the accepted-with-reason note above.
		if call.Ellipsis.IsValid() {
			return true
		}
		pos := fset.Position(call.Pos())

		if bin == "" {
			if key := acceptedBinaryKey(pos.Filename, binExpr); key != "" {
				if hits != nil {
					hits[key] = true
				}
				return true
			}
			out = append(out, auditFinding{
				Pos: pos.String(), Arg: binExpr, Call: name, Bin: "?",
				Why: "the binary is not a literal, so no argv policy can be resolved for it",
			})
			return true
		}

		rule, known := argfence.PolicyFor(bin)
		if !known {
			out = append(out, auditFinding{
				Pos: pos.String(), Arg: bin, Call: name, Bin: bin,
				Why: "binary " + quote(bin) + " has no argv policy — add one to internal/argfence",
			})
			return true
		}

		flags := valueTakingFlags[bin]
		sawSeparator := false
		for i, a := range args {
			if lit, ok := stringLit(a); ok {
				if separatorTokens[lit] {
					sawSeparator = true
					continue
				}
				// A flag demoted PAST the separator is its own finding: the
				// separator ends flag parsing, so the flag becomes a positional.
				if sawSeparator && strings.HasPrefix(lit, "-") && rule.Kind == argfence.Separator {
					out = append(out, auditFinding{
						Pos: fset.Position(a.Pos()).String(), Arg: lit, Call: name, Bin: bin,
						Why: "flag appears after the end-of-options token, which makes it a positional — move it ahead of the separator",
					})
				}
				continue
			}
			if hasConstPrefix(a) {
				continue // a constant prefix makes a leading dash unreachable
			}
			// The value of a preceding VALUE-TAKING flag (`-m msg`, `--json
			// fields`) is an option ARGUMENT, not a positional: the tool reads
			// it literally, and a separator in front of it would become the
			// value.
			//
			// An explicit set, not "any preceding dash": `git branch -D <name>`
			// takes a boolean -D followed by a real positional, so treating
			// every flag as value-taking would wave through exactly the shape
			// this audit exists to catch.
			if i > 0 {
				if prev, ok := stringLit(args[i-1]); ok && flags[prev] {
					continue
				}
			}
			if rule.Kind == argfence.Separator && sawSeparator {
				continue // fenced: the tool cannot read it as an option
			}
			why := "passes a derived positional with no " + rule.Token + " before it"
			if rule.Kind == argfence.Reject {
				why = "has no end-of-options token, so a derived positional cannot be fenced at all — reject a leading dash before exec, or give the value a constant prefix"
			}
			out = append(out, auditFinding{
				Pos: fset.Position(a.Pos()).String(), Arg: exprText(a), Call: name, Bin: bin, Why: why,
			})
		}
		return true
	})
	return out
}

func quote(s string) string { return "\"" + s + "\"" }

// spawnArgvOf recognises every shape a subprocess argv takes in this codebase
// and returns the binary plus the argument slice that is the argv.
//
// bin is empty when the binary expression is not a string literal; binExpr then
// carries its source text for the accepted-with-reason lookup.
func spawnArgvOf(call *ast.CallExpr) (name, bin, binExpr string, args []ast.Expr, ok bool) {
	switch fn := call.Fun.(type) {
	case *ast.Ident:
		// gitRun(repoDir, args...) — first arg is the repo dir.
		if gitCallFuncs[fn.Name] && len(call.Args) > 1 {
			return fn.Name, "git", "", call.Args[1:], true
		}
		// ghCommand(args...) — the whole tail is the argv.
		if fn.Name == "ghCommand" && len(call.Args) > 0 {
			return fn.Name, "gh", "", call.Args, true
		}
	case *ast.SelectorExpr:
		pkg, isIdent := fn.X.(*ast.Ident)
		if !isIdent {
			return "", "", "", nil, false
		}
		// gitrun.Trim(dir, args...) and its siblings — the repo-wide git
		// runner; first arg is the dir, the tail is the git argv.
		if pkg.Name == "gitrun" && gitrunVerbs[fn.Sel.Name] && len(call.Args) > 1 {
			return "gitrun." + fn.Sel.Name, "git", "", call.Args[1:], true
		}
		if pkg.Name != "exec" {
			return "", "", "", nil, false
		}
		if fn.Sel.Name != "Command" && fn.Sel.Name != "CommandContext" {
			return "", "", "", nil, false
		}
		rest := call.Args
		if fn.Sel.Name == "CommandContext" && len(rest) > 0 {
			rest = rest[1:]
		}
		if len(rest) == 0 {
			return "", "", "", nil, false
		}
		callName := "exec." + fn.Sel.Name
		lit, isLit := stringLit(rest[0])
		if !isLit {
			return callName, "", exprText(rest[0]), rest[1:], true
		}
		return callName, lit, "", rest[1:], true
	}
	return "", "", "", nil, false
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
	case *ast.IndexExpr:
		return exprText(v.X) + "[…]"
	case *ast.CallExpr:
		return exprText(v.Fun) + "(…)"
	}
	return "<expr>"
}

// auditRoots are the trees scanned. Both, not just internal/cmd — see
// TestAuditScansCodexPackage.
var auditRoots = []string{"internal", "cmd"}

// runAudit walks the audit roots and returns every finding plus the file count.
func runAudit(t *testing.T) ([]auditFinding, int) {
	t.Helper()
	findings, scanned, _ := runAuditHits(t)
	return findings, scanned
}

// runAuditHits is runAudit also returning the acceptedNonLiteralBinaries keys
// the live tree consumed.
func runAuditHits(t *testing.T) ([]auditFinding, int, map[string]bool) {
	t.Helper()
	root := repoRootForDocs(t)
	fset := token.NewFileSet()
	var findings []auditFinding
	scanned := 0
	hits := map[string]bool{}

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
			findings = append(findings, auditFileHits(fset, f, hits)...)
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", r, err)
		}
	}
	return findings, scanned, hits
}

// TestAcceptedBinariesAreLive: every accepted non-literal binary still names a
// site in the tree. A key left behind when its spawn moved would sit ready to
// accept whatever next appears under that name — and a stale key is refused.
func TestAcceptedBinariesAreLive(t *testing.T) {
	_, _, hits := runAuditHits(t)
	for _, key := range staleAcceptedBinaries(acceptedNonLiteralBinaries, hits) {
		t.Errorf("acceptedNonLiteralBinaries[%q] matches no spawn site — its site moved or went; re-key or drop it", key)
	}
	table := map[string]string{"internal/cmd/gone.go:vanished": "a site that no longer exists"}
	for k, v := range acceptedNonLiteralBinaries {
		table[k] = v
	}
	if got := staleAcceptedBinaries(table, hits); len(got) != 1 || got[0] != "internal/cmd/gone.go:vanished" {
		t.Errorf("stale keys = %v, want exactly the vanished one", got)
	}
}

// TestAliasedGitrunImportIsAFinding: the audits see a git argv through the
// gitrun selector, so importing the runner under another name — or dotted in —
// would hide every call. The import itself is reported; the plain import is
// not.
func TestAliasedGitrunImportIsAFinding(t *testing.T) {
	for _, tc := range []struct {
		name, imp string
		want      int
	}{
		{"plain", `"` + gitrunImportPath + `"`, 0},
		{"aliased", `g "` + gitrunImportPath + `"`, 1},
		{"dotted", `. "` + gitrunImportPath + `"`, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, "x.go", "package x\n\nimport "+tc.imp+"\n", 0)
			if err != nil {
				t.Fatal(err)
			}
			got := auditFile(fset, f)
			if len(got) != tc.want {
				t.Fatalf("findings = %+v, want %d", got, tc.want)
			}
			if tc.want == 1 && !strings.Contains(got[0].Why, "gitrun selector") {
				t.Errorf("the finding does not say why an alias matters: %s", got[0].Why)
			}
		})
	}
}

// TestAcceptedBinaryKeyIsRepoRelative: the remote.go key accepts its seam at
// internal/remote/remote.go and nowhere else — the same expression in a file
// that merely shares the base name is a finding.
func TestAcceptedBinaryKeyIsRepoRelative(t *testing.T) {
	const src = "package x\n\nimport \"os/exec\"\n\nfunc f(argv []string) *exec.Cmd {\n\tcmd := exec.Command(argv[0])\n\tcmd.Args = argv\n\treturn cmd\n}\n"
	dir := t.TempDir()
	for _, tc := range []struct {
		rel      string
		accepted bool
	}{
		{"internal/remote/remote.go", true},
		{"internal/testlane/remote.go", false},
		{"remote.go", false},
	} {
		t.Run(tc.rel, func(t *testing.T) {
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, filepath.Join(dir, filepath.FromSlash(tc.rel)), src, 0)
			if err != nil {
				t.Fatal(err)
			}
			hits := map[string]bool{}
			findings := auditFileHits(fset, f, hits)
			if tc.accepted {
				if len(findings) != 0 || !hits["internal/remote/remote.go:argv[…]"] {
					t.Errorf("findings = %v, hits = %v — the seam was not accepted by its own key", findings, hits)
				}
				return
			}
			if len(findings) != 1 || len(hits) != 0 {
				t.Errorf("findings = %v, hits = %v — a base-name match was accepted", findings, hits)
			}
		})
	}
}

// TestNoUnseparatedPositional is the gate, across every binary.
func TestNoUnseparatedPositional(t *testing.T) {
	findings, scanned := runAudit(t)
	if scanned == 0 {
		t.Fatal("scanned no files — the audit would pass vacuously")
	}
	for _, f := range findings {
		t.Errorf("%s: %s(…) → %s: %q %s", f.Pos, f.Call, f.Bin, f.Arg, f.Why)
	}
}

// TestNoUnseparatedGitPositional is the original git-only guarantee, kept as its
// own test after the generalisation. A broader gate that quietly stopped
// covering git would still pass TestNoUnseparatedPositional with zero findings.
func TestNoUnseparatedGitPositional(t *testing.T) {
	findings, scanned := runAudit(t)
	if scanned == 0 {
		t.Fatal("scanned no files — the audit would pass vacuously")
	}
	for _, f := range findings {
		if f.Bin != "git" {
			continue
		}
		t.Errorf("%s: %s(…) passes %q as a positional with no separator before it — build the argv with gitRefArgs/gitPathArgs",
			f.Pos, f.Call, f.Arg)
	}
}

// TestUnknownBinaryFailsClosed: the default is flag, not pass. A tool nobody
// anticipated is in scope the day it is added — that is the whole point of the
// locked audit_gate_breadth decision.
func TestUnknownBinaryFailsClosed(t *testing.T) {
	got := auditSnippet(t, `exec.Command("cargo", "test", pkg)`)
	if len(got) == 0 {
		t.Fatal("an unlisted binary was waved through")
	}
	if !strings.Contains(got[0].Why, "no argv policy") {
		t.Errorf("finding does not say why: %+v", got[0])
	}
	// A NON-LITERAL binary fails closed too, unless named with its reason.
	got = auditSnippet(t, `exec.Command(chosen, "install")`)
	if len(got) == 0 {
		t.Error("a non-literal binary was waved through")
	}
}

// TestAuditFlagsItsOwnSnippets checks the checker. An audit that quietly
// degraded into "return no findings" would pass the real gate forever and read
// as coverage, so it is made to prove it can still see.
//
// The floor is DERIVED from the table — checked must equal the number of
// FLAG/PASS rows the file actually contains — rather than a number chosen in
// advance, so it cannot drift below the real count and no row can be deleted
// without failing.
func TestAuditFlagsItsOwnSnippets(t *testing.T) {
	root := repoRootForDocs(t)
	path := filepath.Join(root, "internal", "cmd", "testdata", "subprocargs_audit", "snippets.txt")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	var (
		name    string
		want    bool
		src     []string
		checked int
		rows    int
	)
	flush := func() {
		if name == "" {
			return
		}
		t.Run(name, func(t *testing.T) {
			got := auditSnippet(t, src...)
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
			rows++
			flush()
			want = strings.HasPrefix(trimmed, "FLAG ")
			name = strings.TrimSpace(trimmed[5:])
			src = nil
			continue
		}
		src = append(src, line)
	}
	flush()

	if rows == 0 {
		t.Fatal("parsed no FLAG/PASS rows — the table or the parser is broken")
	}
	if checked != rows {
		t.Fatalf("parsed %d rows but exercised %d — the table is being silently truncated", rows, checked)
	}
}

// auditSnippet parses one or more lines of Go inside a synthetic function and
// returns what the audit makes of them.
func auditSnippet(t *testing.T, lines ...string) []auditFinding {
	t.Helper()
	fset := token.NewFileSet()
	const preamble = "package cmd\n" +
		"import \"os/exec\"\n" +
		"var _ = exec.Command\n" +
		"var ghCommand func(...string) *exec.Cmd\n" +
		"func gitRun(dir string, a ...string) error { return nil }\n" +
		"func gitNoOut(dir string, a ...string) error { return nil }\n" +
		"func gitTrim(dir string, a ...string) (string, error) { return \"\", nil }\n" +
		"func snippet(repoDir, branch, base, ref, path, msg, pkg, dir, fields, num, file, lang, pattern, mutate, chosen, host string) {\n"
	f, err := parser.ParseFile(fset, "snippet.go", preamble+strings.Join(lines, "\n")+"\n}\n", 0)
	if err != nil {
		t.Fatalf("snippet does not parse: %v\n%s", err, strings.Join(lines, "\n"))
	}
	return auditFile(fset, f)
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
			got := len(auditSnippet(t, "\t"+tc.line)) > 0
			if got != tc.want {
				t.Errorf("flagged = %v, want %v, for: %s", got, tc.want, tc.line)
			}
		})
	}
}

// TestAuditScansCodexPackage: the first sweep of the git-only version nearly
// stopped at internal/cmd, and internal/codex/git.go shells git too. A narrowed
// scan root is a silent loss of coverage, so it is asserted rather than assumed.
func TestAuditScansCodexPackage(t *testing.T) {
	assertAuditCovers(t, filepath.Join("internal", "codex", "git.go"))
}

// TestAuditScansMutationAndShip is the same assertion for the two packages this
// phase brought into scope. They are where the non-git spawns live, so a root
// list that stopped covering them would leave the generalisation cosmetic.
func TestAuditScansMutationAndShip(t *testing.T) {
	for _, rel := range []string{
		filepath.Join("internal", "mutation", "gremlins.go"),
		filepath.Join("internal", "mutation", "stryker.go"),
		filepath.Join("internal", "mutation", "stryker_net.go"),
		filepath.Join("internal", "ship", "open.go"),
		filepath.Join("internal", "ship", "merged.go"),
		filepath.Join("internal", "codex", "ast_grep.go"),
	} {
		assertAuditCovers(t, rel)
	}
}

// TestAuditScansRemotePackage: internal/remote is where dross spawns ssh and
// rsync, the two binaries in the table that can execute an arbitrary LOCAL
// command from a flag (-o ProxyCommand, -e). A scan root that stopped covering
// it would leave the worst case unwatched.
//
// hold.go is the host-lock session: it spawns through remote.go's buildCommand
// (the accepted "remote.go:argv[…]" entry above covers it — there is ONE exec
// seam in the package, on purpose) but it is a second file that hands an argv
// to that seam, and the walk must see it.
func TestAuditScansRemotePackage(t *testing.T) {
	assertAuditCovers(t, filepath.Join("internal", "remote", "remote.go"))
	assertAuditCovers(t, filepath.Join("internal", "remote", "hold.go"))
}

func assertAuditCovers(t *testing.T, rel string) {
	t.Helper()
	root := repoRootForDocs(t)
	target := filepath.Join(root, rel)
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("expected %s to exist: %v", rel, err)
	}
	for _, r := range auditRoots {
		if strings.HasPrefix(target, filepath.Join(root, r)+string(filepath.Separator)) {
			return
		}
	}
	t.Errorf("%s is outside the audit roots %v", rel, auditRoots)
}

// TestAuditKnowsEveryPolicyBinary keeps the two tables in step: a binary that
// gains an argfence policy but no value-taking-flag entry silently loses the
// option-argument carve-out, which shows up as a wave of false positives nobody
// can act on.
func TestAuditKnowsEveryPolicyBinary(t *testing.T) {
	var missing []string
	for bin := range argfence.Policy() {
		if _, ok := valueTakingFlags[bin]; !ok {
			missing = append(missing, bin)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("binaries with an argfence policy but no value-taking-flag set: %v", missing)
	}
	for bin := range valueTakingFlags {
		if _, ok := argfence.PolicyFor(bin); !ok {
			t.Errorf("valueTakingFlags names %q, which has no argfence policy — the entry is stale", bin)
		}
	}
}
