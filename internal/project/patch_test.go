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

// --- coverage the first verify found missing (t-7) --------------------------
//
// Every test below targets a path the 2026-09-13 mutation run reported NOT
// COVERED: shapes dross's own encoder never writes but a hand-edited file can
// carry, and the refusals that keep a shape the patcher does not follow from
// being guessed at. Each one asserts the span, the decoded name or the exact
// changed lines, never just "no error".

func TestIndexLiteralStrings(t *testing.T) {
	src := []byte("[t]\n" +
		"p = 'C:\\path # not a comment'  # real\n" +
		"m = '''\nline ] one\nline = two\n'''\n" +
		"tail = '''x''''\n" +
		"next = 1\n")
	d := mustIndex(t, src)

	_, p := keyLineFor(t, d, []string{"t"}, "p")
	if got := string(src[p.valStart:p.valEnd]); got != `'C:\path # not a comment'` {
		t.Errorf("literal span = %q", got)
	}
	if got := string(src[p.valEnd:d.lines[1].end]); got != "  # real" {
		t.Errorf("trailing after literal = %q", got)
	}

	i, m := keyLineFor(t, d, []string{"t"}, "m")
	if i != 2 || m.endLine != 5 {
		t.Errorf("multi-line literal spans %d..%d, want 2..5", i, m.endLine)
	}
	if got := string(src[m.valStart:m.valEnd]); got != "'''\nline ] one\nline = two\n'''" {
		t.Errorf("multi-line literal span = %q", got)
	}

	// A fourth quote belongs to the string, not the delimiter.
	_, tail := keyLineFor(t, d, []string{"t"}, "tail")
	if got := string(src[tail.valStart:tail.valEnd]); got != "'''x''''" {
		t.Errorf("four-quote tail span = %q", got)
	}
	if j, _ := keyLineFor(t, d, []string{"t"}, "next"); j != 7 {
		t.Errorf("next indexed at line %d, want 7", j)
	}
	tree := decodeTree(t, src)
	if got, _ := lookup(tree, []string{"t"}, -1, "tail"); got != "x'" {
		t.Errorf("decoder reads tail as %q, want %q — the span disagrees with TOML", got, "x'")
	}

	for name, bad := range map[string]string{
		"unterminated literal":            "[t]\np = 'open\n",
		"unterminated literal at EOF":     "[t]\np = 'open",
		"unterminated multi-line literal": "[t]\nm = '''\nnever closed\n",
	} {
		if _, err := indexDoc([]byte(bad)); err == nil || !strings.Contains(err.Error(), "unterminated") {
			t.Errorf("%s: err = %v, want an unterminated refusal", name, err)
		}
	}
}

func TestIndexMultilineBasicEscapesAndExtraQuotes(t *testing.T) {
	// A \" inside must not close the string, and one extra quote before the
	// delimiter is content.
	src := []byte("[t]\ns = \"\"\"a \\\" b\"\"\"\"\nnext = 1\n")
	d := mustIndex(t, src)
	_, kl := keyLineFor(t, d, []string{"t"}, "s")
	if got := string(src[kl.valStart:kl.valEnd]); got != "\"\"\"a \\\" b\"\"\"\"" {
		t.Errorf("span = %q", got)
	}
	tree := decodeTree(t, src)
	if got, _ := lookup(tree, []string{"t"}, -1, "s"); got != `a " b"` {
		t.Errorf("decoder reads s as %q", got)
	}
	if _, err := indexDoc([]byte("[t]\ns = \"\"\"never\n")); err == nil || !strings.Contains(err.Error(), "unterminated") {
		t.Errorf("unterminated multi-line basic: err = %v", err)
	}
}

func TestIndexQuotedKeyEscapes(t *testing.T) {
	src := []byte("[t]\n" +
		"\"a\\\"b\" = 1\n" +
		"\"t\\tab\" = 2\n" +
		"\"e\\u00e9\" = 3\n" +
		"\"g\\U0001F600\" = 4\n" +
		"\"bs\\\\\" = 5\n" +
		"\"ctl\\b\\f\\n\\r\" = 6\n")
	d := mustIndex(t, src)
	for _, want := range []string{"a\"b", "t\tab", "e\u00e9", "g\U0001F600", "bs\\", "ctl\b\f\n\r"} {
		keyLineFor(t, d, []string{"t"}, want)
	}
	// The index agrees with the decoder on every one of them.
	tree := decodeTree(t, src)
	for _, want := range []string{"a\"b", "t\tab", "e\u00e9", "g\U0001F600", "bs\\", "ctl\b\f\n\r"} {
		if _, ok := lookup(tree, []string{"t"}, -1, want); !ok {
			t.Errorf("decoder has no key %q", want)
		}
	}

	for name, tc := range map[string]struct{ src, want string }{
		"unknown escape":      {"[t]\n\"x\\q\" = 1\n", "unknown escape"},
		"short unicode":       {"[t]\n\"x\\u12\" = 1\n", "short unicode escape"},
		"bad unicode hex":     {"[t]\n\"x\\u12zz\" = 1\n", "bad unicode escape"},
		"short long unicode":  {"[t]\n\"x\\U0001F6\" = 1\n", "short unicode escape"},
		"unterminated quoted": {"[t]\n\"x = 1\n", "unterminated"},
	} {
		_, err := indexDoc([]byte(tc.src))
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want %q", name, err, tc.want)
		}
	}
	// A dangling backslash cannot survive scanBasic, so unescapeBasic is
	// asked directly.
	if _, err := unescapeBasic(`x\`); err == nil || !strings.Contains(err.Error(), "dangling escape") {
		t.Errorf("dangling escape: err = %v", err)
	}
	if got, err := unescapeBasic("plain"); err != nil || got != "plain" {
		t.Errorf("no-escape fast path = %q, %v", got, err)
	}
}

func TestIndexLiteralKeyKeepsDots(t *testing.T) {
	src := []byte("[t]\n'a.b' = 1\n\"c.d\" . e = 2\n")
	d := mustIndex(t, src)
	keyLineFor(t, d, []string{"t"}, "a.b")
	_, kl := keyLineFor(t, d, []string{"t", "c.d"}, "e")
	if got := string(src[kl.keyStart:kl.lastKeyStart]); got != "\"c.d\" . " {
		t.Errorf("dotted prefix with spaces = %q", got)
	}
	tree := decodeTree(t, src)
	if _, ok := lookup(tree, []string{"t"}, -1, "a.b"); !ok {
		t.Error("decoder has no key a.b")
	}
	if _, ok := lookup(tree, []string{"t", "c.d"}, -1, "e"); !ok {
		t.Error("decoder has no key c.d.e")
	}
}

func TestIndexRefusesMalformedLines(t *testing.T) {
	for name, tc := range map[string]struct{ src, want string }{
		"no key":                 {"[t]\n= 1\n", "expected a key"},
		"no equals":              {"[t]\nk 1\n", "expected '='"},
		"no value":               {"[t]\nk =\n", "missing value"},
		"no value at EOF":        {"[t]\nk = ", "missing value"},
		"unterminated string":    {"[t]\nk = \"open\n", "unterminated string"},
		"trailing junk":          {"[t]\nk = \"v\" x\n", "unexpected \"x\" after value"},
		"header unclosed":        {"[t\nk = 1\n", "expected ]"},
		"array header unclosed":  {"[[t]\nk = 1\n", "expected ]]"},
		"header trailing junk":   {"[t] x\n", "unexpected \"x\" after value"},
		"header bad key":         {"[\"x\\q\"]\nk = 1\n", "line 1: header: unknown escape"},
		"unterminated array":     {"[t]\nk = [1,\n", "unterminated [...]"},
		"unterminated inline":    {"[t]\nk = { a = 1\n", "unterminated {...}"},
		"line number in message": {"[t]\nok = 1\nbad\n", "line 3"},
	} {
		_, err := indexDoc([]byte(tc.src))
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want %q", name, err, tc.want)
		}
	}
}

func TestFirstBytesTruncates(t *testing.T) {
	if got := firstBytes([]byte("abc\ndef"), 12); got != "abc" {
		t.Errorf("newline: %q", got)
	}
	if got := firstBytes([]byte("abcdefghijklmnop"), 5); got != "abcde" {
		t.Errorf("length: %q", got)
	}
	if got := firstBytes([]byte("ab"), 5); got != "ab" {
		t.Errorf("short input: %q", got)
	}
}

func TestScanBracketedNested(t *testing.T) {
	src := []byte("[t]\narr = [ # opening comment ]\n  [\"a]\", 'b#'], # c ]\n  [1, [2, 3]],\n]\nnext = 1\n")
	d := mustIndex(t, src)
	i, kl := keyLineFor(t, d, []string{"t"}, "arr")
	if i != 1 || kl.endLine != 4 {
		t.Errorf("arr spans %d..%d, want 1..4", i, kl.endLine)
	}
	if got := string(src[kl.valStart:kl.valEnd]); !strings.HasSuffix(got, "\n]") || kl.inline {
		t.Errorf("span = %q inline = %v", got, kl.inline)
	}
	if j, _ := keyLineFor(t, d, []string{"t"}, "next"); j != 5 {
		t.Errorf("next at line %d, want 5", j)
	}
	// An inline table anywhere inside an array marks the key inline.
	d = mustIndex(t, []byte("[t]\narr = [ { a = 1 }, 2 ]\n"))
	if _, kl := keyLineFor(t, d, []string{"t"}, "arr"); !kl.inline {
		t.Error("array holding an inline table not flagged inline")
	}
	// A string inside the array that fails to scan is the array's error.
	if _, err := indexDoc([]byte("[t]\narr = [ \"open ]\n")); err == nil || !strings.Contains(err.Error(), "unterminated") {
		t.Errorf("bad string inside array: err = %v", err)
	}
}

func TestScanValueBareStopsBeforeCommentAndTabs(t *testing.T) {
	src := []byte("[t]\nn = 42\t \t# c\nb = true\nlast = 7")
	d := mustIndex(t, src)
	_, n := keyLineFor(t, d, []string{"t"}, "n")
	if got := string(src[n.valStart:n.valEnd]); got != "42" {
		t.Errorf("bare span = %q", got)
	}
	_, last := keyLineFor(t, d, []string{"t"}, "last")
	if got := string(src[last.valStart:last.valEnd]); got != "7" || d.finalNewline {
		t.Errorf("unterminated last value = %q finalNewline = %v", got, d.finalNewline)
	}
}

func TestApplySetDottedKeyMirrorsSpelling(t *testing.T) {
	src := []byte("[board]\n  provider = \"x\"\n  state_map.planned = \"Open\"\n  # after\n\n[paths]\n")
	out := mustApply(t, src, set("board.state_map", "uat", "UAT"))
	assertChanged(t, src, out, []string{`  state_map.uat = "UAT"`}, nil)
	if !bytes.Contains(out, []byte("  state_map.planned = \"Open\"\n  state_map.uat = \"UAT\"\n  # after\n")) {
		t.Errorf("dotted key did not land after its sibling:\n%s", out)
	}
	// Replacing through the dotted spelling touches only the value.
	out = mustApply(t, src, set("board.state_map", "planned", "Todo"))
	assertChanged(t, src, out, []string{`  state_map.planned = "Todo"`}, []string{`  state_map.planned = "Open"`})
}

func TestApplySetRootKey(t *testing.T) {
	root := func(key string, value any) op {
		return op{kind: opKey, table: nil, elem: -1, key: key, value: value}
	}
	src := []byte("# top\ntitle = \"x\"\n\n[project]\n  name = \"y\"\n")
	out := mustApply(t, src, root("author", "z"))
	assertChanged(t, src, out, []string{`author = "z"`}, nil)
	if !bytes.HasPrefix(out, []byte("# top\ntitle = \"x\"\nauthor = \"z\"\n\n[project]\n")) {
		t.Errorf("root key did not land after the last root key:\n%s", out)
	}

	// No root key yet: the key goes at the very top, ahead of every header.
	src = []byte("[project]\n  name = \"y\"\n")
	out = mustApply(t, src, root("title", "t"))
	if !bytes.HasPrefix(out, []byte("title = \"t\"\n[project]\n")) {
		t.Errorf("first root key did not land at the top:\n%s", out)
	}

	// An empty document.
	out = mustApply(t, nil, root("title", "t"))
	if string(out) != "title = \"t\"\n" {
		t.Errorf("empty doc = %q", out)
	}

	// Deleting a root key removes only its line.
	src = []byte("title = \"x\"\nauthor = \"z\"\n\n[project]\n  name = \"y\"\n")
	out = mustApply(t, src, root("author", nil))
	assertChanged(t, src, out, nil, []string{`author = "z"`})
}

func TestQualifyRefusals(t *testing.T) {
	src := []byte("[[a]]\n  k = 1\n\n  [a.sub]\n    s = 1\n\n[[a]]\n  k = 2\n\n[plain]\n  p = 1\n")
	for name, tc := range map[string]struct {
		o    op
		want string
	}{
		"table nested in array element": {op{kind: opKey, table: []string{"a", "sub"}, elem: -1, key: "s", value: 2}, "a table nested inside one of its elements"},
		"array op without element":      {op{kind: opKey, table: []string{"a"}, elem: -1, key: "k", value: 2}, "names no element"},
		"element out of range":          {op{kind: opKey, table: []string{"a"}, elem: 5, key: "k", value: 2}, "has 2 element(s), no element 5; append it instead"},
		"delete element out of range":   {op{kind: opDeleteElem, table: []string{"a"}, elem: 2}, "no element 2"},
		"delete absent key":             {op{kind: opKey, table: []string{"plain"}, elem: -1, key: "nope", value: nil}, "plain.nope is not present"},
		"append body not a block":       {op{kind: opAppendElem, table: []string{"a"}, elem: -1, value: "x"}, "must be a block or map[string]any"},
	} {
		out, err := apply(src, []op{tc.o})
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want %q", name, err, tc.want)
		}
		if !bytes.Equal(out, src) {
			t.Errorf("%s: bytes changed on a refused op", name)
		}
	}
	// An element the index says exists but no header carries is an index
	// bug, and setKey says so rather than inventing a header.
	d := mustIndex(t, src)
	if _, err := d.setKey([]seg{{"a", 0}, {"ghost", 0}}, "k", 1); err == nil || !strings.Contains(err.Error(), "has no header line") {
		t.Errorf("ghost element: err = %v", err)
	}
	if _, err := d.deleteElem([]seg{{"a", 7}}); err == nil || !strings.Contains(err.Error(), "has no header line") {
		t.Errorf("ghost delete: err = %v", err)
	}
}

func TestRemoveBlockBlankHandling(t *testing.T) {
	del := func(elem int) op { return op{kind: opDeleteElem, table: []string{"a"}, elem: elem} }

	// The first block of the file has no blank above it, so the blanks
	// below go with it and the next block becomes the first.
	src := []byte("[[a]]\n  k = 1\n\n\n[[a]]\n  k = 2\n")
	if out := mustApply(t, src, del(0)); string(out) != "[[a]]\n  k = 2\n" {
		t.Errorf("first block removal left:\n%q", out)
	}

	// A middle block takes every blank above it and leaves the ones below,
	// so the neighbours keep exactly one separator.
	src = []byte("[[a]]\n  k = 1\n\n\n[[a]]\n  k = 2\n\n[[a]]\n  k = 3\n")
	if out := mustApply(t, src, del(1)); string(out) != "[[a]]\n  k = 1\n\n[[a]]\n  k = 3\n" {
		t.Errorf("middle block removal left:\n%q", out)
	}

	// The last block, unterminated: the file ends where the previous EOL is.
	src = []byte("[[a]]\n  k = 1\n\n[[a]]\n  k = 2")
	if out := mustApply(t, src, del(1)); string(out) != "[[a]]\n  k = 1\n" {
		t.Errorf("unterminated last block removal left:\n%q", out)
	}
}

func TestRemoveLinesUnterminatedTail(t *testing.T) {
	src := []byte("[a]\n  k = 1\n  j = 2")
	out := mustApply(t, src, set("a", "j", nil))
	if string(out) != "[a]\n  k = 1\n" {
		t.Errorf("deleting the unterminated last line left %q", out)
	}
	// And a lone unterminated root key leaves an empty file, not a stray EOL.
	if out := mustApply(t, []byte("k = 1"), op{kind: opKey, elem: -1, key: "k"}); len(out) != 0 {
		t.Errorf("deleting the only line left %q", out)
	}
}

func TestToBlockOrder(t *testing.T) {
	got, err := toBlock(map[string]any{"why": "w", "choice": "c", "locked_at": "d"})
	if err != nil {
		t.Fatal(err)
	}
	if want := (block{{"choice", "c"}, {"locked_at", "d"}, {"why", "w"}}); !reflect.DeepEqual(got, want) {
		t.Errorf("map keys not sorted: %v", got)
	}
	in := block{{"z", 1}, {"a", 2}}
	if got, err := toBlock(in); err != nil || !reflect.DeepEqual(got, in) {
		t.Errorf("block passthrough = %v, %v", got, err)
	}
	if _, err := toBlock(42); err == nil || !strings.Contains(err.Error(), "not int") {
		t.Errorf("scalar body: err = %v", err)
	}
}

func TestOpStringNamesEveryShape(t *testing.T) {
	for _, tc := range []struct {
		o    op
		want string
	}{
		{op{kind: opKey, table: []string{"a", "b"}, elem: -1, key: "k", value: 1}, "set a.b.k"},
		{op{kind: opKey, table: []string{"a"}, elem: 2, key: "k"}, "delete a[2].k"},
		{op{kind: opAppendElem, table: []string{"a"}, elem: -1}, "append [[a]]"},
		{op{kind: opDeleteElem, table: []string{"a"}, elem: 0}, "delete [[a[0]]]"},
	} {
		if got := tc.o.String(); got != tc.want {
			t.Errorf("%#v.String() = %q, want %q", tc.o, got, tc.want)
		}
	}
}

func TestAppendElemSubTableErrorsPropagate(t *testing.T) {
	src := []byte("[[a]]\n  k = 1\n")
	// A sub-table whose key cannot be rendered fails the whole append,
	// leaving the bytes alone.
	out, err := apply(src, []op{{kind: opAppendElem, table: []string{"a"}, elem: -1, value: block{{"k", 2}, {"", block{{"s", 1}}}}}})
	if err == nil || !strings.Contains(err.Error(), "empty key") {
		t.Errorf("empty sub-table key: err = %v", err)
	}
	if !bytes.Equal(out, src) {
		t.Errorf("bytes changed on a refused append")
	}
	// And a sub-table body carrying a value that cannot render fails too.
	out, err = apply(src, []op{{kind: opAppendElem, table: []string{"a"}, elem: -1, value: block{{"k", 2}, {"sub", block{{"s", make(chan int)}}}}}})
	if err == nil {
		t.Errorf("unrenderable sub-table value was appended:\n%s", out)
	}
	if !bytes.Equal(out, src) {
		t.Errorf("bytes changed on a refused append")
	}
	// The valid shape, for contrast: keys, then a blank, then the sub-table.
	// (mustApply's normalize does not model a nested block, so the decoded
	// tree is checked directly.)
	out, err = apply(src, []op{{kind: opAppendElem, table: []string{"a"}, elem: -1, value: block{{"k", 2}, {"sub", block{{"s", 1}}}}}})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasSuffix(out, []byte("\n[[a]]\n  k = 2\n\n  [a.sub]\n    s = 1\n")) {
		t.Errorf("append with sub-table rendered:\n%s", out)
	}
	elems, _ := decodeTree(t, out)["a"].([]map[string]any)
	if len(elems) != 2 {
		t.Fatalf("decoder sees %d elements, want 2", len(elems))
	}
	if sub, _ := elems[1]["sub"].(map[string]any); sub["s"] != int64(1) {
		t.Errorf("decoder sees a[1].sub = %#v", elems[1]["sub"])
	}
}
