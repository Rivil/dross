package memadvise

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
	"time"
)

// corpusDir builds the fixture memory dir: testdata/corpus/MEMORY.md plus the
// memories it indexes, written here so each test owns a fresh copy. It sits
// three levels down so "../../.ssh/id.md" lands on a real file the advisor
// must still refuse to open.
func corpusDir(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "a", "b", "corpus")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	index, err := os.ReadFile(filepath.Join("testdata", "corpus", IndexName))
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		IndexName:       string(index),
		"a_thing_v2.md": "---\nname: a-thing\ndescription: the thing\n---\nThe thing.\n",
		"linker.md":     "---\nname: linker\n---\nSee [[a-thing]], again [[a-thing]], [[shared]] and [[nowhere]].\n",
		"notes.txt":     "not a memory\n",
		"dup_one.md":    "---\nname: shared\n---\none\n",
		"dup_two.md":    "---\nname: shared\n---\ntwo\n",
		"self.md":       "---\nname: self\n---\nI am [[self]]. Quoted: `[[a-thing]]` and ``[[a-thing]]``.\n```\n[[a-thing]]\n```\n",
		"extra.md":      "---\nname: extra\n---\nnobody indexes me\n",
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink("a_thing_v2.md", filepath.Join(dir, "link.md")); err != nil {
		t.Fatal(err)
	}
	ssh := filepath.Join(root, "a", ".ssh")
	if err := os.MkdirAll(ssh, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ssh, "id.md"), []byte("---\nname: secret\n---\nkey\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func loadCorpus(t *testing.T, dir string) *Corpus {
	t.Helper()
	c, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func deadTargets(in Integrity) []string {
	var out []string
	for _, l := range in.DeadLinks {
		out = append(out, l.Target)
	}
	return out
}

func TestDeadIndexLink(t *testing.T) {
	in := loadCorpus(t, corpusDir(t)).Integrity()
	if !slices.Contains(deadTargets(in), "gone.md") {
		t.Errorf("dead links %v lack gone.md", deadTargets(in))
	}
	for _, l := range in.DeadLinks {
		if l.Target == "gone.md" && (l.LineNo != 2 || l.Hook != "its file was deleted") {
			t.Errorf("gone.md line parsed as %+v", l)
		}
	}
}

func TestIndexLinkOutsideDirIsDead(t *testing.T) {
	c := loadCorpus(t, corpusDir(t))
	dead := deadTargets(c.Integrity())
	for _, target := range []string{"../../.ssh/id.md", "/etc/x.md", "notes.txt", "link.md"} {
		if !slices.Contains(dead, target) {
			t.Errorf("%s is not reported dead (dead: %v)", target, dead)
		}
		if c.Resolve(target) != nil {
			t.Errorf("%s resolved to a memory", target)
		}
	}
	// Nothing outside the dir was read into the corpus.
	for _, m := range c.Memories {
		if m.Name == "secret" {
			t.Errorf("%s from outside the dir was loaded", m.File)
		}
	}
}

func TestOrphans(t *testing.T) {
	in := loadCorpus(t, corpusDir(t)).Integrity()
	if !reflect.DeepEqual(in.Orphans, []string{"extra.md"}) {
		t.Errorf("orphans = %v, want [extra.md]", in.Orphans)
	}
}

func TestInboundResolvesByFrontmatterName(t *testing.T) {
	in := loadCorpus(t, corpusDir(t)).Integrity()
	if got := in.Inbound["a-thing"]; !reflect.DeepEqual(got, []string{"linker.md"}) {
		t.Errorf("Inbound[a-thing] = %v, want [linker.md]", got)
	}
	if _, ok := in.Inbound["a_thing_v2"]; ok {
		t.Error("inbound keyed by filename stem")
	}
	if got := in.Dangling["nowhere"]; !reflect.DeepEqual(got, []string{"linker.md"}) {
		t.Errorf("Dangling[nowhere] = %v, want [linker.md]", got)
	}
}

func TestInboundIgnoresCodeSpansAndSelf(t *testing.T) {
	in := loadCorpus(t, corpusDir(t)).Integrity()
	if got := in.Inbound["a-thing"]; slices.Contains(got, "self.md") {
		t.Errorf("a [[link]] inside code counted: Inbound[a-thing] = %v", got)
	}
	if got, ok := in.Inbound["self"]; ok {
		t.Errorf("a self-link counted: Inbound[self] = %v", got)
	}
}

func TestDuplicateNameSlugReported(t *testing.T) {
	in := loadCorpus(t, corpusDir(t)).Integrity()
	if got := in.DuplicateNames["shared"]; !reflect.DeepEqual(got, []string{"dup_one.md", "dup_two.md"}) {
		t.Errorf("DuplicateNames[shared] = %v, want both files", got)
	}
	if got, ok := in.Inbound["shared"]; ok {
		t.Errorf("a duplicated slug got inbound credit: %v", got)
	}
	if got, ok := in.Dangling["shared"]; ok {
		t.Errorf("a duplicated slug reported dangling: %v", got)
	}
}

func TestCorpusSkipsFIFO(t *testing.T) {
	if _, err := exec.LookPath("mkfifo"); err != nil {
		t.Skip("mkfifo not available")
	}
	dir := corpusDir(t)
	fifo := filepath.Join(dir, "foo.md")
	if out, err := exec.Command("mkfifo", fifo).CombinedOutput(); err != nil {
		t.Fatalf("mkfifo: %v: %s", err, out)
	}
	done := make(chan *Corpus, 1)
	go func() {
		c, _ := Load(dir)
		done <- c
	}()
	select {
	case c := <-done:
		if c == nil {
			t.Fatal("Load failed")
		}
		if _, ok := c.Memories["foo.md"]; ok {
			t.Error("the FIFO was loaded as a memory")
		}
	case <-time.After(time.Second):
		t.Fatal("Load blocked on a FIFO")
	}
}

func TestCorpusToleratesMissingAndCRLFFrontmatter(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		IndexName:  "- [Bare](bare.md) — no frontmatter\r\n- [CRLF](crlf.md) — windows endings\r\n",
		"bare.md":  "Just a body with [[crlf-one]].\n",
		"crlf.md":  "---\r\nname: crlf-one\r\ndescription: \"quoted\"\r\nmetadata:\r\n  type: user\r\n---\r\nBody line.\r\n",
		"empty.md": "",
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	c := loadCorpus(t, dir)
	bare := c.Memories["bare.md"]
	if bare == nil || bare.Name != "" || bare.Body != files["bare.md"] {
		t.Fatalf("bare.md parsed as %+v", bare)
	}
	crlf := c.Memories["crlf.md"]
	if crlf == nil || crlf.Name != "crlf-one" || crlf.Description != "quoted" || crlf.Body != "Body line.\r\n" {
		t.Fatalf("crlf.md parsed as %+v", crlf)
	}
	if len(c.Lines) != 2 || c.Lines[1].Hook != "windows endings" || c.Lines[1].Target != "crlf.md" {
		t.Errorf("CRLF index parsed as %+v", c.Lines)
	}
	in := c.Integrity()
	if got := in.Inbound["crlf-one"]; !reflect.DeepEqual(got, []string{"bare.md"}) {
		t.Errorf("Inbound[crlf-one] = %v", got)
	}
	if len(in.DeadLinks) != 0 {
		t.Errorf("dead links = %v", deadTargets(in))
	}
	if !reflect.DeepEqual(in.Orphans, []string{"empty.md"}) {
		t.Errorf("orphans = %v, want [empty.md]", in.Orphans)
	}
}

func TestLoadWithoutIndex(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.md"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	c := loadCorpus(t, dir)
	if c.HasIndex || len(c.Lines) != 0 || c.Memories["a.md"] == nil {
		t.Errorf("corpus without MEMORY.md = %+v", c)
	}
	if _, err := Load(filepath.Join(dir, "missing")); err == nil {
		t.Error("Load of a missing dir returned no error")
	}
}
