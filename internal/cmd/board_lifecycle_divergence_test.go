package cmd

import (
	"github.com/Rivil/dross/internal/configenum"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"testing"

	"github.com/Rivil/dross/internal/board"
)

// The reap-side half of the board lifecycle divergence guard. The emit-side
// and mirror-lane tests moved with the sync core to internal/boardsync; these
// three stay here because they read reapLanes / reapLaneFor from issue_reap.go,
// which t-8 moves — they move with it. Until then the lane table below is a
// copy of boardsync's; the two are reconciled when they meet.

// mirrorLane says how one board.json namespace's cards reach a terminal state,
// and which lifecycle statuses belong to that lane.
type mirrorLane struct {
	// prompt and emission are the terminal call site: which prompt must carry
	// it, and the literal that must appear there. Matched against the RAW file
	// rather than promptContent's normalised form — quick.md's line carries a
	// shell variable, and the normaliser strips underscores, so a normalised
	// match would silently depend on $NEW_VERSION becoming $newversion.
	prompt   string
	emission string
	// terminal is the lifecycle status that emission carries; others are the
	// lane's own non-terminal statuses. Empty terminal means the lane closes
	// without a --status, which is not a gap: see backlog below.
	terminal string
	others   []string
	// reapTerminal is the status `dross issue reap` writes when it closes one
	// of this lane's stranded cards. It lives in the same row as the forward
	// emission on purpose: the locked reap_state decision is that a reaped card
	// lands in the SAME state the forward path writes, so the board stays one
	// coherent history and the state map stays the single source of truth. Kept
	// as its own field rather than reusing `terminal` because the two are not
	// always literally equal — the backlog lane closes without a --status,
	// which boardsync.CloseIssue resolves to "complete" — and collapsing them would
	// hide exactly that case.
	reapTerminal string
}

// mirrorLanes is keyed by board.Board FIELD NAME, and the test below checks
// that key set against reflection over the struct rather than trusting it. A
// namespace added to Board with no entry here fails, which is the point: a new
// kind of mirror must say how its cards end.
var mirrorLanes = map[string]mirrorLane{
	"Phases": {
		prompt:       "ship.md",
		emission:     "dross issue phase sync <phase-id> --status complete --close",
		terminal:     "complete",
		reapTerminal: "complete",
		others:       []string{"planned", "in-progress", "shipped", "uat"},
	},
	"Tasks": {
		prompt:       "ship.md",
		emission:     "dross issue task sync <phase-id> --status task-complete --close",
		terminal:     "task-complete",
		reapTerminal: "task-complete",
		others:       []string{"task-in-progress", "task-in-review"},
	},
	"Quicks": {
		prompt:   "quick.md",
		emission: "dross issue quick $NEW_VERSION --close",
		terminal: "complete", // closeBoardIssue's default for a lane with no --status
		// The sweep never auto-closes a quick — there is no completion record
		// to read — but the lane still has a terminal, because a quick closed
		// BY HAND off the plan's unattributable list must land in the same
		// column the forward path uses.
		reapTerminal: "complete",
	},
	"Milestones": {
		prompt:       "milestone.md",
		emission:     "dross issue milestone sync <version> --close",
		terminal:     "complete",
		reapTerminal: "complete",
	},
	"Backlog": {
		prompt: "ship.md",
		// The backlog lane has no --close flag by design: backlog sync owns the
		// live set and reconciles both directions, closing the mirrors whose
		// artefact resolved. Asserting a flag here would guard something that
		// was never built.
		emission: "dross issue backlog sync",
		// No forward --status, so no forward terminal — but the sweep does pass
		// one, and it is boardsync.CloseIssue's own default for exactly this case.
		reapTerminal: "complete",
	},
}

// boardNamespaceFields enumerates board.Board's map-typed fields — the same
// derivation internal/board's namespace guard uses, from the same struct, so
// the two cannot disagree about how many mirror lanes exist.
func boardNamespaceFields(t *testing.T) []string {
	t.Helper()
	typ := reflect.TypeOf(board.Board{})
	var out []string
	for i := 0; i < typ.NumField(); i++ {
		if typ.Field(i).Type.Kind() == reflect.Map {
			out = append(out, typ.Field(i).Name)
		}
	}
	if len(out) == 0 {
		t.Fatal("reflection found no map-typed fields on board.Board — every assertion below would be vacuous")
	}
	sort.Strings(out)
	return out
}

// stateMapKeys extracts the keys of a package-level map literal from source,
// rather than exporting the map for the test's benefit. Same technique
// enum_divergence_test.go uses on the dispatch switches.
func stateMapKeys(t *testing.T, path, varName string) []string {
	pairs := stateMapPairs(t, path, varName)
	keys := make([]string, 0, len(pairs))
	for k := range pairs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// stateMapPairs reads the same declaration as stateMapKeys but keeps the
// VALUES too — which board state each status resolves to. The lane guards below
// need them to ask whether two statuses land in the same column, and the maps
// are unexported in package forge, so source is the only way to read them from
// here.
func stateMapPairs(t *testing.T, path, varName string) map[string]string {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	pairs := map[string]string{}
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
					v, ok := kv.Value.(*ast.BasicLit)
					if !ok || v.Kind != token.STRING {
						t.Errorf("%s[%q] has a non-literal value %v — this guard can only read string literals", varName, s, kv.Value)
						continue
					}
					state, err := strconv.Unquote(v.Value)
					if err != nil {
						t.Errorf("%s: unquote %s: %v", varName, v.Value, err)
						continue
					}
					pairs[s] = state
				}
			}
		}
	}
	if !found {
		t.Fatalf("no %s in %s", varName, path)
	}
	return pairs
}

// TestEveryMirrorLaneHasATerminalEmission above asks whether every mirror class
// has a way FORWARD to a terminal state. These ask the other half: whether
// every mirror class has a way BACK — a reap path for the cards the forward
// lifecycle already left behind.
//
// The two questions are separate because the answers were: the forward
// emissions were added when the lifecycle was fixed, and the ninety cards that
// predate them are not reachable by any of those lines. A namespace can have a
// perfect forward path and still have no way to close its history.

// TestEveryMirrorLaneHasATerminalEmission above asks whether every mirror class
// has a way FORWARD to a terminal state. These ask the other half: whether
// every mirror class has a way BACK — a reap path for the cards the forward
// lifecycle already left behind.
//
// The two questions are separate because the answers were: the forward
// emissions were added when the lifecycle was fixed, and the ninety cards that
// predate them are not reachable by any of those lines. A namespace can have a
// perfect forward path and still have no way to close its history.

// TestEveryBoardNamespaceHasAReapPath: a map field added to board.Board with no
// reap lane fails by field name in the same run that adds it.
func TestEveryBoardNamespaceHasAReapPath(t *testing.T) {
	fields := boardNamespaceFields(t)
	for _, field := range fields {
		lane, ok := reapLaneFor(field)
		if !ok {
			t.Errorf("board.Board has a %s namespace with no reap lane — every stranded %s card would be unreachable by the sweep; add it to reapLanes", field, field)
			continue
		}
		if lane.Terminal == "" {
			t.Errorf("the %s reap lane declares no terminal status — the sweep would have nothing to write", field)
		}
	}

	// The reverse: a lane naming a namespace that no longer exists is a
	// classifier arm nothing can ever reach.
	known := map[string]bool{}
	for _, f := range fields {
		known[f] = true
	}
	for _, lane := range reapLanes {
		if !known[lane.Name] {
			t.Errorf("reapLanes describes %q, which is not a board.Board namespace any more — drop it rather than leaving a classifier arm over nothing", lane.Name)
		}
	}
	if len(reapLanes) == 0 {
		t.Fatal("the reap lane registry is empty — every assertion here would be vacuous")
	}
}

// TestReapTerminalMatchesTheForwardTerminal holds the locked reap_state
// decision: a reaped card lands in the same state the forward lifecycle writes
// for its class. A sweep-specific state would make the board two histories and
// double the mapping — and the mapping is the thing the previous phase made
// trustworthy.
func TestReapTerminalMatchesTheForwardTerminal(t *testing.T) {
	checked := 0
	for _, field := range boardNamespaceFields(t) {
		lane, ok := reapLaneFor(field)
		if !ok {
			continue // TestEveryBoardNamespaceHasAReapPath owns the missing case
		}
		row, ok := mirrorLanes[field]
		if !ok {
			continue // TestEveryMirrorLaneHasATerminalEmission owns that one
		}
		checked++
		if row.reapTerminal != lane.Terminal {
			t.Errorf("the %s lane reaps to %q but the registry records %q — the two tables have drifted", field, lane.Terminal, row.reapTerminal)
		}
		// The backlog lane is the one place the forward path carries no
		// --status, so its forward terminal is empty and only the reap one is
		// set. Everywhere else the two must be the same column.
		if row.terminal != "" && row.terminal != row.reapTerminal {
			t.Errorf("the %s lane ends forward at %q but reaps to %q — reap_state says a reaped card lands where the forward path puts it", field, row.terminal, row.reapTerminal)
		}
	}
	if checked == 0 {
		t.Fatal("compared no lanes — the guard would pass vacuously")
	}
}

// TestEveryReapTerminalIsMapped: a terminal nothing can resolve is a close that
// fails on a live board, per provider. Both default maps are checked
// independently, and the failure names which provider is missing which status.
func TestEveryReapTerminalIsMapped(t *testing.T) {
	root := repoRootFromTest(t)
	maps := map[string]map[string]string{
		"jira":     stateMapPairs(t, filepath.Join(root, "internal", "forge", "jira.go"), "defaultJiraStateMap"),
		"youtrack": stateMapPairs(t, filepath.Join(root, "internal", "forge", "youtrack.go"), "defaultYouTrackStateMap"),
	}
	for provider, states := range maps {
		if len(states) == 0 {
			t.Fatalf("parsed no state-map pairs for %s — every assertion below would be vacuous", provider)
		}
	}

	for _, lane := range reapLanes {
		if !configenum.LifecycleStatuses.Has(lane.Terminal) {
			t.Errorf("the %s lane reaps to %q, which is not a lifecycle status (%s) — nothing downstream could validate it", lane.Name, lane.Terminal, configenum.LifecycleStatuses.List())
			continue
		}
		for provider, states := range maps {
			if _, ok := states[lane.Terminal]; !ok {
				t.Errorf("%s has no state-map entry for %q, the %s lane's reap terminal — every close in that lane would fail on a %s board", provider, lane.Terminal, lane.Name, provider)
			}
		}
	}
}
