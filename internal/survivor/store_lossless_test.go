package survivor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A store with two categories and three acceptances, two of them carrying the
// trailing `# dross:allow-secret` marker a whole-file re-encode used to strip.
const losslessStore = `[[category]]
  name = "keep-cat"
  reason = "shared reason"

[[category]]
  name = "lone-cat"
  reason = "only entry two uses this"

[[accepted]]
  key = "k1"
  file = "a.go"
  op = "OP"
  text = "first source line"  # dross:allow-secret
  category = "keep-cat"

[[accepted]]
  key = "k2"
  file = "b.go"
  op = "OP"
  text = "second"
  category = "lone-cat"

[[accepted]]
  key = "k3"
  file = "c.go"
  op = "OP"
  text = "third source line"  # dross:allow-secret
  category = "keep-cat"
`

const (
	loneCatBlock = "\n[[category]]\n  name = \"lone-cat\"\n  reason = \"only entry two uses this\"\n"
	k2Block      = "\n[[accepted]]\n  key = \"k2\"\n  file = \"b.go\"\n  op = \"OP\"\n  text = \"second\"\n  category = \"lone-cat\"\n"
	k1Block      = "[[accepted]]\n  key = \"k1\"\n  file = \"a.go\"\n  op = \"OP\"\n  text = \"first source line\"  # dross:allow-secret\n  category = \"keep-cat\"\n"
	k3Block      = "[[accepted]]\n  key = \"k3\"\n  file = \"c.go\"\n  op = \"OP\"\n  text = \"third source line\"  # dross:allow-secret\n  category = \"keep-cat\"\n"
)

func seedStore(t *testing.T, src string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), StoreFile)
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func readStore(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TestRetireMiddleKeepsNeighbours: retiring the middle acceptance removes its
// block and the category only it used — and nothing else. The neighbours keep
// their markers, and the still-used category is untouched.
func TestRetireMiddleKeepsNeighbours(t *testing.T) {
	path := seedStore(t, losslessStore)
	if err := Retire(path, "k2"); err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(strings.Replace(losslessStore, loneCatBlock, "", 1), k2Block, "", 1)
	if got := readStore(t, path); got != want {
		t.Errorf("retire wrote:\n%s\nwant:\n%s", got, want)
	}
}

// TestAcceptNewCategoryLandsAfterCategories: a new category's block goes after
// the last existing category, the new acceptance goes at the end, and every
// existing acceptance block is untouched.
func TestAcceptNewCategoryLandsAfterCategories(t *testing.T) {
	path := seedStore(t, losslessStore)
	if err := Accept(path, Acceptance{Key: "k4", File: "d.go", Op: "OP", Text: "fourth", Category: "new-cat", Reason: "new reason"}); err != nil {
		t.Fatal(err)
	}
	got := readStore(t, path)
	newCat := strings.Index(got, `name = "new-cat"`)
	if newCat < strings.Index(got, `name = "lone-cat"`) || newCat > strings.Index(got, "[[accepted]]") {
		t.Errorf("the new category did not land after the existing ones and before the acceptances:\n%s", got)
	}
	for _, block := range []string{k1Block, k2Block, k3Block} {
		if !strings.Contains(got, block) {
			t.Errorf("an existing acceptance block changed; missing:\n%s\nin:\n%s", block, got)
		}
	}
	if !strings.HasSuffix(strings.TrimRight(got, "\n"), `category = "new-cat"`) {
		t.Errorf("the new acceptance is not the last block:\n%s", got)
	}
}

// TestRetireLastLeavesNoEmptyList: emptying the store's lists writes no
// `category = []` / `accepted = []` line the next acceptance would trip over.
func TestRetireLastLeavesNoEmptyList(t *testing.T) {
	path := seedStore(t, losslessStore)
	if err := Retire(path, "k1", "k2", "k3"); err != nil {
		t.Fatal(err)
	}
	if got := readStore(t, path); strings.Contains(got, "= []") {
		t.Errorf("an emptied store wrote an empty-list line:\n%s", got)
	}
	if err := Accept(path, Acceptance{Key: "k5", File: "e.go", Op: "OP", Text: "fifth", Reason: "own reason"}); err != nil {
		t.Fatalf("accepting into an emptied store: %v", err)
	}
}

// TestRepoStoreSurvivesAnAcceptance: the repo's own survivors.toml, copied
// aside, keeps every original byte ahead of a newly accepted entry.
func TestRepoStoreSurvivesAnAcceptance(t *testing.T) {
	orig, err := os.ReadFile(filepath.Join("..", "..", ".dross", StoreFile))
	if err != nil {
		t.Fatal(err)
	}
	path := seedStore(t, string(orig))
	if err := Accept(path, Acceptance{Key: "corpus-key", File: "z.go", Op: "OP", Text: "corpus", Reason: "a corpus round-trip"}); err != nil {
		t.Fatal(err)
	}
	got := readStore(t, path)
	if !strings.HasPrefix(got, string(orig)) {
		t.Error("the original survivors.toml bytes did not survive ahead of the new block")
	}
	if !strings.Contains(got[len(orig):], `key = "corpus-key"`) {
		t.Errorf("the new acceptance is not after the original bytes:\n%s", got[len(orig):])
	}
}
