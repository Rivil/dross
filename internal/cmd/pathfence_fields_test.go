package cmd

import (
	"bufio"
	"fmt"
	"go/token"
	"go/types"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/pathfence"
)

// The enumerating guard behind c-6 and c-9.
//
// artifact_scope (locked) puts EVERY path-shaped field in scope, not merely the
// ones something opens today. The old walker decided what was path-shaped from
// two hand-kept lists — four schema directories and an eleven-word tag
// vocabulary — and the 2026-09-07 experiment showed what that costs: three new
// string fields on phase.Task under unlisted tag words (workdir, output_dir,
// spec) passed silently.
//
// So nothing is guessed any more. EVERY toml- or json-tagged field, in every
// package the shared load matched, whose type bottoms out in a string —
// string, []string, a named string type, a pointer or slice of one — must be
// declared, in exactly one of two places:
//
//   - pathfence.Fields(), if it holds a path, with its disposition; or
//   - testdata/pathfence_scan/not_paths.txt, the hand-edited ledger of
//     serialized strings that are not paths.
//
// Both directions are checked: an undeclared field fails with the exact ledger
// row to paste, and a registry entry or ledger row naming a field that no
// longer exists fails as stale. There is no -update flag: filing a field is a
// judgement, and the path-field taint scan (pathtaint_audit_test.go) holds the
// ledger to it — a ledger row whose value reaches the filesystem is a finding.
//
// OUT of the enumeration, by construction: untagged exported fields (which
// BurntSushi/toml and encoding/json serialize under the Go name), maps keyed by
// path (verify's WholeFile/Ranges provenance), fields of struct types declared
// inside functions, and fields of anonymous struct types.

// serializedFieldFloor is ~25% under the 380 fields the walk finds today.
const serializedFieldFloor = 285

// notPathsLedger is the ledger of serialized string fields that are not paths.
const notPathsLedger = "testdata/pathfence_scan/not_paths.txt"

// serializedField is one enumerated field.
type serializedField struct {
	Tag string
	Pos token.Position
}

// stringValued reports whether a value of type t is, or is made of, strings:
// through pointers, slices, arrays and named types, never through a map or a
// struct.
func stringValued(t types.Type) bool {
	for range 16 {
		switch u := t.Underlying().(type) {
		case *types.Pointer:
			t = u.Elem()
		case *types.Slice:
			t = u.Elem()
		case *types.Array:
			t = u.Elem()
		case *types.Basic:
			return u.Kind() == types.String
		default:
			return false
		}
	}
	return false
}

// serializedTagKey is a struct tag's toml key, falling back to json, lowercased
// with its options stripped. It reports false for "-" and for neither key.
func serializedTagKey(tag string) (string, bool) {
	st := reflect.StructTag(tag)
	v := st.Get("toml")
	if v == "" {
		v = st.Get("json")
	}
	// The split is load-bearing: most fields carry an option
	// (`toml:"files,omitempty"`), and a whole-value match would miss them.
	key := strings.ToLower(strings.TrimSpace(strings.Split(v, ",")[0]))
	if key == "" || key == "-" {
		return "", false
	}
	return key, true
}

// serializedStringFields enumerates every tagged string-valued field of every
// package-level struct type in the view, keyed "<pkg>.<Type>.<Field>" by
// package NAME — the spelling pathfence.Fields() uses. A key two packages would
// both produce is reported, never silently merged.
func serializedStringFields(v *srcView) (map[string]serializedField, []string) {
	out := map[string]serializedField{}
	from := map[string]string{}
	var collisions []string
	for _, p := range v.Pkgs {
		scope := p.Types.Scope()
		for _, name := range scope.Names() {
			tn, ok := scope.Lookup(name).(*types.TypeName)
			if !ok || tn.IsAlias() {
				continue
			}
			st, ok := tn.Type().Underlying().(*types.Struct)
			if !ok {
				continue
			}
			for i := 0; i < st.NumFields(); i++ {
				f := st.Field(i)
				key, ok := serializedTagKey(st.Tag(i))
				if !ok || !stringValued(f.Type()) {
					continue
				}
				full := p.Types.Name() + "." + name + "." + f.Name()
				if prev, dup := from[full]; dup && prev != p.Path {
					collisions = append(collisions, fmt.Sprintf("%s is declared in both %s and %s", full, prev, p.Path))
				}
				from[full] = p.Path
				out[full] = serializedField{Tag: key, Pos: v.Fset.Position(f.Pos())}
			}
		}
	}
	sort.Strings(collisions)
	return out, collisions
}

// ledgerRow is one not_paths.txt row.
type ledgerRow struct {
	Tag  string
	Line int
}

// parseNotPaths reads the ledger: one `<pkg>.<Type>.<Field> <tag>` per line,
// blank lines and #-comments ignored. A malformed or repeated row is an error.
func parseNotPaths(body string) (map[string]ledgerRow, []string) {
	out := map[string]ledgerRow{}
	var errs []string
	sc := bufio.NewScanner(strings.NewReader(body))
	for line := 1; sc.Scan(); line++ {
		text := strings.TrimSpace(sc.Text())
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		fields := strings.Fields(text)
		if len(fields) != 2 || strings.Count(fields[0], ".") != 2 {
			errs = append(errs, fmt.Sprintf("%s:%d: %q is not `<pkg>.<Type>.<Field> <tag>`", notPathsLedger, line, text))
			continue
		}
		if prev, dup := out[fields[0]]; dup {
			errs = append(errs, fmt.Sprintf("%s:%d: %s is already filed at line %d", notPathsLedger, line, fields[0], prev.Line))
			continue
		}
		out[fields[0]] = ledgerRow{Tag: fields[1], Line: line}
	}
	return out, errs
}

// classifyFields is the two-way check. withStale also reports registry entries
// and ledger rows naming fields the enumeration did not find — off for a
// fixture, which holds only a slice of the tree.
func classifyFields(found map[string]serializedField, registry []pathfence.Field, ledger map[string]ledgerRow, withStale bool) []string {
	declared := map[string]string{}
	for _, f := range registry {
		declared[f.Name()] = f.Tag
	}
	var problems []string
	for name, f := range found {
		regTag, inReg := declared[name]
		row, inLedger := ledger[name]
		switch {
		case inReg && inLedger:
			problems = append(problems, fmt.Sprintf("%s is double-declared: pathfence.Fields() calls it a path and %s:%d says it is not — keep one", name, notPathsLedger, row.Line))
		case !inReg && !inLedger:
			problems = append(problems, fmt.Sprintf("undeclared serialized string field %s (tag %q, %s:%d): declare it in "+
				"pathfence.Fields() if it holds a path, or paste this row into %s if it does not:\n    %s %s",
				name, f.Tag, f.Pos.Filename, f.Pos.Line, notPathsLedger, name, f.Tag))
		case inReg && regTag != f.Tag:
			problems = append(problems, fmt.Sprintf("pathfence.Fields() records %s with tag %q but the struct tag is %q", name, regTag, f.Tag))
		case inLedger && row.Tag != f.Tag:
			problems = append(problems, fmt.Sprintf("%s:%d records %s with tag %q but the struct tag is %q", notPathsLedger, row.Line, name, row.Tag, f.Tag))
		}
	}
	if withStale {
		for name, tag := range declared {
			if _, ok := found[name]; !ok {
				problems = append(problems, fmt.Sprintf("stale declaration %s (tag %q): pathfence.Fields() names a struct "+
					"or field that no longer exists, so its promise is about nothing", name, tag))
			}
		}
		for name, row := range ledger {
			if _, ok := found[name]; !ok {
				problems = append(problems, fmt.Sprintf("stale ledger row %s:%d: %s no longer exists — delete the row", notPathsLedger, row.Line, name))
			}
		}
	}
	sort.Strings(problems)
	return problems
}

// readNotPaths loads and parses the ledger, failing the test on a malformed row.
func readNotPaths(t *testing.T) map[string]ledgerRow {
	t.Helper()
	body, err := os.ReadFile(notPathsLedger)
	if err != nil {
		t.Fatal(err)
	}
	rows, errs := parseNotPaths(string(body))
	for _, e := range errs {
		t.Error(e)
	}
	return rows
}

// TestEveryPathShapedFieldIsDeclared is the two-way check over the live tree:
// every serialized string field is filed exactly once, and every filing names
// a field that exists.
func TestEveryPathShapedFieldIsDeclared(t *testing.T) {
	found, collisions := serializedStringFields(liveView(t))
	for _, c := range collisions {
		t.Errorf("enumeration key collision: %s — the registry cannot tell them apart", c)
	}
	for _, p := range classifyFields(found, pathfence.Fields(), readNotPaths(t), true) {
		t.Error(p)
	}
}

// TestWalkerFindsANonEmptySet is the vacuity guard. A walker that silently
// stopped matching would make the two-way check pass by finding nothing on one
// side and reporting every filing as stale on the other — this fails first and
// says why.
func TestWalkerFindsANonEmptySet(t *testing.T) {
	found, _ := serializedStringFields(liveView(t))
	if err := serializedFieldFloorErr(len(found)); err != nil {
		t.Fatal(err)
	}
	if serializedFieldFloorErr(serializedFieldFloor) != nil || serializedFieldFloorErr(serializedFieldFloor-1) == nil {
		t.Error("the floor must pass at its minimum and fail one under it")
	}
	// The field the 2026-09-07 experiment was about, and one outside the
	// four schema directories the old walker was limited to.
	for _, name := range []string{"phase.Task.Files", "localstore.DetachedRun.RunDir", "survivor.Acceptance.File"} {
		if _, ok := found[name]; !ok {
			t.Errorf("the walk does not enumerate %s", name)
		}
	}
}

func serializedFieldFloorErr(n int) error {
	if n < serializedFieldFloor {
		return fmt.Errorf("the walker found only %d serialized string fields, under the floor of %d — "+
			"it has stopped seeing the schemas, and every assertion resting on it is vacuous", n, serializedFieldFloor)
	}
	return nil
}

// TestWalkerSelfTest drives the walker over a fixture whose answer is written
// down here. It pins the rules at once:
//
//   - EVERY tagged string field is found — replay, a tag no vocabulary listed;
//   - tag options are stripped, and a json-only tag counts;
//   - the type test sees through names, slices and pointers — a `type RelPath
//     string`, a []RelPath and a *string are all found;
//   - a bool tagged "public", an int, a struct, an untagged field and a "-"
//     tag are not;
//   - the package is found wherever it lives: this one is named remote, which
//     no schema-directory list ever held.
func TestWalkerSelfTest(t *testing.T) {
	const src = `package remote

type RelPath string

type Pin struct {
	Doc     string            ` + "`toml:\"doc\"`" + `
	Replay  string            ` + "`toml:\"replay\"`" + `
	Files   []string          ` + "`toml:\"files,omitempty\"`" + `
	Rel     RelPath           ` + "`toml:\"rel\"`" + `
	Rels    []RelPath         ` + "`toml:\"rels\"`" + `
	Ptr     *string           ` + "`toml:\"ptr\"`" + `
	JSONed  string            ` + "`json:\"path,omitempty\"`" + `
	Public  bool              ` + "`toml:\"public\"`" + `
	Count   int               ` + "`toml:\"files\"`" + `
	Nested  struct{}          ` + "`toml:\"source\"`" + `
	ByPath  map[string]string ` + "`toml:\"by_path\"`" + `
	Skipped string
	Dashed  string            ` + "`toml:\"-\"`" + `
}
`
	fx, err := typecheckFixture(liveImportable(t), []fixtureSource{{Name: "walker.go", Src: []byte(src)}})
	if err != nil {
		t.Fatal(err)
	}
	found, _ := serializedStringFields(fx.srcView)
	got := map[string]string{}
	for name, f := range found {
		got[name] = f.Tag
	}
	want := map[string]string{
		"remote.Pin.Doc":    "doc",
		"remote.Pin.Replay": "replay",
		"remote.Pin.Files":  "files",
		"remote.Pin.Rel":    "rel",
		"remote.Pin.Rels":   "rels",
		"remote.Pin.Ptr":    "ptr",
		"remote.Pin.JSONed": "path",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("walker self-test:\n got %v\nwant %v", got, want)
	}
	// Undeclared, every one of them is reported — outside the old schema dirs.
	if p := classifyFields(found, nil, nil, false); len(p) != len(want) {
		t.Errorf("classifyFields over the fixture = %d problems, want %d:\n%s", len(p), len(want), strings.Join(p, "\n"))
	}
}

// TestTaskExperimentStaysAFailure keeps the 2026-09-07 experiment as a
// must-fail fixture: phase.Task with three new string fields under tag words
// the old vocabulary never held yields exactly three undeclared findings, one
// naming each; the same struct without them yields none.
func TestTaskExperimentStaysAFailure(t *testing.T) {
	path := fixturePath("pathfence_scan", "task_experiment.go.txt")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	ledger := readNotPaths(t)
	run := func(src string) []string {
		fx, err := typecheckFixture(liveImportable(t), []fixtureSource{{Name: "task_experiment.go.txt", Src: []byte(src)}})
		if err != nil {
			t.Fatal(err)
		}
		found, _ := serializedStringFields(fx.srcView)
		return classifyFields(found, pathfence.Fields(), ledger, false)
	}

	got := run(string(body))
	if len(got) != 3 {
		t.Fatalf("the experiment yields %d findings, want 3:\n%s", len(got), strings.Join(got, "\n"))
	}
	for _, f := range []string{"phase.Task.WorkDir", "phase.Task.OutputDir", "phase.Task.Spec"} {
		named := false
		for _, p := range got {
			if strings.Contains(p, "undeclared serialized string field "+f+" ") {
				named = true
			}
		}
		if !named {
			t.Errorf("no finding names %s:\n%s", f, strings.Join(got, "\n"))
		}
	}

	var control []string
	for _, line := range strings.Split(string(body), "\n") {
		if strings.Contains(line, "// EXPERIMENT") {
			continue
		}
		control = append(control, line)
	}
	if p := run(strings.Join(control, "\n")); len(p) != 0 {
		t.Errorf("phase.Task without the experiment's fields still yields findings:\n%s", strings.Join(p, "\n"))
	}
}

// TestFieldLedgerRotIsCaught: a ledger row whose field vanished is stale, a
// field filed in both places is double-declared, a malformed or repeated row is
// an error, and a ledger tag that drifted from the struct tag is reported.
func TestFieldLedgerRotIsCaught(t *testing.T) {
	found := map[string]serializedField{
		"x.T.Kept":  {Tag: "kept"},
		"x.T.Both":  {Tag: "both"},
		"x.T.Moved": {Tag: "moved"},
	}
	registry := []pathfence.Field{{Struct: "x.T", Field: "Both", Tag: "both"}}
	ledger, errs := parseNotPaths("# comment\nx.T.Kept kept\nx.T.Both both\nx.T.Gone gone\nx.T.Moved old_tag\n")
	if len(errs) != 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	got := strings.Join(classifyFields(found, registry, ledger, true), "\n")
	for _, want := range []string{
		"x.T.Both is double-declared",
		"stale ledger row " + notPathsLedger + ":4: x.T.Gone",
		`records x.T.Moved with tag "old_tag" but the struct tag is "moved"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("rot report lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "x.T.Kept") {
		t.Errorf("a correctly filed row was reported:\n%s", got)
	}
	if _, errs := parseNotPaths("x.T.A a\nx.T.A a\nnot-a-row\n"); len(errs) != 2 {
		t.Errorf("parseNotPaths reported %d errors for a repeated and a malformed row, want 2: %v", len(errs), errs)
	}
}
