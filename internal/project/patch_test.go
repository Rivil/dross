package project

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

// The patcher's contract is byte-level: every test below either pins an
// index span or applies an op to a document and asserts that the set of
// changed lines is exactly the targeted ones. mustApply additionally proves
// every patched document still decodes and that the op's value is what the
// decoder now sees at the op's path — a splice that looks right but does not
// parse is the failure mode a text patcher invites.

func fixture(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "lossless.toml"))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func mustIndex(t *testing.T, src []byte) *doc {
	t.Helper()
	d, err := indexDoc(src)
	if err != nil {
		t.Fatalf("indexDoc: %v", err)
	}
	return d
}

func keyLineFor(t *testing.T, d *doc, table []string, key string) (int, *keyLine) {
	t.Helper()
	for i, ln := range d.lines {
		if ln.kind == lineKey && ln.key.key == key && reflect.DeepEqual(segNames(ln.key.table), table) {
			return i, ln.key
		}
	}
	t.Fatalf("no key line for %s.%s", strings.Join(table, "."), key)
	return -1, nil
}

func splitLines(b []byte) []string {
	s := strings.TrimSuffix(string(b), "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// changedLines is a multiset diff of before vs after: the lines added and
// the lines removed, ignoring position. A one-line change shows up as one
// removed and one added; a byte-identical document as none of either.
func changedLines(before, after []byte) (added, removed []string) {
	count := map[string]int{}
	for _, l := range splitLines(before) {
		count[l]--
	}
	for _, l := range splitLines(after) {
		count[l]++
	}
	for _, l := range splitLines(after) {
		if count[l] > 0 {
			added = append(added, l)
			count[l]--
		}
	}
	for _, l := range splitLines(before) {
		if count[l] < 0 {
			removed = append(removed, l)
			count[l]++
		}
	}
	return added, removed
}

func assertChanged(t *testing.T, before, after []byte, wantAdded, wantRemoved []string) {
	t.Helper()
	added, removed := changedLines(before, after)
	if !reflect.DeepEqual(added, wantAdded) {
		t.Errorf("added lines:\n got %q\nwant %q", added, wantAdded)
	}
	if !reflect.DeepEqual(removed, wantRemoved) {
		t.Errorf("removed lines:\n got %q\nwant %q", removed, wantRemoved)
	}
}

// normalize round-trips v through the encoder and decoder so a typed Go
// value compares equal to what toml.Decode produces for the same TOML.
func normalize(t *testing.T, v any) any {
	t.Helper()
	if b, ok := v.(block); ok {
		m := map[string]any{}
		for _, e := range b {
			m[e.key] = e.value
		}
		v = m
	}
	out, err := encodeFresh(map[string]any{"k": v})
	if err != nil {
		t.Fatalf("normalize %v: %v", v, err)
	}
	var m map[string]any
	if _, err := toml.Decode(string(out), &m); err != nil {
		t.Fatalf("normalize decode: %v", err)
	}
	return m["k"]
}

func lookup(tree map[string]any, table []string, elem int, key string) (any, bool) {
	var cur any = tree
	for i, name := range table {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		if cur, ok = m[name]; !ok {
			return nil, false
		}
		if i == len(table)-1 && elem >= 0 {
			arr, ok := cur.([]map[string]any)
			if !ok || elem >= len(arr) {
				return nil, false
			}
			cur = arr[elem]
		}
	}
	if key == "" {
		return cur, true
	}
	m, ok := cur.(map[string]any)
	if !ok {
		return nil, false
	}
	v, ok := m[key]
	return v, ok
}

func decodeTree(t *testing.T, src []byte) map[string]any {
	t.Helper()
	var tree map[string]any
	if _, err := toml.Decode(string(src), &tree); err != nil {
		t.Fatalf("patched document no longer decodes: %v\n%s", err, src)
	}
	return tree
}

// mustApply applies ops and proves the result decodes with every op's effect
// visible at its path.
func mustApply(t *testing.T, src []byte, ops ...op) []byte {
	t.Helper()
	before := decodeTree(t, src)
	out, err := apply(src, ops)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	after := decodeTree(t, out)
	for _, o := range ops {
		switch o.kind {
		case opDeleteElem:
			b, _ := lookup(before, o.table, -1, "")
			a, _ := lookup(after, o.table, -1, "")
			bl, _ := b.([]map[string]any)
			al, _ := a.([]map[string]any)
			if len(al) != len(bl)-1 {
				t.Errorf("%s: %d elements before, %d after", o, len(bl), len(al))
			}
		case opAppendElem:
			a, _ := lookup(after, o.table, -1, "")
			al, _ := a.([]map[string]any)
			if len(al) == 0 {
				t.Fatalf("%s: no elements after append", o)
			}
			if got, want := al[len(al)-1], normalize(t, o.value); !reflect.DeepEqual(got, want) {
				t.Errorf("%s: last element\n got %#v\nwant %#v", o, got, want)
			}
		default:
			got, ok := lookup(after, o.table, o.elem, o.key)
			if o.value == nil {
				if ok {
					t.Errorf("%s: key still decodes as %#v", o, got)
				}
				continue
			}
			if !ok {
				t.Errorf("%s: key absent after apply\n%s", o, out)
				continue
			}
			if want := normalize(t, o.value); !reflect.DeepEqual(got, want) {
				t.Errorf("%s:\n got %#v\nwant %#v", o, got, want)
			}
		}
	}
	return out
}

func set(table string, key string, value any) op {
	return op{kind: opKey, table: strings.Split(table, "."), elem: -1, key: key, value: value}
}

func del(table string, key string) op {
	return set(table, key, nil)
}

// --- index -----------------------------------------------------------------

func TestIndexValueSpanIgnoresHashInsideStrings(t *testing.T) {
	src := []byte("[t]\ndesc = \"a # b\"  # real\n")
	d := mustIndex(t, src)
	_, kl := keyLineFor(t, d, []string{"t"}, "desc")
	if got := string(src[kl.valStart:kl.valEnd]); got != `"a # b"` {
		t.Errorf("value span = %q, want %q", got, `"a # b"`)
	}
	if got := string(src[kl.valEnd:d.lines[1].end]); got != "  # real" {
		t.Errorf("trailing = %q, want %q", got, "  # real")
	}
}

func TestIndexMultilineArraySpan(t *testing.T) {
	src := []byte("[goals]\nnon_goals = [\n \"a\", # c\n \"b\",\n]\nafter = 1\n")
	d := mustIndex(t, src)
	i, kl := keyLineFor(t, d, []string{"goals"}, "non_goals")
	if i != 1 || kl.endLine != 4 {
		t.Fatalf("non_goals spans lines %d..%d, want 1..4", i, kl.endLine)
	}
	for j := 2; j <= 4; j++ {
		if d.lines[j].kind != lineCont {
			t.Errorf("line %d kind = %v, want continuation", j, d.lines[j].kind)
		}
	}
	if got := string(src[kl.valStart:kl.valEnd]); !strings.HasPrefix(got, "[") || !strings.HasSuffix(got, "]") {
		t.Errorf("value span = %q", got)
	}
	if j, _ := keyLineFor(t, d, []string{"goals"}, "after"); j != 5 {
		t.Errorf("after indexed at line %d, want 5", j)
	}
}

func TestIndexMultilineBasicString(t *testing.T) {
	src := []byte("[t]\ns = \"\"\"\nhas ] and = inside\n\"\"\"\nnext = 1\n")
	d := mustIndex(t, src)
	_, kl := keyLineFor(t, d, []string{"t"}, "s")
	if kl.endLine != 3 {
		t.Errorf("s ends on line %d, want 3", kl.endLine)
	}
	if got := string(src[kl.valStart:kl.valEnd]); got != "\"\"\"\nhas ] and = inside\n\"\"\"" {
		t.Errorf("value span = %q", got)
	}
	if j, _ := keyLineFor(t, d, []string{"t"}, "next"); j != 4 {
		t.Errorf("next indexed at line %d, want 4", j)
	}
}

func TestIndexArrayOfTablesElementIndex(t *testing.T) {
	d := mustIndex(t, fixture(t))
	var locked, lanes, comps []int
	for _, ln := range d.lines {
		if ln.kind != lineHeader || !ln.hdr.array {
			continue
		}
		p := ln.hdr.path
		switch strings.Join(segNames(p), ".") {
		case "stack.locked":
			locked = append(locked, p[len(p)-1].elem)
		case "runtime.test_lane":
			lanes = append(lanes, p[len(p)-1].elem)
		case "competition":
			comps = append(comps, p[len(p)-1].elem)
		}
	}
	if !reflect.DeepEqual(locked, []int{0, 1, 2, 3}) {
		t.Errorf("stack.locked elems = %v", locked)
	}
	if !reflect.DeepEqual(lanes, []int{0, 1, 2}) {
		t.Errorf("runtime.test_lane elems = %v", lanes)
	}
	if !reflect.DeepEqual(comps, []int{0, 1}) {
		t.Errorf("competition elems = %v", comps)
	}
	// Keys under the third block carry its index, so they can never be
	// mistaken for the first block's.
	_, kl := keyLineFor(t, d, []string{"stack", "locked"}, "choice")
	if kl.table[1].elem != 0 {
		t.Errorf("first choice under elem %d", kl.table[1].elem)
	}
}

func TestIndexRecordsCRLF(t *testing.T) {
	src := []byte("[t]\r\n  k = \"v\"  # c\r\n")
	d := mustIndex(t, src)
	if d.eol != "\r\n" {
		t.Fatalf("eol = %q, want CRLF", d.eol)
	}
	_, kl := keyLineFor(t, d, []string{"t"}, "k")
	if got := string(src[kl.valStart:kl.valEnd]); got != `"v"` {
		t.Errorf("value span = %q", got)
	}
	if got := string(src[d.lines[1].start:d.lines[1].end]); strings.Contains(got, "\r") {
		t.Errorf("line content includes CR: %q", got)
	}
}

func TestIndexQuotedAndDottedKeys(t *testing.T) {
	src := []byte("[board]\n  \"fix versions\" = \"x\"\n  state_map.planned = \"y\"\n")
	d := mustIndex(t, src)
	keyLineFor(t, d, []string{"board"}, "fix versions")
	_, kl := keyLineFor(t, d, []string{"board", "state_map"}, "planned")
	if got := string(src[kl.keyStart:kl.lastKeyStart]); got != "state_map." {
		t.Errorf("dotted prefix = %q, want %q", got, "state_map.")
	}
}

// --- apply -----------------------------------------------------------------

func TestApplyReplaceKeepsIndentAndTrailingComment(t *testing.T) {
	src := fixture(t)
	out := mustApply(t, src, set("project", "version", "1.7.11.0"))
	assertChanged(t, src, out,
		[]string{`  version = "1.7.11.0"  # bumped`},
		[]string{`  version = "1.7.5.0"  # bumped`})
}

func TestApplyMultilineArrayReplacedWhole(t *testing.T) {
	src := fixture(t)
	out := mustApply(t, src, set("goals", "non_goals", []any{"x", "y"}))
	assertChanged(t, src, out,
		[]string{`  non_goals = ["x", "y"]`},
		[]string{
			`  non_goals = [`,
			`    "Not a general-purpose tool", # marketing is out of scope`,
			`    "No feature PRs that don't match the author's workflow",`,
			`  ]`,
		})
}

func TestApplyInsertStaysInsideTableSpan(t *testing.T) {
	src := fixture(t)
	out := mustApply(t, src, set("stack", "profile", "go"))
	assertChanged(t, src, out, []string{`  profile = "go"`}, nil)
	lines := splitLines(out)
	for i, l := range lines {
		if l == `  profile = "go"` {
			if lines[i-1] != `  languages = ["go"]` {
				t.Errorf("profile inserted after %q, want after languages", lines[i-1])
			}
			return
		}
	}
	t.Fatal("profile line not found")
}

func TestApplyAddKeyMatchesSiblingIndent(t *testing.T) {
	src := fixture(t)
	out := mustApply(t, src, set("paths", "source", "internal"))
	assertChanged(t, src, out, []string{`  source = "internal"`}, nil)
	if !bytes.Contains(out, []byte("[paths]\n  source = \"internal\"\n\n[env]\n")) {
		t.Errorf("paths block:\n%s", out)
	}
}

// TestApplyNewSubtableMatchesEncoderLayout uses the repo's own project.toml
// shape — [board] with direct keys only, then [paths] — and requires the new
// sub-table to land where and how the encoder would have written it.
func TestApplyNewSubtableMatchesEncoderLayout(t *testing.T) {
	src := []byte(`[board]
  provider = "youtrack"
  enabled = true
  milestone_mode = "epic"

[paths]

[env]
`)
	out := mustApply(t, src, set("board.state_map", "planned", "Open"))
	want := "  milestone_mode = \"epic\"\n\n  [board.state_map]\n    planned = \"Open\"\n\n[paths]\n"
	if !bytes.Contains(out, []byte(want)) {
		t.Errorf("layout:\n%s", out)
	}
	// The same two lines are what a fresh encode of the same struct emits.
	enc, err := encodeFresh(&Project{Board: Board{Provider: "youtrack", StateMap: map[string]string{"planned": "Open"}}})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(enc, []byte("\n  [board.state_map]\n    planned = \"Open\"\n")) {
		t.Errorf("encoder layout drifted:\n%s", enc)
	}
}

func TestApplyTwoOpsSameTableBothLand(t *testing.T) {
	src := fixture(t)
	out := mustApply(t, src, set("paths", "source", "internal"), set("paths", "tests", "internal"))
	assertChanged(t, src, out, []string{`  source = "internal"`, `  tests = "internal"`}, nil)
	if !bytes.Contains(out, []byte("[paths]\n  source = \"internal\"\n  tests = \"internal\"\n\n[env]\n")) {
		t.Errorf("paths block:\n%s", out)
	}
}

func TestApplyDeleteLeavesNeighbourComment(t *testing.T) {
	src := fixture(t)
	out := mustApply(t, src, del("project", "description"))
	assertChanged(t, src, out, nil, []string{`  description = "a # b"  # real`})
	if !bytes.Contains(out, []byte("  # what the project is for\n  created = ")) {
		t.Errorf("neighbour comment lost:\n%s", out)
	}
}

func TestApplyDeleteLastKeyRemovesEmptyHeader(t *testing.T) {
	src := fixture(t)
	out := mustApply(t, src, del("board.state_map", "planned"))
	assertChanged(t, src, out, nil, []string{``, `  [board.state_map]`, `    planned = "Open"`})
	if !bytes.Contains(out, []byte("  milestone_mode = \"epic\"\n\n[paths]\n")) {
		t.Errorf("board region:\n%s", out)
	}
}

func TestApplyKeepsHeaderThatHoldsAComment(t *testing.T) {
	src := []byte("[board]\n  enabled = true\n\n  [board.state_map]\n    # states we map\n    planned = \"Open\"\n\n[paths]\n")
	out := mustApply(t, src, del("board.state_map", "planned"))
	assertChanged(t, src, out, nil, []string{`    planned = "Open"`})
}

func TestApplyKeepsHeaderThatHoldsUnmodeledKey(t *testing.T) {
	src := []byte("[board]\n  enabled = true\n\n  [board.state_map]\n    planned = \"Open\"\n    custom = \"x\"\n\n[paths]\n")
	out := mustApply(t, src, del("board.state_map", "planned"))
	assertChanged(t, src, out, nil, []string{`    planned = "Open"`})
	if !bytes.Contains(out, []byte("  [board.state_map]\n    custom = \"x\"\n")) {
		t.Errorf("unmodeled key or its header lost:\n%s", out)
	}
}

func TestApplyAppendArrayElementAfterLastSibling(t *testing.T) {
	src := fixture(t)
	o := op{kind: opAppendElem, table: []string{"stack", "locked"}, elem: -1, value: block{
		{"choice", "BurntSushi/toml for reads"},
		{"why", "already locked"},
		{"locked_at", "2026-09-13"},
	}}
	out := mustApply(t, src, o)
	assertChanged(t, src, out, []string{
		``,
		`  [[stack.locked]]`,
		`    choice = "BurntSushi/toml for reads"`,
		`    why = "already locked"`,
		`    locked_at = "2026-09-13"`,
	}, nil)
	want := "    why = \"CLI provides state; markdown prompts provide orchestration.\"\n    locked_at = \"2026-06-19\"\n\n  [[stack.locked]]\n    choice = \"BurntSushi/toml for reads\"\n    why = \"already locked\"\n    locked_at = \"2026-09-13\"\n\n[runtime]\n"
	if !bytes.Contains(out, []byte(want)) {
		t.Errorf("fifth block placement:\n%s", out)
	}
}

func TestApplyDeleteArrayElementKeepsSiblings(t *testing.T) {
	src := fixture(t)
	out := mustApply(t, src, op{kind: opDeleteElem, table: []string{"competition"}, elem: 0})
	assertChanged(t, src, out, nil, []string{``, `[[competition]]`, `  name = "one"`, `  url = "https://one.invalid"`})
	if !bytes.Contains(out, []byte("[custom]\n  anything = 1\n\n[[competition]]\n  name = \"two\"\n  note = \"hand-added\"\n")) {
		t.Errorf("sibling block:\n%s", out)
	}
	// Deleting the middle of three lanes: neighbours byte-identical, the
	// survivor's comment intact.
	out = mustApply(t, src, op{kind: opDeleteElem, table: []string{"runtime", "test_lane"}, elem: 1})
	assertChanged(t, src, out, nil, []string{
		``,
		`  [[runtime.test_lane]]`,
		`    # the project package is the fast lane`,
		`    name = "project"`,
		`    match = ["internal/project/**"]`,
		`    command = "go test ./internal/project/..."`,
	})
}

func TestRenderValueMatchesEncoder(t *testing.T) {
	for _, v := range []any{
		"a\"b\\c\n",
		"plain",
		[]any{"x", "y"},
		[]string{},
		int64(3),
		true,
		1.5,
	} {
		var buf bytes.Buffer
		if err := toml.NewEncoder(&buf).Encode(map[string]any{"k": v}); err != nil {
			t.Fatal(err)
		}
		want := strings.TrimSuffix(strings.TrimPrefix(buf.String(), "k = "), "\n")
		got, err := renderValue(v)
		if err != nil {
			t.Fatalf("renderValue(%#v): %v", v, err)
		}
		if got != want {
			t.Errorf("renderValue(%#v) = %q, want %q", v, got, want)
		}
	}
	if _, err := renderValue(map[string]any{"a": 1}); err == nil {
		t.Error("a map rendered as a single value")
	}
}

func TestApplyPreservesCRLF(t *testing.T) {
	src := []byte("[project]\r\n  version = \"1\"\r\n\r\n[paths]\r\n")
	out := mustApply(t, src,
		set("project", "version", "2"),
		set("paths", "source", "src"),
		set("board.state_map", "planned", "Open"),
	)
	if crlf, lf := bytes.Count(out, []byte("\r\n")), bytes.Count(out, []byte("\n")); crlf != lf {
		t.Errorf("%d CRLF vs %d LF line endings:\n%q", crlf, lf, out)
	}
	want := "[project]\r\n  version = \"2\"\r\n\r\n[paths]\r\n  source = \"src\"\r\n\r\n  [board.state_map]\r\n    planned = \"Open\"\r\n"
	if string(out) != want {
		t.Errorf("got:\n%q\nwant:\n%q", out, want)
	}
}

func TestApplyEOFNewline(t *testing.T) {
	src := []byte("[a]\nk = 1")
	out := mustApply(t, src, set("b", "x", "y"))
	if want := "[a]\nk = 1\n\n[b]\nx = \"y\"\n"; string(out) != want {
		t.Errorf("got:\n%q\nwant:\n%q", out, want)
	}
	// An empty file gets no leading blank line.
	out = mustApply(t, nil, set("project", "name", "n"))
	if want := "[project]\n  name = \"n\"\n"; string(out) != want {
		t.Errorf("got:\n%q\nwant:\n%q", out, want)
	}
}

func TestApplyRefusesInlineTableByLine(t *testing.T) {
	src := []byte("[board]\n  provider = \"x\"\n  state_map = { planned = \"Open\" }\n")
	for _, o := range []op{
		set("board.state_map", "uat", "UAT"),
		set("board", "state_map", "flat"),
		del("board.state_map", "planned"),
	} {
		out, err := apply(src, []op{o})
		if err == nil {
			t.Fatalf("%s: inline table was patched:\n%s", o, out)
		}
		for _, want := range []string{"board.state_map", "line 3", "[board.state_map]"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("%s: error %q lacks %q", o, err, want)
			}
		}
		if !bytes.Equal(out, src) {
			t.Errorf("%s: returned bytes differ from input", o)
		}
	}
	// An array of inline tables is refused the same way for element ops.
	src = []byte("[stack]\n  locked = [{ choice = \"a\", why = \"b\", locked_at = \"c\" }]\n")
	if out, err := apply(src, []op{{kind: opAppendElem, table: []string{"stack", "locked"}, elem: -1, value: block{{"choice", "x"}}}}); err == nil {
		t.Errorf("array of inline tables was patched:\n%s", out)
	}
}

// TestFixtureLoads keeps the fixture honest: it has to be a project.toml Load
// accepts, or the lossless tests prove things about a file dross would refuse.
func TestFixtureLoads(t *testing.T) {
	p, err := Load(filepath.Join("testdata", "lossless.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Stack.Locked) != 4 || len(p.Runtime.TestLane) != 3 || len(p.Competition) != 2 {
		t.Errorf("fixture decoded unexpectedly: %d locked, %d lanes, %d competitors",
			len(p.Stack.Locked), len(p.Runtime.TestLane), len(p.Competition))
	}
	if p.Board.StateMap["planned"] != "Open" {
		t.Errorf("state_map = %v", p.Board.StateMap)
	}
}
