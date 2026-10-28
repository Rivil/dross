package assets

import (
	"bytes"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"
)

const (
	soloFetch = "**When** `mode=solo` — the run began with --solo: `dross prompt partial execute solo`"
	soloBody  = "## Solo\n\nsolo-only text\n"
)

// partialFS is a minimal prompt tree with one fetched partial.
func partialFS(execute string, extra map[string]string) fstest.MapFS {
	m := fstest.MapFS{
		"prompts/execute.md":       {Data: []byte(execute)},
		"prompts/verify.md":        {Data: []byte("# verify\n")},
		"prompts/_interaction.md":  {Data: []byte("# Interaction playbook\n")},
		"prompts/_execute.solo.md": {Data: []byte(soloBody)},
	}
	for k, v := range extra {
		m[k] = &fstest.MapFile{Data: []byte(v)}
	}
	return m
}

// recordingFS remembers every path opened through it.
type recordingFS struct {
	fs.FS
	opened []string
}

func (r *recordingFS) Open(name string) (fs.File, error) {
	r.opened = append(r.opened, name)
	return r.FS.Open(name)
}

func wantErrContaining(t *testing.T, err error, subs ...string) {
	t.Helper()
	if err == nil {
		t.Fatalf("got nil error, want one containing %q", subs)
	}
	for _, s := range subs {
		if !strings.Contains(err.Error(), s) {
			t.Errorf("error %q does not contain %q", err, s)
		}
	}
}

func TestPartialRejectsBadNames(t *testing.T) {
	base := partialFS("# execute\n"+soloFetch+"\n", map[string]string{"verify.md": "outside prompts"})
	for _, c := range [][2]string{{"execute", "../verify"}, {"execute", "Solo"}, {"interaction", "x"}, {"execute.md", "solo"}, {"../prompts/execute", "solo"}} {
		rec := &recordingFS{FS: base}
		if b, err := Partial(rec, c[0], c[1]); err == nil {
			t.Errorf("Partial(%q, %q) = %q, want an error", c[0], c[1], b)
		}
		for _, p := range rec.opened {
			if !strings.HasPrefix(p, "prompts/") {
				t.Errorf("Partial(%q, %q) opened %q, outside prompts/", c[0], c[1], p)
			}
		}
	}
	b, err := Partial(base, "execute", "solo")
	if err != nil || string(b) != soloBody {
		t.Fatalf("Partial(execute, solo) = %q, %v; want the file verbatim", b, err)
	}
	_, err = Partial(base, "execute", "nope")
	wantErrContaining(t, err, `no partial "nope"`, "valid: solo")
}

func TestPartialNamesSkipInteraction(t *testing.T) {
	fsys := partialFS("# execute\n"+soloFetch+"\n", map[string]string{"prompts/_verify.drain.md": "drain\n"})
	names, err := PartialNames(fsys, "execute")
	if err != nil || strings.Join(names, ",") != "solo" {
		t.Fatalf("PartialNames(execute) = %v, %v; want [solo]", names, err)
	}
	for _, p := range []string{"execute", "verify", "quick", "ship"} {
		names, err := PartialNames(FS, p)
		if err != nil {
			t.Fatal(err)
		}
		for _, n := range names {
			if strings.Contains(n, "interaction") {
				t.Errorf("%s lists %q as a partial", p, n)
			}
		}
	}
	// _interaction.md is fetched by no line and is still not an orphan.
	if err := CheckPartials(partialFS("# execute\n"+soloFetch+"\n", nil)); err != nil {
		t.Fatalf("CheckPartials: %v", err)
	}
}

func TestPartialRegistryRealAssets(t *testing.T) {
	if err := CheckPartials(FS); err != nil {
		t.Fatalf("embedded prompts break the partial registry:\n%v", err)
	}
}

func TestPartialRegistryMissingAndOrphan(t *testing.T) {
	missing := partialFS("# execute\n\n"+strings.Replace(soloFetch, "execute solo", "execute ghost", 1)+"\n"+soloFetch+"\n", nil)
	wantErrContaining(t, CheckPartials(missing), "execute.md:3", "missing partial prompts/_execute.ghost.md")

	orphan := partialFS("# execute\n"+soloFetch+"\n", map[string]string{"prompts/_execute.ghost.md": "boo\n"})
	wantErrContaining(t, CheckPartials(orphan), "prompts/_execute.ghost.md: orphan partial")

	foreign := partialFS("# execute\n"+soloFetch+"\n**When** `mode=solo` — x: `dross prompt partial verify solo`\n", map[string]string{"prompts/_verify.solo.md": "v\n"})
	wantErrContaining(t, CheckPartials(foreign), "execute.md:3", "a prompt fetches only its own")

	nested := partialFS("# execute\n"+soloFetch+"\n", map[string]string{"prompts/_execute.solo.md": "x\n" + soloFetch + "\n"})
	wantErrContaining(t, CheckPartials(nested), "_execute.solo.md:2", "do not nest")
}

func TestFetchLineGrammar(t *testing.T) {
	for _, c := range []struct{ line, want string }{
		{"Run `dross prompt partial execute solo` when solo.", "a fetch line must read **When**"},
		{"**When** solo — the run: `dross prompt partial execute solo`", "a fetch line must read **When**"},
		{"**When** `colour=red` — red: `dross prompt partial execute solo`", `unknown trigger key "colour"`},
		{"**When** `mode=duo` — duo: `dross prompt partial execute solo`", `mode has no value "duo"`},
		{"**When** `mode=solo` — x: `dross prompt partial execute Solo`", "bad partial reference"},
	} {
		_, err := FetchPoints(partialFS("# execute\n"+c.line+"\n", nil), "execute.md")
		wantErrContaining(t, err, "execute.md:2", c.want)
	}
	points, err := FetchPoints(partialFS("# execute\n"+soloFetch+"\n", nil), "execute.md")
	if err != nil || len(points) != 1 {
		t.Fatalf("FetchPoints = %v, %v", points, err)
	}
	if p := points[0]; p.Line != 2 || p.Prompt != "execute" || p.Partial != "solo" || p.Key != "mode" || p.Value != "solo" || p.Refetch {
		t.Errorf("FetchPoint = %+v", p)
	}
}

func TestCommonPathFetchesNothing(t *testing.T) {
	for _, trig := range []string{"mode=pair", "test_exit=0", "verdict=pass", "leg=attached", "unclassified=zero"} {
		line := "**When** `" + trig + "` — always: `dross prompt partial execute solo`"
		_, err := FetchPoints(partialFS("# execute\n"+line+"\n", nil), "execute.md")
		wantErrContaining(t, err, "is the common path")
	}
	common := CommonPath()
	if common["verdict"] != "pass" || common["mode"] != "pair" || common["test_exit"] != "0" {
		t.Fatalf("CommonPath() = %v", common)
	}
	prompts, err := fs.Glob(FS, "prompts/[a-z]*.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range prompts {
		name := strings.TrimSuffix(strings.TrimPrefix(p, "prompts/"), ".md")
		got, err := Fetched(FS, name, common)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 0 {
			t.Errorf("%s fetches %v on the common path", name, got)
		}
	}
	// The other arm: the trigger's own fact does fetch.
	got, err := Fetched(partialFS("# execute\n"+soloFetch+"\n", nil), "execute", map[string]string{"mode": "solo"})
	if err != nil || strings.Join(got, ",") != "solo" {
		t.Fatalf("Fetched(mode=solo) = %v, %v; want [solo]", got, err)
	}
}

func TestTriggerProseAvoidsAlwaysWholeFile(t *testing.T) {
	for _, w := range []string{"a whole_file leg", "reason adapter-lacks-range-runner", "a Gremlins leg"} {
		line := "**When** `scope=degraded` — " + w + ": `dross prompt partial execute solo`"
		_, err := FetchPoints(partialFS("# execute\n"+line+"\n", nil), "execute.md")
		wantErrContaining(t, err, "holds on every gremlins run")
	}
}

func TestPromptsNeverReadPartialFiles(t *testing.T) {
	for _, ref := range []string{"Read `_execute.solo.md` now.", "Open ~/.claude/dross/prompts/_execute.solo.md."} {
		err := CheckPartials(partialFS("# execute\n"+soloFetch+"\n"+ref+"\n", nil))
		wantErrContaining(t, err, "execute.md:3", "fetch it with `dross prompt partial`")
	}
}

func TestExpandIdentity(t *testing.T) {
	want, err := FS.ReadFile("prompts/quick.md")
	if err != nil {
		t.Fatal(err)
	}
	got, err := Expand(FS, "quick")
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("Expand(quick) differs from quick.md (err %v)", err)
	}
	out, err := Expand(partialFS("# execute\n"+soloFetch+"\nnext base line\n", nil), "execute")
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	fetch, body, next := strings.Index(s, soloFetch), strings.Index(s, soloBody), strings.Index(s, "next base line")
	if fetch < 0 || body < 0 || next < 0 || !(fetch < body && body < next) {
		t.Fatalf("want fetch < body < next base line, got %d %d %d in:\n%s", fetch, body, next, s)
	}
	if want := "# execute\n" + soloFetch + "\n" + soloBody + "next base line\n"; s != want {
		t.Errorf("Expand = %q, want %q", s, want)
	}
}

func TestCommonPathInline(t *testing.T) {
	for file, needles := range map[string][]string{
		"prompts/execute.md": {"git add <task.files>", "dross changes record", "dross test --files", "approve <task-id>", "### 1g"},
		"prompts/verify.md": {
			"dross verify <phase>", "dross verify finalize", "`covered`", "`weak`", "`uncovered`", "If verdict is `pass`:",
			"milestone's `phases` array", "no active milestone",
			"gremlins always is", "Judge the score against what was actually instrumented",
		},
	} {
		raw, err := FS.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, n := range needles {
			if !bytes.Contains(raw, []byte(n)) {
				t.Errorf("common-path text %q left base %s", n, file)
			}
		}
	}
}

func TestRefetchLineIsAReference(t *testing.T) {
	refetch := "**Re-fetch** if context was compacted: `dross prompt partial execute solo`"
	fsys := partialFS("# execute\n"+soloFetch+"\nmiddle\n"+refetch+"\nend\n", nil)
	if err := CheckPartials(fsys); err != nil {
		t.Fatalf("CheckPartials: %v", err)
	}
	points, err := FetchPoints(fsys, "execute.md")
	if err != nil || len(points) != 2 || !points[1].Refetch || points[1].Partial != "solo" {
		t.Fatalf("FetchPoints = %+v, %v; want the re-fetch as a second, Refetch reference", points, err)
	}
	out, err := Expand(fsys, "execute")
	if err != nil {
		t.Fatal(err)
	}
	if want := "# execute\n" + soloFetch + "\n" + soloBody + "middle\n" + refetch + "\nend\n"; string(out) != want {
		t.Errorf("Expand = %q, want %q", out, want)
	}
	// Alone, a re-fetch is no fetch point: the partial is then an orphan and
	// the re-fetch names a partial nothing fetches.
	alone := partialFS("# execute\n"+refetch+"\n", nil)
	wantErrContaining(t, CheckPartials(alone), "re-fetches solo, which no When line fetches", "orphan partial")
	ghost := partialFS("# execute\n"+soloFetch+"\n"+strings.Replace(refetch, "solo", "ghost", 1)+"\n", nil)
	wantErrContaining(t, CheckPartials(ghost), "execute.md:3", "missing partial")
}

func TestExpandInlinesOnce(t *testing.T) {
	second := strings.Replace(soloFetch, "mode=solo", "preflight=recovery", 1)
	out, err := Expand(partialFS("# execute\n"+soloFetch+"\nmid\n"+second+"\nend\n", nil), "execute")
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if n := strings.Count(s, soloBody); n != 1 {
		t.Fatalf("partial inlined %d times, want 1:\n%s", n, s)
	}
	if strings.Index(s, soloBody) != strings.Index(s, soloFetch)+len(soloFetch)+1 {
		t.Errorf("partial not right after the first fetch line:\n%s", s)
	}
}

func TestExpandPartialPassThrough(t *testing.T) {
	fsys := partialFS("# execute\n"+soloFetch+"\n", nil)
	for name, want := range map[string]string{"_execute.solo": soloBody, "_interaction": "# Interaction playbook\n"} {
		got, err := Expand(fsys, name)
		if err != nil || string(got) != want {
			t.Errorf("Expand(%s) = %q, %v; want the file bytes", name, got, err)
		}
	}
	got, err := Expand(FS, "_interaction")
	if err != nil || string(got) != InteractionPlaybook {
		t.Errorf("Expand(FS, _interaction) differs from the playbook (err %v)", err)
	}
}

func TestExpandStripsMdSuffix(t *testing.T) {
	a, errA := Expand(FS, "verify.md")
	b, errB := Expand(FS, "verify")
	if errA != nil || errB != nil || !bytes.Equal(a, b) || len(a) == 0 {
		t.Fatalf("Expand(verify.md) != Expand(verify) (%v, %v)", errA, errB)
	}
	if _, err := Expand(FS, "../go.mod"); err == nil {
		t.Error("Expand(../go.mod) succeeded; want a refused name")
	}
}
