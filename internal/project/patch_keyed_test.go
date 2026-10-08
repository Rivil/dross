package project

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

// keyedDoc is plan.toml's shape in miniature: a root key and an
// array-of-tables whose elements are identified by id.
type keyedDoc struct {
	Seq   int         `toml:"seq"`
	Tasks []keyedTask `toml:"task"`
}

type keyedTask struct {
	ID     string `toml:"id"`
	Status string `toml:"status"`
	Title  string `toml:"title,omitempty"`
}

var byID = ArrayKey{Path: "task", Field: "id"}

const keyedSrc = `# plan header
seq = 3

[[task]]
  id = "t-1"
  status = "pending"

[[task]]
  id = "t-2"
  status = "pending"  # t-2 inline
  # t-2 body comment
  title = "two"

# a loose note, kept apart by a blank line

# about t-3
[[task]]
  id = "t-3"
  status = "pending"
`

func seedKeyed(t *testing.T, src string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "plan.toml")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func loadKeyed(t *testing.T, path string) *keyedDoc {
	t.Helper()
	var d keyedDoc
	if _, err := toml.DecodeFile(path, &d); err != nil {
		t.Fatal(err)
	}
	return &d
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func ids(d *keyedDoc) []string {
	var out []string
	for _, t := range d.Tasks {
		out = append(out, t.ID)
	}
	return out
}

// TestKeyedReorderMovesTheBlock: [t-1,t-2,t-3] → [t-1,t-3,t-2] relocates t-3's
// block whole, its own comment with it. Nothing is rewritten in place: every
// block is byte-identical, and the loose note — apart from t-3 by a blank line
// — stays where it was.
func TestKeyedReorderMovesTheBlock(t *testing.T) {
	path := seedKeyed(t, keyedSrc)
	d := loadKeyed(t, path)
	d.Tasks = []keyedTask{d.Tasks[0], d.Tasks[2], d.Tasks[1]}
	if err := SaveTOML(path, d, byID); err != nil {
		t.Fatal(err)
	}
	want := `# plan header
seq = 3

[[task]]
  id = "t-1"
  status = "pending"

# about t-3
[[task]]
  id = "t-3"
  status = "pending"

[[task]]
  id = "t-2"
  status = "pending"  # t-2 inline
  # t-2 body comment
  title = "two"

# a loose note, kept apart by a blank line
`
	if got := readFile(t, path); got != want {
		t.Errorf("reorder wrote:\n%s\nwant:\n%s", got, want)
	}
	if got := ids(loadKeyed(t, path)); strings.Join(got, ",") != "t-1,t-3,t-2" {
		t.Errorf("order = %v", got)
	}
}

// TestKeyedInsertLandsAtItsIndex: a new element at index 1 goes between t-1 and
// t-2, not at the end — and cutting it back out returns the original bytes.
func TestKeyedInsertLandsAtItsIndex(t *testing.T) {
	path := seedKeyed(t, keyedSrc)
	d := loadKeyed(t, path)
	d.Tasks = append([]keyedTask{d.Tasks[0], {ID: "t-9", Status: "pending"}}, d.Tasks[1:]...)
	if err := SaveTOML(path, d, byID); err != nil {
		t.Fatal(err)
	}
	got := readFile(t, path)
	inserted := "[[task]]\n  id = \"t-9\"\n  status = \"pending\"\n\n"
	at := strings.Index(got, inserted)
	if at < 0 {
		t.Fatalf("the new element is not where expected:\n%s", got)
	}
	if at > strings.Index(got, `id = "t-2"`) || at < strings.Index(got, `id = "t-1"`) {
		t.Errorf("the new element did not land between t-1 and t-2:\n%s", got)
	}
	if back := got[:at] + got[at+len(inserted):]; back != keyedSrc {
		t.Errorf("cutting the insertion out does not restore the file:\n%s", back)
	}
}

// TestKeyedDeleteTakesOnlyItsBlock: deleting t-2 removes its body comment but
// keeps "# about t-3", which belongs to the next element.
func TestKeyedDeleteTakesOnlyItsBlock(t *testing.T) {
	path := seedKeyed(t, keyedSrc)
	d := loadKeyed(t, path)
	d.Tasks = []keyedTask{d.Tasks[0], d.Tasks[2]}
	if err := SaveTOML(path, d, byID); err != nil {
		t.Fatal(err)
	}
	got := readFile(t, path)
	if strings.Contains(got, "t-2 body comment") {
		t.Errorf("t-2's own comment survived its deletion:\n%s", got)
	}
	if !strings.Contains(got, "# about t-3\n[[task]]\n  id = \"t-3\"") {
		t.Errorf("t-3's comment did not survive t-2's deletion:\n%s", got)
	}
}

// TestKeyedEditAndDeleteTouchTwoRegions: changing t-4's status while removing
// t-2 changes exactly t-4's status line and drops t-2's block.
func TestKeyedEditAndDeleteTouchTwoRegions(t *testing.T) {
	src := `seq = 4

[[task]]
  id = "t-1"
  status = "pending"

[[task]]
  id = "t-2"
  status = "pending"

[[task]]
  id = "t-3"
  status = "pending"  # keep me

[[task]]
  id = "t-4"
  status = "pending"
`
	path := seedKeyed(t, src)
	d := loadKeyed(t, path)
	d.Tasks = []keyedTask{d.Tasks[0], d.Tasks[2], d.Tasks[3]}
	d.Tasks[2].Status = "done"
	if err := SaveTOML(path, d, byID); err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(src, "\n[[task]]\n  id = \"t-2\"\n  status = \"pending\"\n", "", 1)
	want = strings.Replace(want, "  id = \"t-4\"\n  status = \"pending\"", "  id = \"t-4\"\n  status = \"done\"", 1)
	if got := readFile(t, path); got != want {
		t.Errorf("edit+delete wrote:\n%s\nwant:\n%s", got, want)
	}
}

// TestKeyedRefusesDuplicateIdentity: a key that does not identify cannot be
// matched by; the save is refused and nothing is written.
func TestKeyedRefusesDuplicateIdentity(t *testing.T) {
	path := seedKeyed(t, keyedSrc)
	d := loadKeyed(t, path)
	d.Tasks = append(d.Tasks, keyedTask{ID: "t-1", Status: "pending"})
	err := SaveTOML(path, d, byID)
	if err == nil || !strings.Contains(err.Error(), "two elements") {
		t.Fatalf("err = %v, want a duplicate-identity refusal", err)
	}
	if got := readFile(t, path); got != keyedSrc {
		t.Errorf("a refused save wrote:\n%s", got)
	}
}

// TestKeyedRefusesUnverifiedPatch: a keyed patch that would not load as the
// saved value is caught by verify; the file is untouched.
func TestKeyedRefusesUnverifiedPatch(t *testing.T) {
	path := seedKeyed(t, keyedSrc)
	d := loadKeyed(t, path)
	d.Tasks[1].Status = "done"

	orig := keyedOps
	keyedOps = func(old, new any, keys map[string]string) ([]op, error) {
		ops, err := diffKeyed(old, new, keys)
		if err != nil {
			return nil, err
		}
		ops[0].value = int64(42)
		return ops, nil
	}
	defer func() { keyedOps = orig }()

	err := SaveTOML(path, d, byID)
	if err == nil || !strings.Contains(err.Error(), "left untouched") {
		t.Fatalf("err = %v, want the mis-typed patch refused", err)
	}
	if got := readFile(t, path); got != keyedSrc {
		t.Errorf("original bytes were replaced:\n%s", got)
	}
}

// TestKeyedCallersUnchanged: no identity map is today's behaviour — diff and
// SaveTOML with no keys still match by position.
func TestKeyedCallersUnchanged(t *testing.T) {
	old := &keyedDoc{Tasks: []keyedTask{{ID: "a", Status: "x"}, {ID: "b", Status: "y"}}}
	reordered := &keyedDoc{Tasks: []keyedTask{{ID: "b", Status: "y"}, {ID: "a", Status: "x"}}}
	ops, err := diff(old, reordered)
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range ops {
		if o.kind == opMoveElem {
			t.Errorf("unkeyed diff emitted a move: %s", o)
		}
	}
}

// TestKeyedRefusesDuplicateOnDisk: a file whose own elements share an id is
// refused too — matching against it would diff the wrong element.
func TestKeyedRefusesDuplicateOnDisk(t *testing.T) {
	src := "[[task]]\n  id = \"t-1\"\n  status = \"pending\"\n\n[[task]]\n  id = \"t-1\"\n  status = \"done\"\n"
	path := seedKeyed(t, src)
	d := loadKeyed(t, path)
	d.Tasks[0].Status = "done"
	err := SaveTOML(path, d, byID)
	if err == nil || !strings.Contains(err.Error(), "two elements") {
		t.Fatalf("err = %v, want the on-disk duplicate refused", err)
	}
	if got := readFile(t, path); got != src {
		t.Errorf("a refused save wrote:\n%s", got)
	}
}

// TestKeyedRefusesMissingIdentity: an element with no id cannot be matched by
// id, so the save is refused rather than guessed.
func TestKeyedRefusesMissingIdentity(t *testing.T) {
	path := seedKeyed(t, keyedSrc)
	d := loadKeyed(t, path)
	d.Tasks = append(d.Tasks, keyedTask{Status: "pending"})
	if err := SaveTOML(path, d, byID); err == nil || !strings.Contains(err.Error(), "no string id") {
		t.Fatalf("err = %v, want a missing-identity refusal", err)
	}
}

// TestFirstElementKeepsSectionComment: a comment directly above the FIRST
// element heads the section, not the element. Deleting that element — keyed
// or not — leaves it, and a block moved to the front lands beneath it.
func TestFirstElementKeepsSectionComment(t *testing.T) {
	src := "seq = 2\n\n# Tasks\n[[task]]\n  id = \"t-1\"\n  status = \"pending\"\n\n[[task]]\n  id = \"t-2\"\n  status = \"pending\"\n"
	for _, keys := range [][]ArrayKey{{byID}, nil} {
		path := seedKeyed(t, src)
		d := loadKeyed(t, path)
		d.Tasks = d.Tasks[1:]
		if err := SaveTOML(path, d, keys...); err != nil {
			t.Fatal(err)
		}
		if got, want := readFile(t, path), "seq = 2\n\n# Tasks\n[[task]]\n  id = \"t-2\"\n  status = \"pending\"\n"; got != want {
			t.Errorf("keys %v: deleting the first element wrote:\n%s\nwant:\n%s", keys, got, want)
		}
	}

	path := seedKeyed(t, src)
	d := loadKeyed(t, path)
	d.Tasks = []keyedTask{d.Tasks[1], d.Tasks[0]}
	if err := SaveTOML(path, d, byID); err != nil {
		t.Fatal(err)
	}
	want := "seq = 2\n\n# Tasks\n[[task]]\n  id = \"t-2\"\n  status = \"pending\"\n\n[[task]]\n  id = \"t-1\"\n  status = \"pending\"\n"
	if got := readFile(t, path); got != want {
		t.Errorf("moving to the front wrote:\n%s\nwant:\n%s", got, want)
	}
}

// TestLooseCommentsStayPut: a comment set apart by a blank line belongs to no
// element. Moving the last element leaves a note at the end of the file where
// it was, and deleting t-2 leaves the loose note that follows it.
func TestLooseCommentsStayPut(t *testing.T) {
	src := "seq = 2\n\n[[task]]\n  id = \"t-1\"\n  status = \"pending\"\n\n[[task]]\n  id = \"t-2\"\n  status = \"pending\"\n\n# trailing note\n"
	path := seedKeyed(t, src)
	d := loadKeyed(t, path)
	d.Tasks = []keyedTask{d.Tasks[1], d.Tasks[0]}
	if err := SaveTOML(path, d, byID); err != nil {
		t.Fatal(err)
	}
	want := "seq = 2\n\n[[task]]\n  id = \"t-2\"\n  status = \"pending\"\n\n[[task]]\n  id = \"t-1\"\n  status = \"pending\"\n\n# trailing note\n"
	if got := readFile(t, path); got != want {
		t.Errorf("moving the last element wrote:\n%s\nwant:\n%s", got, want)
	}

	path = seedKeyed(t, keyedSrc)
	d = loadKeyed(t, path)
	d.Tasks = []keyedTask{d.Tasks[0], d.Tasks[2]}
	if err := SaveTOML(path, d, byID); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, path); !strings.Contains(got, "# a loose note, kept apart by a blank line") {
		t.Errorf("deleting t-2 took the loose note with it:\n%s", got)
	}
}

// TestEmptyHeaderCascadeKeepsSeparator pins removeBlock's behaviour for the
// empty-header cascade deleteKey runs, which the element-deletion separator
// rule must not reach: a nested header written right under its parent, emptied
// by the delete, still leaves the blank line before the next header.
func TestEmptyHeaderCascadeKeepsSeparator(t *testing.T) {
	type svc struct {
		Cmd string `toml:"cmd,omitempty"`
	}
	type doc struct {
		A       int `toml:"a"`
		Runtime struct {
			Services map[string]svc `toml:"services,omitempty"`
		} `toml:"runtime"`
		Other struct {
			B int `toml:"b"`
		} `toml:"other"`
	}
	src := "a = 1\n\n[runtime]\n[runtime.services]\n[runtime.services.x]\ncmd = \"c\"\n\n[other]\nb = 2\n"
	path := seedKeyed(t, src)
	var d doc
	if _, err := toml.DecodeFile(path, &d); err != nil {
		t.Fatal(err)
	}
	d.Runtime.Services = nil
	if err := SaveTOML(path, &d); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, path); !strings.Contains(got, "\n\n[other]") {
		t.Errorf("the separator before [other] was lost:\n%q", got)
	}
}

// TestKeyedInsertAtEndAppends: a new element whose final index is the end of
// the array is an append — `dross task add` with no --after is exactly this
// shape. It lands after the last element and every original byte is kept.
func TestKeyedInsertAtEndAppends(t *testing.T) {
	path := seedKeyed(t, keyedSrc)
	d := loadKeyed(t, path)
	d.Tasks = append(d.Tasks, keyedTask{ID: "t-9", Status: "pending"})
	if err := SaveTOML(path, d, byID); err != nil {
		t.Fatal(err)
	}
	got := readFile(t, path)
	if !strings.HasPrefix(got, keyedSrc) {
		t.Fatalf("an append at the end rewrote existing bytes:\n%s", got)
	}
	if rest := got[len(keyedSrc):]; !strings.Contains(rest, "[[task]]\n  id = \"t-9\"\n  status = \"pending\"\n") {
		t.Errorf("the new element is not after the last one; appended:\n%q", rest)
	}
	if got := ids(loadKeyed(t, path)); strings.Join(got, ",") != "t-1,t-2,t-3,t-9" {
		t.Errorf("order = %v", got)
	}
}

// nestedKeyedDoc keys an array-of-tables that sits under a table, so the
// element count is looked up through a qualified parent segment.
type nestedKeyedDoc struct {
	Outer struct {
		Name  string      `toml:"name"`
		Items []keyedTask `toml:"item"`
	} `toml:"outer"`
}

// TestKeyedNestedArrayInserts: [[outer.item]] matched by id takes an insert at
// its index and an insert at its end, keeping every original byte.
func TestKeyedNestedArrayInserts(t *testing.T) {
	src := `[outer]
  name = "x"

[[outer.item]]
  id = "a"
  status = "pending"

[[outer.item]]
  id = "b"
  status = "pending"
`
	path := seedKeyed(t, src)
	var d nestedKeyedDoc
	if _, err := toml.DecodeFile(path, &d); err != nil {
		t.Fatal(err)
	}
	d.Outer.Items = []keyedTask{d.Outer.Items[0], {ID: "m", Status: "pending"}, d.Outer.Items[1], {ID: "z", Status: "pending"}}
	if err := SaveTOML(path, &d, ArrayKey{Path: "outer.item", Field: "id"}); err != nil {
		t.Fatal(err)
	}
	var back nestedKeyedDoc
	if _, err := toml.DecodeFile(path, &back); err != nil {
		t.Fatal(err)
	}
	var order []string
	for _, it := range back.Outer.Items {
		order = append(order, it.ID)
	}
	if strings.Join(order, ",") != "a,m,b,z" {
		t.Errorf("order = %v, want a,m,b,z:\n%s", order, readFile(t, path))
	}
	got := readFile(t, path)
	for _, block := range []string{"[outer]\n  name = \"x\"\n", "[[outer.item]]\n  id = \"a\"\n  status = \"pending\"\n", "[[outer.item]]\n  id = \"b\"\n  status = \"pending\"\n"} {
		if !strings.Contains(got, block) {
			t.Errorf("an original block changed; missing:\n%s\nin:\n%s", block, got)
		}
	}
}

// TestKeyedMoveCarriesTrailingComments: comment lines touching an element's
// last key belong to it, so a move takes them along and a delete removes them;
// a comment set apart by a blank line stays put either way.
func TestKeyedMoveCarriesTrailingComments(t *testing.T) {
	src := `seq = 3

[[task]]
  id = "t-1"
  status = "pending"

[[task]]
  id = "t-2"
  status = "pending"
  # t-2 trailing note
  # and its second line

# loose note

[[task]]
  id = "t-3"
  status = "pending"
`
	// [t-2, t-1, t-3]: the diff moves t-2 itself to the front.
	path := seedKeyed(t, src)
	d := loadKeyed(t, path)
	d.Tasks = []keyedTask{d.Tasks[1], d.Tasks[0], d.Tasks[2]}
	if err := SaveTOML(path, d, byID); err != nil {
		t.Fatal(err)
	}
	got := readFile(t, path)
	moved := "[[task]]\n  id = \"t-2\"\n  status = \"pending\"\n  # t-2 trailing note\n  # and its second line\n"
	at := strings.Index(got, moved)
	if at < 0 {
		t.Fatalf("t-2 moved without its trailing comments:\n%s", got)
	}
	if at > strings.Index(got, `id = "t-1"`) {
		t.Errorf("t-2 is not ahead of t-1:\n%s", got)
	}
	if strings.Count(got, "# t-2 trailing note") != 1 || !strings.Contains(got, "\n# loose note\n") {
		t.Errorf("a comment was duplicated, dropped, or the loose note moved:\n%s", got)
	}

	path = seedKeyed(t, src)
	d = loadKeyed(t, path)
	d.Tasks = []keyedTask{d.Tasks[0], d.Tasks[2]}
	if err := SaveTOML(path, d, byID); err != nil {
		t.Fatal(err)
	}
	got = readFile(t, path)
	if strings.Contains(got, "t-2 trailing note") || strings.Contains(got, "its second line") {
		t.Errorf("t-2's trailing comments survived its deletion:\n%s", got)
	}
	if !strings.Contains(got, "# loose note") {
		t.Errorf("the loose note went with t-2:\n%s", got)
	}
}

// TestMoveElemRefusesALaterDestination: the keyed diff only ever moves an
// element earlier, so a move to its own index or a later one is a patcher
// error, naming the array and both indices.
func TestMoveElemRefusesALaterDestination(t *testing.T) {
	d, err := indexDoc([]byte(keyedSrc))
	if err != nil {
		t.Fatal(err)
	}
	for _, to := range []int{1, 2} {
		_, err := d.moveElem([]seg{{"task", 1}}, to)
		if want := fmt.Sprintf("(1 → %d)", to); err == nil || !strings.Contains(err.Error(), "[[task]]") || !strings.Contains(err.Error(), want) {
			t.Errorf("moveElem(1 → %d) = %v, want a refusal naming [[task]] and %s", to, err, want)
		}
	}
}
