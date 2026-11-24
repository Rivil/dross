package project

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
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

// precondDoc is plan.toml's task with its nested [[task.preconditions]]
// blocks: an array-of-tables inside each element of a keyed one.
type precondDoc struct {
	Seq   int           `toml:"seq"`
	Tasks []precondTask `toml:"task"`
}

type precondTask struct {
	ID            string    `toml:"id"`
	Status        string    `toml:"status"`
	Title         string    `toml:"title,omitempty"`
	Preconditions []precond `toml:"preconditions,omitempty"`
}

type precond struct {
	Run string   `toml:"run"`
	Max *float64 `toml:"max,omitempty"`
	Why string   `toml:"why,omitempty"`
}

func loadPrecond(t *testing.T, path string) *precondDoc {
	t.Helper()
	var d precondDoc
	if _, err := toml.DecodeFile(path, &d); err != nil {
		t.Fatalf("reload: %v\n%s", err, readFile(t, path))
	}
	return &d
}

func fptr(f float64) *float64 { return &f }

// precondSrc: t-2 carries two header-form blocks, one with a comment between
// its keys and one with a key the struct does not model.
const precondSrc = `# plan header
seq = 3

[[task]]
  id = "t-1"
  status = "pending"

[[task]]
  id = "t-2"
  status = "pending"

  [[task.preconditions]]
    run = "true"
    # first block's own note
    why = "smoke"

  [[task.preconditions]]
    run = "wc -l < x"
    max = 3.0
    mx = 3

# a loose note between t-2 and t-3

# about t-3
[[task]]
  id = "t-3"
  status = "pending"
`

// TestNestedStatusChangeTouchesOneLine: a status change on a plan whose t-2
// carries precondition blocks saves, and changes exactly t-1's status line.
func TestNestedStatusChangeTouchesOneLine(t *testing.T) {
	path := seedKeyed(t, precondSrc)
	d := loadPrecond(t, path)
	d.Tasks[0].Status = "done"
	if err := SaveTOML(path, d, byID); err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(precondSrc, "  id = \"t-1\"\n  status = \"pending\"", "  id = \"t-1\"\n  status = \"done\"", 1)
	if got := readFile(t, path); got != want {
		t.Errorf("status change wrote:\n%s\nwant:\n%s", got, want)
	}
}

// TestNestedEditTouchesOnlyItsValue: changing preconditions[1].max splices that
// one value; the comment in the other block and the unmodeled key stay.
func TestNestedEditTouchesOnlyItsValue(t *testing.T) {
	path := seedKeyed(t, precondSrc)
	d := loadPrecond(t, path)
	d.Tasks[1].Preconditions[1].Max = fptr(5)
	if err := SaveTOML(path, d, byID); err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(precondSrc, "max = 3.0", "max = 5.0", 1)
	if got := readFile(t, path); got != want {
		t.Errorf("max edit wrote:\n%s\nwant:\n%s", got, want)
	}
}

// TestNestedFirstPreconditionLandsInItsTask: t-2's first block goes at the end
// of t-2's own span — not after t-3, whose block is the last
// [[task.preconditions]] in the file — and a second one follows it there.
func TestNestedFirstPreconditionLandsInItsTask(t *testing.T) {
	src := strings.Replace(keyedSrc, "  id = \"t-3\"\n  status = \"pending\"\n",
		"  id = \"t-3\"\n  status = \"pending\"\n\n  [[task.preconditions]]\n    run = \"three\"\n", 1)
	path := seedKeyed(t, src)
	d := loadPrecond(t, path)
	d.Tasks[1].Preconditions = []precond{{Run: "make lint"}}
	if err := SaveTOML(path, d, byID); err != nil {
		t.Fatal(err)
	}
	block := "\n  [[task.preconditions]]\n    run = \"make lint\"\n"
	want := strings.Replace(src, "  title = \"two\"\n", "  title = \"two\"\n"+block, 1)
	if got := readFile(t, path); got != want {
		t.Errorf("first precondition wrote:\n%s\nwant:\n%s", got, want)
	}
	if back := loadPrecond(t, path); len(back.Tasks[1].Preconditions) != 1 || len(back.Tasks[2].Preconditions) != 1 || back.Tasks[2].Preconditions[0].Run != "three" {
		t.Errorf("reload put the block in the wrong task: %+v", back.Tasks)
	}

	d = loadPrecond(t, path)
	d.Tasks[1].Preconditions = append(d.Tasks[1].Preconditions, precond{Run: "make vet", Why: "second"})
	if err := SaveTOML(path, d, byID); err != nil {
		t.Fatal(err)
	}
	second := block + "\n  [[task.preconditions]]\n    run = \"make vet\"\n    why = \"second\"\n"
	want = strings.Replace(src, "  title = \"two\"\n", "  title = \"two\"\n"+second, 1)
	if got := readFile(t, path); got != want {
		t.Errorf("second precondition wrote:\n%s\nwant:\n%s", got, want)
	}
}

// TestNestedClearTakesExactlyItsBlocks: clearing t-2's preconditions drops
// both blocks — the unmodeled key with them — and leaves the loose note and
// t-3's comment as they were, whether the blocks are spaced the encoder's way
// or written tight against the task's keys.
func TestNestedClearTakesExactlyItsBlocks(t *testing.T) {
	spaced := strings.Replace(precondSrc, `
  [[task.preconditions]]
    run = "true"
    # first block's own note
    why = "smoke"

  [[task.preconditions]]
    run = "wc -l < x"
    max = 3.0
    mx = 3
`, "", 1)
	tight := "seq = 2\n\n[[task]]\n  id = \"t-2\"\n  status = \"pending\"\n  [[task.preconditions]]\n    run = \"a\"\n  [[task.preconditions]]\n    run = \"b\"\n    mx = 3\n\n# between t-2 and t-3\n[[task]]\n  id = \"t-3\"\n  status = \"pending\"\n"
	for name, tc := range map[string]struct {
		src, want string
		task      int
	}{
		"spaced": {precondSrc, spaced, 1},
		"tight":  {tight, "seq = 2\n\n[[task]]\n  id = \"t-2\"\n  status = \"pending\"\n\n# between t-2 and t-3\n[[task]]\n  id = \"t-3\"\n  status = \"pending\"\n", 0},
	} {
		path := seedKeyed(t, tc.src)
		d := loadPrecond(t, path)
		d.Tasks[tc.task].Preconditions = nil
		if err := SaveTOML(path, d, byID); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got := readFile(t, path); got != tc.want {
			t.Errorf("%s: clearing wrote:\n%s\nwant:\n%s", name, got, tc.want)
		}
	}
}

// TestNestedDeleteKeepsAHeadingWithItsSibling: a comment right above the first
// block heads the precondition list. Deleting that block leaves it hugging the
// next sibling block; deleting the only block leaves it, and the separator
// before the next task, where they were.
func TestNestedDeleteKeepsAHeadingWithItsSibling(t *testing.T) {
	two := "[[task]]\n  id = \"t-1\"\n  status = \"pending\"\n  # gates\n  [[task.preconditions]]\n    run = \"a\"\n\n  [[task.preconditions]]\n    run = \"b\"\n\n[[task]]\n  id = \"t-2\"\n  status = \"pending\"\n"
	path := seedKeyed(t, two)
	d := loadPrecond(t, path)
	d.Tasks[0].Preconditions = d.Tasks[0].Preconditions[1:]
	if err := SaveTOML(path, d, byID); err != nil {
		t.Fatal(err)
	}
	if got, want := readFile(t, path), "[[task]]\n  id = \"t-1\"\n  status = \"pending\"\n  # gates\n  [[task.preconditions]]\n    run = \"b\"\n\n[[task]]\n  id = \"t-2\"\n  status = \"pending\"\n"; got != want {
		t.Errorf("deleting the first of two wrote:\n%s\nwant:\n%s", got, want)
	}

	d = loadPrecond(t, path)
	d.Tasks[0].Preconditions = nil
	if err := SaveTOML(path, d, byID); err != nil {
		t.Fatal(err)
	}
	if got, want := readFile(t, path), "[[task]]\n  id = \"t-1\"\n  status = \"pending\"\n  # gates\n\n[[task]]\n  id = \"t-2\"\n  status = \"pending\"\n"; got != want {
		t.Errorf("deleting the only block wrote:\n%s\nwant:\n%s", got, want)
	}
}

// TestNestedNewTaskRendersItsBlocks: a new task carrying preconditions — at
// the end (task add) and in the middle (task add --after) — renders them after
// its own [[task]] header, and the file reloads as the saved value.
func TestNestedNewTaskRendersItsBlocks(t *testing.T) {
	newTask := precondTask{ID: "t-9", Status: "pending", Preconditions: []precond{
		{Run: "go vet ./...", Max: fptr(2)},
		{Run: "true", Why: "w"},
	}}
	rendered := "[[task]]\n  id = \"t-9\"\n  status = \"pending\"\n\n  [[task.preconditions]]\n    run = \"go vet ./...\"\n    max = 2.0\n\n  [[task.preconditions]]\n    run = \"true\"\n    why = \"w\"\n"
	for _, at := range []int{3, 1} {
		path := seedKeyed(t, precondSrc)
		d := loadPrecond(t, path)
		d.Tasks = append(d.Tasks[:at], append([]precondTask{newTask}, d.Tasks[at:]...)...)
		if err := SaveTOML(path, d, byID); err != nil {
			t.Fatalf("at %d: %v", at, err)
		}
		got := readFile(t, path)
		if !strings.Contains(got, rendered) {
			t.Errorf("at %d: the new task's blocks are not under its header:\n%s", at, got)
		}
		if back := loadPrecond(t, path); !reflect.DeepEqual(back, d) {
			t.Errorf("at %d: reload = %+v\nwant %+v", at, back, d)
		}
	}
}

// ownedSrc: every task owns blocks, so a span that loses track of them shows
// up as a block reattached to a neighbour.
const ownedSrc = `seq = 3

[[task]]
  id = "t-1"
  status = "pending"

  [[task.preconditions]]
    run = "one-a"

  [[task.preconditions]]
    run = "one-b"

[[task]]
  id = "t-2"
  status = "pending"

  [[task.preconditions]]
    run = "two-a"

# about t-3
[[task]]
  id = "t-3"
  status = "pending"

  [[task.preconditions]]
    run = "three-a"
    max = 1.0
`

// TestNestedBlocksTravelWithTheirTask: moving, deleting and inserting tasks
// carries each task's blocks as part of its span.
func TestNestedBlocksTravelWithTheirTask(t *testing.T) {
	for name, edit := range map[string]func(d *precondDoc){
		"move t-3 before t-1": func(d *precondDoc) { d.Tasks = []precondTask{d.Tasks[2], d.Tasks[0], d.Tasks[1]} },
		"delete t-2":          func(d *precondDoc) { d.Tasks = []precondTask{d.Tasks[0], d.Tasks[2]} },
		"add after t-1": func(d *precondDoc) {
			d.Tasks = append(d.Tasks[:1], append([]precondTask{{ID: "t-9", Status: "pending"}}, d.Tasks[1:]...)...)
		},
	} {
		path := seedKeyed(t, ownedSrc)
		d := loadPrecond(t, path)
		edit(d)
		if err := SaveTOML(path, d, byID); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		got := readFile(t, path)
		if back := loadPrecond(t, path); !reflect.DeepEqual(back, d) {
			t.Errorf("%s: reload = %+v\nwant %+v\n%s", name, back, d, got)
		}
		if name == "move t-3 before t-1" && !strings.HasPrefix(got, "seq = 3\n\n# about t-3\n[[task]]\n  id = \"t-3\"\n  status = \"pending\"\n\n  [[task.preconditions]]\n    run = \"three-a\"\n    max = 1.0\n\n[[task]]\n  id = \"t-1\"") {
			t.Errorf("%s: t-3 did not move whole with its comment and block:\n%s", name, got)
		}
		if name == "delete t-2" && strings.Contains(got, "two-a") {
			t.Errorf("%s: t-2's block survived:\n%s", name, got)
		}
	}
}

// TestNestedInlineForm: an element spelling its preconditions inline takes a
// status change, but an edit to that inline list is refused by name with the
// file byte-unchanged — and it does not block a header-form edit in another
// task.
func TestNestedInlineForm(t *testing.T) {
	src := `[[task]]
  id = "t-1"
  status = "pending"
  preconditions = [{run = "a", max = 3.0}]

[[task]]
  id = "t-2"
  status = "pending"

  [[task.preconditions]]
    run = "b"
`
	path := seedKeyed(t, src)
	d := loadPrecond(t, path)
	d.Tasks[0].Status = "done"
	if err := SaveTOML(path, d, byID); err != nil {
		t.Fatalf("status change next to an inline list: %v", err)
	}
	changed := strings.Replace(src, "  id = \"t-1\"\n  status = \"pending\"", "  id = \"t-1\"\n  status = \"done\"", 1)
	if got := readFile(t, path); got != changed {
		t.Errorf("status change wrote:\n%s\nwant:\n%s", got, changed)
	}

	for name, edit := range map[string]func(d *precondDoc){
		"set a key":      func(d *precondDoc) { d.Tasks[0].Preconditions[0].Max = fptr(5) },
		"append a block": func(d *precondDoc) { d.Tasks[0].Preconditions = append(d.Tasks[0].Preconditions, precond{Run: "c"}) },
	} {
		d = loadPrecond(t, path)
		edit(d)
		err := SaveTOML(path, d, byID)
		if err == nil || !strings.Contains(err.Error(), "task.preconditions at line 4 is an inline table") {
			t.Errorf("%s: err = %v, want the inline refusal naming task.preconditions", name, err)
		}
		if got := readFile(t, path); got != changed {
			t.Errorf("%s: a refused save wrote:\n%s", name, got)
		}
	}

	d = loadPrecond(t, path)
	d.Tasks[1].Preconditions[0].Run = "c"
	if err := SaveTOML(path, d, byID); err != nil {
		t.Fatalf("header-form edit beside an inline list: %v", err)
	}
	if got, want := readFile(t, path), strings.Replace(changed, `run = "b"`, `run = "c"`, 1); got != want {
		t.Errorf("header-form edit wrote:\n%s\nwant:\n%s", got, want)
	}
}

// deepDoc nests one level past what has a spelling: a precondition with its
// own table and its own array-of-tables.
type deepDoc struct {
	Tasks []deepTask `toml:"task"`
}

type deepTask struct {
	ID            string    `toml:"id"`
	Preconditions []deepPre `toml:"preconditions,omitempty"`
}

type deepPre struct {
	Run   string            `toml:"run"`
	Env   map[string]string `toml:"env,omitempty"`
	Steps []deepStep        `toml:"steps,omitempty"`
}

type deepStep struct {
	Cmd string `toml:"cmd"`
}

// TestNestedRefusalStaysNarrow: only an array-of-tables directly under a
// keyed element has a spelling. One level deeper — a table or an array in a
// precondition, on an existing task or a new one — and any nesting under an
// unkeyed array are refused by dotted path with the file untouched.
func TestNestedRefusalStaysNarrow(t *testing.T) {
	src := "[[task]]\n  id = \"t-1\"\n\n  [[task.preconditions]]\n    run = \"a\"\n"
	for name, tc := range map[string]struct {
		edit func(d *deepDoc)
		want string
	}{
		"table in a precondition": {func(d *deepDoc) { d.Tasks[0].Preconditions[0].Env = map[string]string{"A": "1"} }, "task.preconditions.env: a table nested inside"},
		"array in a precondition": {func(d *deepDoc) { d.Tasks[0].Preconditions[0].Steps = []deepStep{{Cmd: "x"}} }, "task.preconditions.steps: a table nested inside"},
		"new block carrying a table": {func(d *deepDoc) {
			d.Tasks[0].Preconditions = append(d.Tasks[0].Preconditions, deepPre{Run: "b", Env: map[string]string{"A": "1"}})
		}, "task.preconditions.env: a table nested inside"},
		"new task carrying a deep block": {func(d *deepDoc) {
			d.Tasks = append(d.Tasks, deepTask{ID: "t-2", Preconditions: []deepPre{{Run: "b", Steps: []deepStep{{Cmd: "x"}}}}})
		}, "task.preconditions.steps: a table nested inside"},
	} {
		path := seedKeyed(t, src)
		var d deepDoc
		if _, err := toml.DecodeFile(path, &d); err != nil {
			t.Fatal(err)
		}
		tc.edit(&d)
		if err := SaveTOML(path, &d, byID); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want %q", name, err, tc.want)
		}
		if got := readFile(t, path); got != src {
			t.Errorf("%s: a refused save wrote:\n%s", name, got)
		}
	}

	// Unkeyed, the same blocks have no spelling at all: a status change on
	// any task is refused rather than guessed.
	path := seedKeyed(t, precondSrc)
	d := loadPrecond(t, path)
	d.Tasks[0].Status = "done"
	if err := SaveTOML(path, d); err == nil || !strings.Contains(err.Error(), "task.preconditions: a table nested inside") {
		t.Errorf("unkeyed: err = %v, want a refusal naming task.preconditions", err)
	}
	if got := readFile(t, path); got != precondSrc {
		t.Errorf("unkeyed: a refused save wrote:\n%s", got)
	}
}

// TestQualifyNestedElement pins the op shapes the patcher accepts under an
// array element: a nested array addressed by element and outer is reached;
// one with no outer, an outer past the end, an outer on an array nested in
// nothing, a plain-table op through an element, or two levels of nesting is
// refused by name.
func TestQualifyNestedElement(t *testing.T) {
	src := []byte(precondSrc)
	pre := []string{"task", "preconditions"}
	out, err := apply(src, []op{{kind: opKey, table: pre, elem: 0, outer: []int{1}, key: "why", value: "s2"}})
	if err != nil {
		t.Fatal(err)
	}
	if want := strings.Replace(precondSrc, `why = "smoke"`, `why = "s2"`, 1); string(out) != want {
		t.Errorf("nested set wrote:\n%s", out)
	}
	for name, tc := range map[string]struct {
		o    op
		want string
	}{
		"no outer":         {op{kind: opKey, table: pre, elem: 0, key: "why", value: "x"}, "[[task]] is an array-of-tables: a table nested inside one of its elements"},
		"outer past end":   {op{kind: opKey, table: pre, elem: 0, outer: []int{7}, key: "why", value: "x"}, "[[task]] has 3 element(s), no element 7"},
		"plain table":      {op{kind: opKey, table: []string{"task", "sub"}, elem: -1, outer: []int{1}, key: "k", value: "x"}, "a table nested inside one of its elements"},
		"outer on top":     {op{kind: opKey, table: []string{"task"}, elem: 0, outer: []int{1}, key: "status", value: "x"}, "no array-of-tables encloses it"},
		"element past end": {op{kind: opKey, table: pre, elem: 2, outer: []int{1}, key: "why", value: "x"}, "has 2 element(s), no element 2"},
		"two levels deep":  {op{kind: opKey, table: []string{"task", "preconditions", "steps"}, elem: 0, outer: []int{1}, key: "c", value: "x"}, "a table nested inside one of its elements"},
	} {
		out, err := apply(src, []op{tc.o})
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want %q", name, err, tc.want)
		}
		if string(out) != precondSrc {
			t.Errorf("%s: bytes changed on a refused op", name)
		}
	}
	if got := (op{kind: opDeleteElem, table: pre, elem: 1, outer: []int{2}}).String(); got != "delete [[task[2].preconditions[1]]]" {
		t.Errorf("String = %q", got)
	}
}

// TestElemCountNestedTwoDeep: elemCount resolves every array level it passes
// through by its own outer index, so an array nested two deep counts the
// elements of the one inner element named — not its first sibling's, and not
// by reusing the outermost index.
func TestElemCountNestedTwoDeep(t *testing.T) {
	src := `[[a]]
  [[a.b]]
    [[a.b.c]]
      n = 1
    [[a.b.c]]
      n = 2
  [[a.b]]
    [[a.b.c]]
      n = 3
[[a]]
  [[a.b]]
    [[a.b.c]]
      n = 4
    [[a.b.c]]
      n = 5
    [[a.b.c]]
      n = 6
`
	d, err := indexDoc([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		table []string
		outer []int
		want  int
	}{
		{[]string{"a"}, nil, 2},
		{[]string{"a", "b"}, []int{0}, 2},
		{[]string{"a", "b"}, []int{1}, 1},
		{[]string{"a", "b", "c"}, []int{0, 0}, 2},
		{[]string{"a", "b", "c"}, []int{0, 1}, 1},
		{[]string{"a", "b", "c"}, []int{1, 0}, 3},
		{[]string{"a", "b", "c"}, []int{1, 1}, 0},
	} {
		if got := d.elemCount(c.table, c.outer); got != c.want {
			t.Errorf("elemCount(%v, %v) = %d, want %d", c.table, c.outer, got, c.want)
		}
	}
}
