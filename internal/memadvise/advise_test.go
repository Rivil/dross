package memadvise

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

var adviseToday = time.Date(2026, 10, 30, 9, 0, 0, 0, time.UTC)

// memFixture writes files into a fresh memory dir and returns it.
func memFixture(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// readme is a tracked doc whose text the dup fixtures restate.
var readme = strings.Repeat("dross keeps phase state in toml files under the dross dir and commits them. ", 4)

func docsWithReadme() *Docs {
	return NewDocs([]string{"README.md"}, map[string]string{"README.md": readme})
}

func advise(t *testing.T, dir string, docs *Docs, known map[string]bool, repoTurns int) []Suggestion {
	t.Helper()
	got, err := Advise(Inputs{Dir: dir, Docs: docs, Today: adviseToday, Known: known, Turns: TurnCounts{Repo: repoTurns, All: repoTurns * 2}})
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func byID(ss []Suggestion) map[string]Suggestion {
	out := map[string]Suggestion{}
	for _, s := range ss {
		out[s.ID] = s
	}
	return out
}

func TestRewordPricesOnlyDelta(t *testing.T) {
	keep := strings.Repeat("k", 80)
	hook := keep + "; until 2026-01-01 xxxxxxxxxxxxxxxxxxxxx" // 120 bytes
	if len(hook) != 120 {
		t.Fatalf("fixture hook is %d bytes", len(hook))
	}
	dir := memFixture(t, map[string]string{
		IndexName: "- [A](a.md) — " + hook + "\n",
		"a.md":    "body\n",
	})
	got := advise(t, dir, nil, nil, 1000)
	if len(got) != 1 || got[0].Action != ActionReword || got[0].NewHook != keep {
		t.Fatalf("suggestions = %+v", got)
	}
	if got[0].BytesSaved != 40 || got[0].MonthlyUSD != MonthlyUSD(40, 1000) {
		t.Errorf("reword priced on %d bytes ($%v), want 40", got[0].BytesSaved, got[0].MonthlyUSD)
	}
}

func TestGuardBlocksEveryDeleteSource(t *testing.T) {
	dir := memFixture(t, map[string]string{
		IndexName:    "- [Dup](dup.md) — restates the readme\n",
		"dup.md":     "---\nname: dup\n---\nNEVER drop this. " + readme,
		"orphan.md":  "---\nname: o\n---\nDECOY " + readme,
		"control.md": readme,
	})
	for _, s := range advise(t, dir, docsWithReadme(), nil, 100) {
		if (s.File == "dup.md" || s.File == "orphan.md") && s.Action == ActionDelete {
			t.Errorf("a guarded memory was offered a delete: %+v", s)
		}
	}
	// The unguarded orphan control still gets its report-only delete.
	if s, ok := byID(advise(t, dir, docsWithReadme(), nil, 100))["delete-control"]; !ok || !s.ReportOnly {
		t.Errorf("the unguarded control orphan lacks its report-only delete")
	}
}

func TestStatusWordsAloneSuggestNothing(t *testing.T) {
	dir := memFixture(t, map[string]string{
		IndexName: "- [A](a.md) — DEAD\n- [B](b.md) — done\n- [C](c.md) — RESOLVED 2026-09-19\n- [D](d.md) — DECLINED\n- [E](e.md) — SHIPPED\n",
		"a.md":    "DEAD\n",
		"b.md":    "done\n",
		"c.md":    "RESOLVED 2026-09-19 (03983fe)\n",
		"d.md":    "DECLINED by the user\n",
		"e.md":    "SHIPPED 2026-10-04\n",
	})
	if got := advise(t, dir, docsWithReadme(), nil, 100); len(got) != 0 {
		t.Errorf("status words produced suggestions: %+v", got)
	}
}

func TestExpiredHookNeverDeletes(t *testing.T) {
	dir := memFixture(t, map[string]string{
		IndexName:  "- [Dup](dup.md) — restates the readme; next phase foo\n- [Plain](plain.md) — shipped; next: foo\n",
		"dup.md":   readme,
		"plain.md": "a plain memory body\n",
	})
	got := advise(t, dir, docsWithReadme(), map[string]bool{"foo": true}, 100)
	if len(got) != 2 {
		t.Fatalf("want one suggestion per memory, got %+v", got)
	}
	for _, s := range got {
		if s.Action != ActionReword || !strings.HasPrefix(s.ID, "reword-") {
			t.Errorf("an expired-hook memory got %+v, want a reword", s)
		}
	}
	ids := byID(got)
	if ids["reword-dup"].NewHook != "restates the readme" || ids["reword-plain"].NewHook != "shipped" {
		t.Errorf("reworded hooks = %q / %q", ids["reword-dup"].NewHook, ids["reword-plain"].NewHook)
	}
}

func TestExpiredHookOnSecondIndexLineNeverDeletes(t *testing.T) {
	second := "- [Dup again](dup.md) — also here; until 2026-01-01"
	dir := memFixture(t, map[string]string{
		IndexName: "- [Dup](dup.md) — restates the readme\n" + second + "\n",
		"dup.md":  readme,
	})
	got := advise(t, dir, docsWithReadme(), nil, 100)
	if len(got) != 1 {
		t.Fatalf("want one suggestion for the doubly-indexed memory, got %+v", got)
	}
	if s := got[0]; s.ID != "reword-dup" || s.Action != ActionReword || s.IndexLine != second || s.NewHook != "also here" {
		t.Errorf("doubly-indexed expired memory got %+v, want a reword of its second line", s)
	}
}

func TestSuggestionIDsUnique(t *testing.T) {
	for name, index := range map[string]string{
		"live first": "- [X](x.md) — d\n- [G1](gone.md) — a\n- [G2](gone.md) — b\n- [Old](old/x.md) — c\n",
		"dead first": "- [Old](old/x.md) — c\n- [G1](gone.md) — a\n- [G2](gone.md) — b\n- [X](x.md) — d\n",
	} {
		dir := memFixture(t, map[string]string{IndexName: index, "x.md": readme, "x-2.md": "taken\n"})
		got := advise(t, dir, docsWithReadme(), nil, 100)
		ids := byID(got)
		if len(ids) != len(got) {
			t.Errorf("%s: suggestion IDs repeat: %+v", name, got)
		}
		if s := ids["delete-x"]; s.File != "x.md" || s.Doc == "" {
			t.Errorf("%s: delete-x = %+v, want the live x.md's duplicate delete", name, s)
		}
		if ids["delete-gone"].File != "gone.md" || ids["delete-gone-2"].File != "gone.md" || ids["delete-x-3"].File != "old/x.md" {
			t.Errorf("%s: dead-link IDs = %v", name, sortedCopy(keys(ids)))
		}
	}
}

func keys(m map[string]Suggestion) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestSuggestionNamesInboundLinks(t *testing.T) {
	dir := memFixture(t, map[string]string{
		IndexName: "- [Dup](dup.md) — x\n- [L1](l1.md) — y\n- [L2](l2.md) — z\n",
		"dup.md":  "---\nname: the-dup\n---\n" + readme,
		"l1.md":   "see [[the-dup]]\n",
		"l2.md":   "also [[the-dup]]\n",
	})
	s, ok := byID(advise(t, dir, docsWithReadme(), nil, 100))["delete-dup"]
	if !ok {
		t.Fatal("no delete-dup suggestion")
	}
	if !reflect.DeepEqual(s.Inbound, []string{"l1.md", "l2.md"}) || !strings.Contains(s.Reason, "linked from") ||
		!strings.Contains(s.Reason, "l1.md") || !strings.Contains(s.Reason, "l2.md") {
		t.Errorf("suggestion = %+v", s)
	}
}

func TestDeleteNamesDoc(t *testing.T) {
	dir := memFixture(t, map[string]string{
		IndexName: "- [Dup](dup.md) — x\n",
		"dup.md":  readme,
	})
	docs := NewDocs([]string{"CLAUDE.md", "docs/state.md"}, map[string]string{
		"CLAUDE.md":     "unrelated guidance about something else entirely in this repository",
		"docs/state.md": readme,
	})
	got := advise(t, dir, docs, nil, 100)
	if len(got) != 1 || got[0].Doc != "docs/state.md" || !strings.Contains(got[0].Reason, "docs/state.md") {
		t.Errorf("suggestions = %+v", got)
	}
}

func TestDeadLinkDeletesIndexLineOnly(t *testing.T) {
	line := "- [Gone](gone.md) — its file was deleted"
	dir := memFixture(t, map[string]string{IndexName: line + "\n"})
	got := advise(t, dir, nil, nil, 1000)
	if len(got) != 1 {
		t.Fatalf("suggestions = %+v", got)
	}
	s := got[0]
	if s.ID != "delete-gone" || s.Action != ActionDelete || s.ReportOnly || s.IndexLine != line {
		t.Errorf("dead link suggestion = %+v", s)
	}
	if s.BytesSaved != len(line)+1 || s.MonthlyUSD != MonthlyUSD(len(line)+1, 1000) {
		t.Errorf("dead link priced on %d bytes, want the line's %d", s.BytesSaved, len(line)+1)
	}
}

func TestOrphanIsReportOnly(t *testing.T) {
	dir := memFixture(t, map[string]string{
		IndexName: "- [A](a.md) — x\n",
		"a.md":    "a\n",
		"lost.md": "nobody indexes me\n",
	})
	s, ok := byID(advise(t, dir, nil, nil, 100))["delete-lost"]
	if !ok || !s.ReportOnly || s.MonthlyUSD != 0 || s.File != "lost.md" {
		t.Errorf("orphan suggestion = %+v (found %v)", s, ok)
	}
}

func TestSuggestionIDsStable(t *testing.T) {
	files := map[string]string{
		IndexName: "- [Dup](dup.md) — x\n- [Gone](gone.md) — y\n- [Old](old.md) — z; until 2026-01-01\n",
		"dup.md":  readme,
		"old.md":  "old\n",
	}
	ids := func(ss []Suggestion) []string {
		var out []string
		for _, s := range ss {
			out = append(out, s.ID)
		}
		return out
	}
	first := ids(advise(t, memFixture(t, files), docsWithReadme(), nil, 100))
	again := ids(advise(t, memFixture(t, files), docsWithReadme(), nil, 100))
	if !reflect.DeepEqual(first, again) {
		t.Errorf("IDs differ across runs: %v vs %v", first, again)
	}
	files[IndexName] = "- [New](new.md) — fresh\n" + files[IndexName]
	files["new.md"] = "new\n"
	moved := ids(advise(t, memFixture(t, files), docsWithReadme(), nil, 100))
	if !reflect.DeepEqual(first, moved) {
		t.Errorf("an unrelated memory at the top changed IDs: %v vs %v", first, moved)
	}
	if want := []string{"delete-dup", "delete-gone", "reword-old"}; !reflect.DeepEqual(sortedCopy(first), want) {
		t.Errorf("IDs = %v, want %v", first, want)
	}
}

func sortedCopy(s []string) []string {
	out := slices.Clone(s)
	slices.Sort(out)
	return out
}

func TestSuggestionsSortByCostThenID(t *testing.T) {
	dir := memFixture(t, map[string]string{
		IndexName: "- [Short](b.md) — x\n- [A much longer index line](zz.md) — that costs more every turn\n- [Short](a.md) — x\n",
	})
	got := advise(t, dir, nil, nil, 1000)
	var order []string
	for _, s := range got {
		order = append(order, s.ID)
	}
	if want := []string{"delete-zz", "delete-a", "delete-b"}; !reflect.DeepEqual(order, want) {
		t.Errorf("order = %v, want %v", order, want)
	}
}

func TestAdviseMissingDirOrIndexIsEmpty(t *testing.T) {
	for name, dir := range map[string]string{
		"missing dir":   filepath.Join(t.TempDir(), "nope"),
		"empty dir":     t.TempDir(),
		"missing index": memFixture(t, map[string]string{"a.md": readme}),
	} {
		got, err := Advise(Inputs{Dir: dir, Docs: docsWithReadme(), Today: adviseToday})
		if err != nil || got == nil || len(got) != 0 {
			t.Errorf("%s: Advise = (%v, %v), want an empty slice and nil", name, got, err)
		}
	}
}
