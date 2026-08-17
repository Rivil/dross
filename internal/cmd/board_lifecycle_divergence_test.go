package cmd

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/configenum"
)

// Board lifecycle / state-map divergence guard (c-2).
//
// The lifecycle vocabulary has two independent sides. dross *emits* a status —
// from a `--status <literal>` in a workflow prompt, or from derivePhaseStatus
// when the flag is absent — and each forge *keys* a state map on one. Nothing
// in the type system connects them, and they drifted: the producer emitted
// "planning" while both maps keyed "planned", so the commonest phase shape
// there is warned and skipped its board transition.
//
// These tests gate both directions. A status dross emits with no state-map
// entry fails; a state map keyed on a status nothing emits fails.
//
// **"Emits" means call sites only** — the `--status <literal>` occurrences in
// assets/prompts/*.md plus the values derivePhaseStatus can return. Declared Go
// constants are deliberately NOT counted: issue.go's constants are defined *as*
// members of configenum.LifecycleStatuses (issue_test.go asserts exactly that),
// so folding them in would make the emit-set equal the Set by construction and
// turn this file into a tautology. The point is to catch a status that has a
// definition but no longer has a caller, which is precisely how "shipped" and
// "complete" sat in both maps as keys nothing ever resolved.

// statusLiteralRE matches a `--status <value>` occurrence in a prompt. The
// value is the lifecycle vocabulary's own shape — lowercase words and hyphens —
// so a placeholder like `--status <value>` is not matched and does not need an
// exemption.
var statusLiteralRE = regexp.MustCompile(`--status\s+([a-z][a-z-]*)`)

// emittedStatuses collects every lifecycle status dross can put on a board.
func emittedStatuses(t *testing.T) map[string]string {
	t.Helper()
	root := repoRootFromTest(t)
	out := map[string]string{} // status -> where it is emitted

	// 1. Prompt call sites.
	promptDir := filepath.Join(root, "assets", "prompts")
	entries, err := os.ReadDir(promptDir)
	if err != nil {
		t.Fatalf("read %s: %v", promptDir, err)
	}
	scanned := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(promptDir, e.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}
		scanned++
		for _, m := range statusLiteralRE.FindAllStringSubmatch(string(b), -1) {
			out[m[1]] = "assets/prompts/" + e.Name()
		}
	}
	if scanned == 0 {
		t.Fatal("scanned no prompt files — the emit-set would be empty and every assertion below vacuous")
	}

	// 2. derivePhaseStatus's return values, read as source. Executing the
	// function instead would only reach the branches a fixture happens to
	// trigger; parsing reaches every return statement, so a new branch is
	// caught the day it is written.
	for _, s := range derivePhaseStatusReturns(t, filepath.Join(root, "internal", "cmd", "issue.go")) {
		out[s] = "internal/cmd/issue.go:derivePhaseStatus"
	}
	return out
}

// derivePhaseStatusReturns resolves every value derivePhaseStatus can return.
// Returns are written as constant identifiers, so the file's string constants
// are collected first and the identifiers resolved through them.
func derivePhaseStatusReturns(t *testing.T, path string) []string {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}

	consts := map[string]string{}
	for _, decl := range file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			continue
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, name := range vs.Names {
				if i >= len(vs.Values) {
					continue
				}
				if lit, ok := vs.Values[i].(*ast.BasicLit); ok && lit.Kind == token.STRING {
					if v, err := strconv.Unquote(lit.Value); err == nil {
						consts[name.Name] = v
					}
				}
			}
		}
	}

	var fn *ast.FuncDecl
	for _, decl := range file.Decls {
		if fd, ok := decl.(*ast.FuncDecl); ok && fd.Name.Name == "derivePhaseStatus" && fd.Body != nil {
			fn = fd
			break
		}
	}
	if fn == nil {
		t.Fatalf("no derivePhaseStatus in %s — the emit-set is missing its Go half", path)
	}

	var got []string
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		ret, ok := n.(*ast.ReturnStmt)
		if !ok || len(ret.Results) != 1 {
			return true
		}
		switch v := ret.Results[0].(type) {
		case *ast.Ident:
			if s, ok := consts[v.Name]; ok {
				got = append(got, s)
			} else {
				t.Errorf("derivePhaseStatus returns identifier %q, which is not a string constant in %s — the emit-set cannot see what it resolves to", v.Name, path)
			}
		case *ast.BasicLit:
			if v.Kind == token.STRING {
				if s, err := strconv.Unquote(v.Value); err == nil {
					got = append(got, s)
				}
			}
		}
		return true
	})
	if len(got) == 0 {
		t.Fatal("derivePhaseStatus has no resolvable return values")
	}
	return got
}

// stateMapKeys extracts the keys of a package-level map literal from source,
// rather than exporting the map for the test's benefit. Same technique
// enum_divergence_test.go uses on the dispatch switches.
func stateMapKeys(t *testing.T, path, varName string) []string {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	var keys []string
	found := false
	for _, decl := range file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.VAR {
			continue
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, name := range vs.Names {
				if name.Name != varName || i >= len(vs.Values) {
					continue
				}
				lit, ok := vs.Values[i].(*ast.CompositeLit)
				if !ok {
					t.Fatalf("%s in %s is not a composite literal", varName, path)
				}
				found = true
				for _, elt := range lit.Elts {
					kv, ok := elt.(*ast.KeyValueExpr)
					if !ok {
						continue
					}
					k, ok := kv.Key.(*ast.BasicLit)
					if !ok || k.Kind != token.STRING {
						t.Errorf("%s has a non-literal key %v — this guard can only read string literals", varName, kv.Key)
						continue
					}
					s, err := strconv.Unquote(k.Value)
					if err != nil {
						t.Errorf("%s: unquote %s: %v", varName, k.Value, err)
						continue
					}
					keys = append(keys, s)
				}
			}
		}
	}
	if !found {
		t.Fatalf("no %s in %s", varName, path)
	}
	return keys
}

// TestEmittedStatusesAreTheLifecycleSet gates the producer side: what dross
// actually puts on a board is exactly configenum.LifecycleStatuses — no more
// (a status the maps cannot resolve) and no less (a Set member with no caller,
// which is a map key nothing will ever reach).
func TestEmittedStatusesAreTheLifecycleSet(t *testing.T) {
	emitted := emittedStatuses(t)

	for status, where := range emitted {
		if !configenum.LifecycleStatuses.Has(status) {
			t.Errorf("%s emits --status %q, which is not a lifecycle status (expected %s) — no state map will resolve it",
				where, status, configenum.LifecycleStatuses.List())
		}
	}
	for _, want := range configenum.LifecycleStatuses.Values() {
		if _, ok := emitted[want]; !ok {
			t.Errorf("lifecycle status %q is in the Set but nothing emits it — a state-map key nothing will ever reach (emitted: %s)",
				want, strings.Join(sortedSet(emitted), ", "))
		}
	}
}

// TestStateMapsKeyExactlyTheEmittedStatuses gates the map side, per provider.
//
// The two maps are checked independently on purpose: a key present in one and
// missing from the other is a real bug for whoever uses that tracker, and
// checking their union or intersection would average it away.
func TestStateMapsKeyExactlyTheEmittedStatuses(t *testing.T) {
	root := repoRootFromTest(t)
	emitted := emittedStatuses(t)

	for _, m := range []struct {
		provider string
		path     string
		varName  string
	}{
		{"jira", filepath.Join(root, "internal", "forge", "jira.go"), "defaultJiraStateMap"},
		{"youtrack", filepath.Join(root, "internal", "forge", "youtrack.go"), "defaultYouTrackStateMap"},
	} {
		t.Run(m.provider, func(t *testing.T) {
			keys := stateMapKeys(t, m.path, m.varName)
			have := map[string]bool{}
			for _, k := range keys {
				have[k] = true
			}

			for status := range emitted {
				if !have[status] {
					t.Errorf("a status dross emits has no state-map entry: %q is missing from %s (%s emits it)", status, m.varName, emitted[status])
				}
			}
			for _, k := range keys {
				if _, ok := emitted[k]; !ok {
					t.Errorf("a state map keys on a status nothing emits: %s has %q, which no prompt and no derivePhaseStatus branch produces", m.varName, k)
				}
			}
		})
	}
}

// taskStatusWriteRE matches the plan-side status write at an execute edge:
// `dross task status <phase> <task-id> <plan-status>`.
var taskStatusWriteRE = regexp.MustCompile(`dross task status\s+\S+\s+\S+\s+([a-z_]+)`)

// taskSyncEdgeRE matches the board-side write at the same edge:
// `dross issue task-sync <phase> <task-id> --status <lifecycle-status>`.
//
// The task-id argument is required by the pattern, and required not to be a
// flag: a `task-sync <phase> --status …` with no task id syncs the whole phase
// at once, which is not an edge and must not satisfy either half below.
var taskSyncEdgeRE = regexp.MustCompile(`dross issue task-sync\s+\S+\s+([^-\s]\S*)\s+--status\s+([a-z][a-z-]*)`)

// taskEdges is the pairing c-2 actually claims: picking a task moves its card
// to in-progress, committing it moves the card to a review state. The key is
// the plan status execute writes at that edge; the value is the lifecycle
// status the board write beside it must carry.
var taskEdges = map[string]string{
	"in_progress": "task-in-progress",
	"done":        "task-in-review",
}

// TestTaskEdgesPairTheBoardStatusWithThePlanStatus pins WHICH execute edge
// emits which task status.
//
// TestEmittedStatusesAreTheLifecycleSet above is a `--status <literal>` regex
// over every prompt, so it only ever proves that both task literals appear
// somewhere in the corpus. Transposing execute.md's pick and commit call sites
// would invert c-2 — a picked task's card would jump straight to review and a
// committed one would go back to in-progress — and that guard, and every other
// test, would still pass.
//
// The anchor is the `dross task status` write execute already makes at the same
// edge. That write is what actually defines the edge (in_progress means picked,
// done means committed), so tying the board status to the plan status beside it
// pins the pairing to something that cannot be relabelled without changing what
// the loop does.
func TestTaskEdgesPairTheBoardStatusWithThePlanStatus(t *testing.T) {
	root := repoRootFromTest(t)
	path := filepath.Join(root, "assets", "prompts", "execute.md")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	// Walk the prompt in order, recording both kinds of write as they appear.
	// Order is the whole signal here: a board write belongs to the plan write
	// most recently above it, which is how the two lines of one code fence
	// relate.
	type event struct {
		line   int
		plan   string // set on a `dross task status` write
		board  string // set on a `dross issue task-sync … --status` write
		taskID string
	}
	var events []event
	for i, line := range strings.Split(string(b), "\n") {
		if m := taskStatusWriteRE.FindStringSubmatch(line); m != nil {
			events = append(events, event{line: i + 1, plan: m[1]})
		}
		if m := taskSyncEdgeRE.FindStringSubmatch(line); m != nil {
			events = append(events, event{line: i + 1, board: m[2], taskID: m[1]})
		}
	}

	// paired[planStatus] is the board status found at that edge, so a missing
	// edge and a duplicated one are both visible below.
	paired := map[string][]string{}
	lastPlan := ""
	lastPlanLine := 0
	for _, e := range events {
		if e.plan != "" {
			lastPlan = e.plan
			lastPlanLine = e.line
			continue
		}
		if lastPlan == "" {
			t.Errorf("%s:%d syncs %s to the board with --status %q, but no `dross task status` write precedes it — nothing says which edge this is",
				path, e.line, e.taskID, e.board)
			continue
		}
		want, known := taskEdges[lastPlan]
		if !known {
			t.Errorf("%s:%d writes plan status %q (line %d) and then board status %q, but %q is not an edge this guard knows — add it to taskEdges or the pairing is unchecked",
				path, e.line, lastPlan, lastPlanLine, e.board, lastPlan)
			continue
		}
		if e.board != want {
			t.Errorf("%s:%d emits --status %q at the edge that writes plan status %q (line %d), want %q — picking a task must move its card to %s and committing it to %s",
				path, e.line, e.board, lastPlan, lastPlanLine, want, taskEdges["in_progress"], taskEdges["done"])
		}
		paired[lastPlan] = append(paired[lastPlan], e.board)
	}

	for plan, want := range taskEdges {
		switch got := paired[plan]; len(got) {
		case 1: // the shape we want
		case 0:
			t.Errorf("no board write follows the `dross task status … %s` edge in %s — that edge stops moving the card, and %q would become a lifecycle status nothing emits",
				plan, path, want)
		default:
			t.Errorf("%d board writes follow the `dross task status … %s` edge in %s (%s) — one edge, one card move",
				len(got), plan, path, strings.Join(got, ", "))
		}
	}
}

// sortedSet renders a status->where map's keys for an error message.
func sortedSet(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
