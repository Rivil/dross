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
	for _, trig := range []string{
		"mode=pair", "test_exit=0", "verdict=pass", "leg=attached", "unclassified=zero",
		"panel=off", "auto=off", "ci=passed", "ship_result=clean", "target=given",
		"spec_file=absent", "deferral=none", "lanes=unchanged", "remote=unchanged",
	} {
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
	// techdebt.md fetches nothing, so its expansion is the file itself.
	if points, err := FetchPoints(FS, "techdebt.md"); err != nil || len(points) != 0 {
		t.Fatalf("techdebt.md has fetch points %v (err %v); pick a prompt that fetches nothing", points, err)
	}
	want, err := FS.ReadFile("prompts/techdebt.md")
	if err != nil {
		t.Fatal(err)
	}
	got, err := Expand(FS, "techdebt")
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("Expand(techdebt) differs from techdebt.md (err %v)", err)
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

func TestCommonPathVocabularyPinned(t *testing.T) {
	want := map[string]string{
		"mode": "pair", "preflight": "ok", "board_pull": "none", "test_exit": "0",
		"leg": "attached", "mutation": "ran", "scope": "ranged", "unclassified": "zero",
		"reviews": "none", "verdict": "pass",
		"panel": "off", "auto": "off", "ci": "passed", "ship_result": "clean",
		"target": "given", "spec_file": "absent", "deferral": "none",
		"lanes": "unchanged", "remote": "unchanged",
	}
	got := CommonPath()
	if len(got) != len(want) {
		t.Errorf("CommonPath() has %d keys, want %d: %v", len(got), len(want), got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("CommonPath()[%q] = %q, want %q (a key's common value must stay first)", k, got[k], v)
		}
	}
}

func TestCommonPathInline(t *testing.T) {
	var optionsHeadings []string
	for n := 1; n <= 17; n++ {
		optionsHeadings = append(optionsHeadings, "\n## "+itoa(n)+". ")
	}
	for file, needles := range map[string][]string{
		"prompts/plan.md": {"## 2G", "## 3. Propose", "## 5. Write plan.toml", "### 6.1 Auto review", `dross state set current_phase_status "planned"`},
		"prompts/spec.md": {
			"Step 1 — the entry gate", "dross phase create", "dross phase list --milestone", "Describe a new phase",
			"### 3b.", "[[criteria]]", `dross state touch "spec locked: <id>"`, "Resume, don't overwrite", "already has a target",
		},
		"prompts/quick.md": {`dross quick begin "<description>"`, "**Red, pair mode**", "dross quick end", "## 5. Commit", "Pair mode by default"},
		"prompts/ship.md": {
			"## 1. Preview", "## 3. Reviewers", "## 3.5 Merge landmarks", "dross ship <phase-id>", "gh run watch",
			"gh pr checks --watch", "dross ship recover", "All checks passed on PR #N. Merge now?",
		},
		"prompts/options.md": append([]string{
			"## Section pick", "Keep · Change · Skip", `dross local set <key> "<value>"`, "dross local get",
			"dross mutation remote status", "dross trust --check", "Never run it on their behalf", "dross test lane list",
		}, optionsHeadings...),
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

// copiedLines reports every partial line of 40+ characters that also stands
// verbatim, as a whole line, in the partial's own base prompt: moved text
// lives in one place.
func copiedLines(t *testing.T, fsys fs.FS) []string {
	t.Helper()
	all, err := allPartials(fsys)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, p := range all {
		base, err := fs.ReadFile(fsys, "prompts/"+p[0]+".md")
		if err != nil {
			t.Fatal(err)
		}
		inBase := map[string]bool{}
		for _, l := range strings.Split(string(base), "\n") {
			inBase[strings.TrimSpace(l)] = true
		}
		body, err := Partial(fsys, p[0], p[1])
		if err != nil {
			t.Fatal(err)
		}
		for i, l := range strings.Split(string(body), "\n") {
			if l = strings.TrimSpace(l); len(l) >= 40 && inBase[l] {
				out = append(out, partialPath(p[0], p[1])+":"+itoa(i+1)+": also in "+p[0]+".md: "+l)
			}
		}
	}
	return out
}

func TestPartialTextMovedNotCopied(t *testing.T) {
	for _, c := range copiedLines(t, FS) {
		t.Error(c)
	}
	line := "This sentence is well over forty characters long."
	fsys := partialFS("# execute\n"+soloFetch+"\n"+line+"\n", map[string]string{"prompts/_execute.solo.md": soloBody + line + "\n"})
	if got := copiedLines(t, fsys); len(got) != 1 || !strings.Contains(got[0], "_execute.solo.md:4") {
		t.Errorf("copied line not reported once: %v", got)
	}
	short := "short shared line"
	fsys = partialFS("# execute\n"+soloFetch+"\n"+short+"\n", map[string]string{"prompts/_execute.solo.md": soloBody + short + "\n"})
	if got := copiedLines(t, fsys); len(got) != 0 {
		t.Errorf("a short shared line was reported: %v", got)
	}
}

// headingLevel is the level of a markdown ATX heading line, else 0.
func headingLevel(line string) int {
	n := 0
	for n < len(line) && line[n] == '#' {
		n++
	}
	if n == 0 || n >= len(line) || line[n] != ' ' {
		return 0
	}
	return n
}

// headings returns each unfenced line's heading level (0 for body text).
func headings(lines []string) []int {
	out := make([]int, len(lines))
	fenced := false
	for i, l := range lines {
		if strings.HasPrefix(strings.TrimSpace(l), "```") {
			fenced = !fenced
			continue
		}
		if !fenced {
			out[i] = headingLevel(l)
		}
	}
	return out
}

// splitSections reports every fetch line whose partial opens a heading while
// base text of the fetch line's section still follows it: inlined there, the
// partial's heading would adopt that text. Text under a heading deeper than
// the partial's shallowest one is the same section, so it counts too.
func splitSections(t *testing.T, fsys fs.FS) []string {
	t.Helper()
	bases, err := fs.Glob(fsys, "prompts/[a-z]*.md")
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, f := range bases {
		file := strings.TrimPrefix(f, "prompts/")
		points, err := FetchPoints(fsys, file)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := fs.ReadFile(fsys, f)
		if err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(string(raw), "\n")
		levels := headings(lines)
		for _, p := range points {
			if p.Refetch {
				continue
			}
			body, err := Partial(fsys, p.Prompt, p.Partial)
			if err != nil {
				t.Fatal(err)
			}
			top := 0
			for _, lv := range headings(strings.Split(string(body), "\n")) {
				if lv > 0 && (top == 0 || lv < top) {
					top = lv
				}
			}
			if top == 0 {
				continue
			}
			for i := p.Line; i < len(lines); i++ { // lines[p.Line] is the line after the fetch
				if levels[i] > 0 && levels[i] <= top {
					break
				}
				if strings.TrimSpace(lines[i]) != "" {
					out = append(out, file+":"+itoa(p.Line)+": partial "+p.Partial+" opens a level-"+itoa(top)+" heading above base text at line "+itoa(i+1))
					break
				}
			}
		}
	}
	return out
}

func TestPartialSectionsStayWhole(t *testing.T) {
	for _, s := range splitSections(t, FS) {
		t.Error(s)
	}
	headed := map[string]string{"prompts/_execute.solo.md": "## Solo\n\nsolo text\n```\n# a shell comment, not a heading\n```\n"}
	for _, c := range []struct {
		name, base string
		want       bool
	}{
		{"fetch ends its section", "# execute\n## 1\ntext\n" + soloFetch + "\n\n## 2\nmore\n", false},
		{"fetch ends the file", "# execute\n## 1\ntext\n" + soloFetch + "\n", false},
		{"base text follows the fetch", "# execute\n## 1\n" + soloFetch + "\nrest of section 1\n## 2\n", true},
		{"a subsection follows the fetch", "# execute\n## 1\n" + soloFetch + "\n### 1a\nsub text\n## 2\n", true},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := splitSections(t, partialFS(c.base, headed))
			if (len(got) > 0) != c.want {
				t.Errorf("splitSections = %v, want reported=%v", got, c.want)
			}
		})
	}
	// A partial with no heading may sit mid-section; a fenced shell comment is
	// not a heading.
	for _, body := range []string{"solo text\n", "solo text\n```\n# a comment\n```\n"} {
		if got := splitSections(t, partialFS("# execute\n## 1\n"+soloFetch+"\nrest\n", map[string]string{"prompts/_execute.solo.md": body})); len(got) != 0 {
			t.Errorf("heading-free partial %q was reported: %v", body, got)
		}
	}
}
